// SPDX-License-Identifier: AGPL-3.0-only
package app

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

type credential struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	Endpoint     string   `json:"endpoint"`
	Region       string   `json:"region"`
	AccessKey    string   `json:"accessKey"`
	SecretKey    string   `json:"secretKey,omitempty"`
	SessionToken string   `json:"sessionToken,omitempty"`
	PathStyle    bool     `json:"pathStyle"`
	Enabled      bool     `json:"enabled"`
	Grants       []string `json:"grants"`
}

func validCredential(c *credential) error {
	c.Name = strings.TrimSpace(c.Name)
	if c.Region == "" {
		c.Region = "us-east-1"
	}
	if c.Name == "" || c.AccessKey == "" {
		return bad("InvalidRequest")
	}
	if c.Endpoint != "" {
		u, e := url.Parse(c.Endpoint)
		if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return bad("InvalidEndpoint")
		}
	}
	for i, g := range c.Grants {
		if g != "*" && !strings.HasPrefix(g, "role:") {
			a, e := mail.ParseAddress(g)
			if e != nil || a.Address != g {
				return bad("InvalidGrant")
			}
			c.Grants[i] = strings.ToLower(g)
		} else if strings.HasPrefix(g, "role:") && g != "role:ADMIN" && g != "role:USER" {
			return bad("InvalidGrant")
		}
	}
	return nil
}

const allowedSQL = `(EXISTS (SELECT 1 FROM ui_grants g WHERE g.credential_id=c.id AND g.subject IN ('*',$1,'role:'||$2)) OR $2='ADMIN')`

func (s *Server) credentialList(w http.ResponseWriter, r *http.Request, all bool) error {
	q := `SELECT c.id,c.name,c.endpoint,c.region,c.access_key,c.path_style,c.enabled,ARRAY(SELECT subject FROM ui_grants g WHERE g.credential_id=c.id ORDER BY subject) FROM ui_credentials c WHERE `
	if all {
		q += `$1::text IS NOT NULL AND $2::text IS NOT NULL`
	} else {
		q += `c.enabled AND ` + allowedSQL
	}
	q += ` ORDER BY c.id`
	rows, e := s.db.Query(r.Context(), q, user(r).Email, user(r).Role)
	if e != nil {
		return e
	}
	defer rows.Close()
	out := []credential{}
	for rows.Next() {
		var c credential
		if e = rows.Scan(&c.ID, &c.Name, &c.Endpoint, &c.Region, &c.AccessKey, &c.PathStyle, &c.Enabled, &c.Grants); e != nil {
			return e
		}
		out = append(out, c)
	}
	if e = rows.Err(); e != nil {
		return e
	}
	writeJSON(w, 200, out)
	return nil
}
func (s *Server) credentials(w http.ResponseWriter, r *http.Request) error {
	return s.credentialList(w, r, false)
}
func (s *Server) adminCredentials(w http.ResponseWriter, r *http.Request) error {
	if e := admin(r); e != nil {
		return e
	}
	return s.credentialList(w, r, true)
}
func (s *Server) selectCredential(w http.ResponseWriter, r *http.Request) error {
	var v struct {
		ID int64 `json:"id"`
	}
	if e := decode(w, r, &v); e != nil {
		return e
	}
	var ok bool
	e := s.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM ui_credentials c WHERE c.id=$3 AND c.enabled AND `+allowedSQL+`)`, user(r).Email, user(r).Role, v.ID).Scan(&ok)
	if e != nil {
		return e
	}
	if !ok {
		return &apiError{403, "AccessDenied"}
	}
	_, e = s.db.Exec(r.Context(), "UPDATE ui_sessions SET credential_id=$1 WHERE token_hash=$2", v.ID, user(r).TokenHash)
	if e == nil {
		w.WriteHeader(204)
	}
	return e
}
func pathID(r *http.Request) (int64, error) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		return 0, bad("InvalidRequest")
	}
	return id, nil
}
func (s *Server) saveCredential(w http.ResponseWriter, r *http.Request) error {
	if e := admin(r); e != nil {
		return e
	}
	var c credential
	if e := decode(w, r, &c); e != nil {
		return e
	}
	if e := validCredential(&c); e != nil {
		return e
	}
	tx, e := s.db.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	var encrypted, token []byte
	if r.Method == "PUT" {
		id, e := pathID(r)
		if e != nil {
			return e
		}
		c.ID = id
		e = tx.QueryRow(r.Context(), "SELECT secret,session_token FROM ui_credentials WHERE id=$1 FOR UPDATE", id).Scan(&encrypted, &token)
		if errors.Is(e, pgx.ErrNoRows) {
			return &apiError{404, "NotFound"}
		}
		if e != nil {
			return e
		}
	}
	if c.SecretKey != "" {
		encrypted = seal(s.cipher, c.SecretKey)
	}
	if len(encrypted) == 0 {
		return bad("SecretRequired")
	}
	if c.SessionToken != "" {
		token = seal(s.cipher, c.SessionToken)
	} else if c.SecretKey != "" {
		token = nil
	}
	if r.Method == "POST" {
		e = tx.QueryRow(r.Context(), `INSERT INTO ui_credentials(name,endpoint,region,access_key,secret,path_style,enabled,session_token) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, c.Name, c.Endpoint, c.Region, c.AccessKey, encrypted, c.PathStyle, c.Enabled, token).Scan(&c.ID)
	} else {
		_, e = tx.Exec(r.Context(), `UPDATE ui_credentials SET name=$1,endpoint=$2,region=$3,access_key=$4,secret=$5,path_style=$6,enabled=$7,session_token=$8 WHERE id=$9`, c.Name, c.Endpoint, c.Region, c.AccessKey, encrypted, c.PathStyle, c.Enabled, token, c.ID)
	}
	if e != nil {
		return e
	}
	if _, e = tx.Exec(r.Context(), "DELETE FROM ui_grants WHERE credential_id=$1", c.ID); e != nil {
		return e
	}
	for _, g := range c.Grants {
		if _, e = tx.Exec(r.Context(), "INSERT INTO ui_grants(credential_id,subject) VALUES($1,$2) ON CONFLICT DO NOTHING", c.ID, g); e != nil {
			return e
		}
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	c.SecretKey = ""
	c.SessionToken = ""
	s.record(r, "SAVE_CREDENTIAL", "", "", nil)
	writeJSON(w, 200, c)
	return nil
}
func (s *Server) deleteCredential(w http.ResponseWriter, r *http.Request) error {
	if e := admin(r); e != nil {
		return e
	}
	id, e := pathID(r)
	if e != nil {
		return e
	}
	_, e = s.db.Exec(r.Context(), "DELETE FROM ui_credentials WHERE id=$1", id)
	if e == nil {
		s.record(r, "DELETE_CREDENTIAL", "", "", nil)
		w.WriteHeader(204)
	}
	return e
}
func (s *Server) users(w http.ResponseWriter, r *http.Request) error {
	if e := admin(r); e != nil {
		return e
	}
	rows, e := s.db.Query(r.Context(), "SELECT id,email,role,enabled FROM ui_users ORDER BY id")
	if e != nil {
		return e
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var email, role string
		var enabled bool
		if e = rows.Scan(&id, &email, &role, &enabled); e != nil {
			return e
		}
		out = append(out, map[string]any{"id": id, "email": email, "role": role, "enabled": enabled})
	}
	if e = rows.Err(); e != nil {
		return e
	}
	writeJSON(w, 200, out)
	return nil
}
func (s *Server) saveUser(w http.ResponseWriter, r *http.Request) error {
	if e := admin(r); e != nil {
		return e
	}
	var v struct {
		Email, Password, Role string
		Enabled               bool
	}
	if e := decode(w, r, &v); e != nil {
		return e
	}
	v.Email = strings.ToLower(strings.TrimSpace(v.Email))
	if v.Role != "USER" && v.Role != "ADMIN" {
		return bad("InvalidRole")
	}
	if v.Password != "" && (len(v.Password) < 12 || len(v.Password) > 72) {
		return bad("InvalidPassword")
	}
	if r.Method == "POST" {
		a, e := mail.ParseAddress(v.Email)
		if e != nil || a.Address != v.Email || v.Password == "" {
			return bad("InvalidRequest")
		}
		h, e := bcrypt.GenerateFromPassword([]byte(v.Password), 12)
		if e != nil {
			return e
		}
		_, e = s.db.Exec(r.Context(), "INSERT INTO ui_users(email,password_hash,role,enabled) VALUES($1,$2,$3,$4)", v.Email, string(h), v.Role, v.Enabled)
		var pe *pgconn.PgError
		if errors.As(e, &pe) && pe.Code == "23505" {
			return &apiError{409, "UserExists"}
		}
		if e != nil {
			return e
		}
	} else {
		id, e := pathID(r)
		if e != nil {
			return e
		}
		if id == user(r).ID && (!v.Enabled || v.Role != "ADMIN") {
			return bad("CannotDisableSelf")
		}
		h := ""
		if v.Password != "" {
			b, e := bcrypt.GenerateFromPassword([]byte(v.Password), 12)
			if e != nil {
				return e
			}
			h = string(b)
		}
		tx, e := s.db.Begin(r.Context())
		if e != nil {
			return e
		}
		defer tx.Rollback(r.Context())
		tag, e := tx.Exec(r.Context(), "UPDATE ui_users SET role=$1,enabled=$2,password_hash=CASE WHEN $3='' THEN password_hash ELSE $3 END WHERE id=$4", v.Role, v.Enabled, h, id)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return &apiError{404, "NotFound"}
		}
		if h != "" || !v.Enabled {
			if _, e = tx.Exec(r.Context(), "DELETE FROM ui_sessions WHERE user_id=$1", id); e != nil {
				return e
			}
		}
		if e = tx.Commit(r.Context()); e != nil {
			return e
		}
	}
	s.record(r, "SAVE_USER", "", "", nil)
	w.WriteHeader(204)
	return nil
}
func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) error {
	if e := admin(r); e != nil {
		return e
	}
	id, e := pathID(r)
	if e != nil {
		return e
	}
	if id == user(r).ID {
		return bad("CannotDisableSelf")
	}
	_, e = s.db.Exec(r.Context(), "DELETE FROM ui_users WHERE id=$1", id)
	if e == nil {
		s.record(r, "DELETE_USER", "", "", nil)
		w.WriteHeader(204)
	}
	return e
}
func (s *Server) record(r *http.Request, action, bucket, key string, err error) {
	code := "OK"
	if err != nil {
		code = "FAILED"
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Second)
	defer cancel()
	_, e := s.db.Exec(ctx, "INSERT INTO ui_audit(actor,action,bucket,object_key,code) VALUES($1,$2,$3,$4,$5)", user(r).Email, action, bucket, key, code)
	if e != nil { /* Log separately from the S3 operation: never retry a committed mutation. */
		logAuditError(e)
	}
}
func (s *Server) audit(w http.ResponseWriter, r *http.Request) error {
	before := int64(9223372036854775807)
	if v := r.URL.Query().Get("before"); v != "" {
		n, e := strconv.ParseInt(v, 10, 64)
		if e != nil {
			return bad("InvalidRequest")
		}
		before = n
	}
	rows, e := s.db.Query(r.Context(), `SELECT id,actor,action,bucket,object_key,code,created_at FROM ui_audit WHERE id<$1 AND (actor=$2 OR $3='ADMIN') ORDER BY id DESC LIMIT 100`, before, user(r).Email, user(r).Role)
	if e != nil {
		return e
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var actor, action, bucket, key, code string
		var date time.Time
		if e = rows.Scan(&id, &actor, &action, &bucket, &key, &code, &date); e != nil {
			return e
		}
		out = append(out, map[string]any{"id": id, "actor": actor, "action": action, "bucket": bucket, "key": key, "code": code, "date": date})
	}
	if e = rows.Err(); e != nil {
		return e
	}
	writeJSON(w, 200, out)
	return nil
}
