// SPDX-License-Identifier: AGPL-3.0-only
package app

import (
	"context"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/smithy-go"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wenisch-tech/s3webui/downstream/src/main/resources"
	"golang.org/x/crypto/bcrypt"
)

//go:embed schema.sql
var schema string

type Server struct {
	db            *pgxpool.Pool
	cipher        cipher.AEAD
	cfg           Config
	handler       http.Handler
	loginSlots    chan struct{}
	transferSlots chan struct{}
	httpClient    *http.Client
	templates     *originalTemplates
}
type identity struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	CredentialID *int64 `json:"credentialId"`
	TokenHash    string `json:"-"`
}
type contextKey struct{}
type apiError struct {
	Status int
	Code   string
}

func (e *apiError) Error() string    { return e.Code }
func bad(code string) error          { return &apiError{400, code} }
func user(r *http.Request) *identity { return r.Context().Value(contextKey{}).(*identity) }

type endpoint func(http.ResponseWriter, *http.Request) error

func (s *Server) wrap(fn endpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if e := fn(w, r); e != nil {
			s.fail(w, e)
		}
	}
}
func (s *Server) fail(w http.ResponseWriter, e error) {
	status, code := 500, "InternalError"
	var ae *apiError
	var se smithy.APIError
	var re *awshttp.ResponseError
	if errors.As(e, &ae) {
		status, code = ae.Status, ae.Code
	} else if errors.As(e, &se) {
		status, code = 502, se.ErrorCode()
		if errors.As(e, &re) {
			status = re.HTTPStatusCode()
		}
		if status < 400 {
			status = 502
		}
	}
	if status >= 500 {
		slog.Error("request failed", "code", code, "error", e)
	}
	writeJSON(w, status, map[string]string{"code": code})
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return bad("InvalidRequest")
	}
	if d.Decode(new(any)) != io.EOF {
		return bad("InvalidRequest")
	}
	return nil
}
func randomToken() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func hashToken(t string) string { v := sha256.Sum256([]byte(t)); return hex.EncodeToString(v[:]) }

func New(ctx context.Context, c Config) (*Server, error) {
	if e := c.validate(); e != nil {
		return nil, e
	}
	a, e := newCipher(c.EncryptionKey)
	if e != nil {
		return nil, e
	}
	db, e := pgxpool.New(ctx, c.DatabaseURL)
	if e != nil {
		return nil, e
	}
	ok := false
	defer func() {
		if !ok {
			db.Close()
		}
	}()
	if e = db.Ping(ctx); e != nil {
		return nil, e
	}
	tx, e := db.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(73981025)"); e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, schema); e != nil {
		return nil, e
	}
	var stored []byte
	e = tx.QueryRow(ctx, "SELECT secret FROM ui_credentials LIMIT 1").Scan(&stored)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return nil, e
	}
	if len(stored) > 0 {
		if _, e = unseal(a, stored); e != nil {
			return nil, errors.New("APP_ENCRYPTION_KEY cannot decrypt stored credentials")
		}
	}
	var count int
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM ui_users").Scan(&count); e != nil {
		return nil, e
	}
	if count == 0 {
		h, e := bcrypt.GenerateFromPassword([]byte(c.AdminPassword), 12)
		if e != nil {
			return nil, e
		}
		if _, e = tx.Exec(ctx, "INSERT INTO ui_users(email,password_hash,role) VALUES($1,$2,'ADMIN')", strings.ToLower(strings.TrimSpace(c.AdminEmail)), string(h)); e != nil {
			return nil, e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	transport.MaxIdleConnsPerHost = 10
	s := &Server{db: db, cipher: a, cfg: c, loginSlots: make(chan struct{}, 4), transferSlots: make(chan struct{}, 2), httpClient: &http.Client{Transport: transport}}
	s.templates, e = loadOriginalTemplates()
	if e != nil {
		return nil, e
	}
	s.routes()
	ok = true
	return s, nil
}
func (s *Server) Close()                                           { s.httpClient.CloseIdleConnections(); s.db.Close() }
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }
func (s *Server) routes() {
	m := http.NewServeMux()
	m.HandleFunc("GET /livez", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	if s.cfg.SourceArchive != "" {
		m.HandleFunc("GET /source", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/gzip")
			w.Header().Set("Content-Disposition", `attachment; filename="s3webui-source.tar.gz"`)
			http.ServeFile(w, r, s.cfg.SourceArchive)
		})
	}
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if s.db.Ping(ctx) != nil {
			w.WriteHeader(503)
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	m.HandleFunc("GET /api/meta", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"sourceUrl": s.cfg.SourceURL, "upstream": "https://github.com/wenisch-tech/s3webui", "license": "AGPL-3.0"})
	})
	m.HandleFunc("POST /api/login", s.wrap(s.login))
	auth := func(pattern string, fn endpoint) { m.Handle(pattern, s.authenticate(s.wrap(fn))) }
	m.HandleFunc("GET /login", s.wrap(s.originalPage("login")))
	m.Handle("GET /access-denied", s.pageAuth(s.wrap(s.originalPage("access-denied"))))
	m.HandleFunc("POST /login", s.wrap(s.formLogin))
	for pattern, page := range map[string]string{"GET /{$}": "buckets", "GET /buckets/{bucket}": "bucket", "GET /history": "history", "GET /search": "search", "GET /admin/settings": "admin/settings"} {
		m.Handle(pattern, s.pageAuth(s.wrap(s.originalPage(page))))
	}
	m.Handle("POST /logout", s.pageAuth(s.wrap(s.formLogout)))
	auth("GET /api/ui/search", s.originalSearchAPI)
	auth("GET /api/buckets/{bucket}/objects/search", s.originalSearchAPI)
	auth("GET /api/buckets/{bucket}/stats", s.originalStats)
	auth("POST /api/buckets/{bucket}/objects/upload", s.originalUpload)
	auth("POST /api/buckets/{bucket}/objects/delete", s.originalDelete)
	auth("POST /api/buckets/{bucket}/objects/move", s.originalMove)
	auth("GET /api/me", func(w http.ResponseWriter, r *http.Request) error { writeJSON(w, 200, user(r)); return nil })
	auth("POST /api/logout", s.logout)
	auth("GET /api/credentials", s.credentials)
	auth("POST /api/credentials/select", s.selectCredential)
	auth("GET /api/admin/credentials", s.adminCredentials)
	auth("POST /api/admin/credentials", s.saveCredential)
	auth("PUT /api/admin/credentials/{id}", s.saveCredential)
	auth("DELETE /api/admin/credentials/{id}", s.deleteCredential)
	auth("GET /api/admin/users", s.users)
	auth("POST /api/admin/users", s.saveUser)
	auth("PUT /api/admin/users/{id}", s.saveUser)
	auth("DELETE /api/admin/users/{id}", s.deleteUser)
	auth("GET /api/audit", s.audit)
	auth("GET /api/buckets", s.buckets)
	auth("POST /api/buckets", s.createBucket)
	auth("DELETE /api/buckets/{bucket}", s.deleteBucket)
	auth("GET /api/buckets/{bucket}/objects", s.objects)
	auth("GET /api/buckets/{bucket}/versions", s.versions)
	auth("GET /api/buckets/{bucket}/objects/download", s.download)
	auth("GET /api/buckets/{bucket}/objects/head", s.head)
	auth("PUT /api/buckets/{bucket}/objects", s.putObject)
	auth("DELETE /api/buckets/{bucket}/objects", s.deleteObject)
	auth("POST /api/buckets/{bucket}/objects/copy", s.copyObject)
	auth("GET /api/search", s.search)
	auth("POST /api/buckets/{bucket}/multipart/initiate", s.initiate)
	auth("PUT /api/buckets/{bucket}/multipart/part", s.uploadPart)
	auth("POST /api/buckets/{bucket}/multipart/complete", s.complete)
	auth("DELETE /api/buckets/{bucket}/multipart/abort", s.abort)
	auth("GET /api/buckets/{bucket}/multipart", s.uploads)
	auth("GET /api/buckets/{bucket}/multipart/parts", s.parts)
	auth("GET /api/buckets/{bucket}/config/{kind}", s.bucketConfig)
	auth("PUT /api/buckets/{bucket}/config/{kind}", s.bucketConfig)
	auth("DELETE /api/buckets/{bucket}/config/{kind}", s.bucketConfig)
	m.HandleFunc("GET /LICENSE", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, licenseText)
	})
	assets, _ := fs.Sub(resources.Files, "static")
	for _, path := range []string{"/css/", "/js/", "/fonts/", "/img/", "/licenses/"} {
		m.Handle("GET "+path, http.FileServer(http.FS(assets)))
	}
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; font-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			u, e := url.Parse(r.Header.Get("Origin"))
			p, _ := url.Parse(s.cfg.PublicURL)
			if e != nil || u.Host != p.Host || u.Scheme != p.Scheme {
				s.fail(w, &apiError{403, "InvalidOrigin"})
				return
			}
		}
		m.ServeHTTP(w, r)
	})
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := r.Cookie("s3webui-session")
		if e != nil {
			s.fail(w, &apiError{401, "Unauthenticated"})
			return
		}
		id := &identity{TokenHash: hashToken(c.Value)}
		e = s.db.QueryRow(r.Context(), `SELECT u.id,u.email,u.role,s.credential_id FROM ui_sessions s JOIN ui_users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>now() AND u.enabled`, id.TokenHash).Scan(&id.ID, &id.Email, &id.Role, &id.CredentialID)
		if e != nil {
			s.fail(w, &apiError{401, "Unauthenticated"})
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, id)))
	})
}
func admin(r *http.Request) error {
	if user(r).Role != "ADMIN" {
		return &apiError{403, "AccessDenied"}
	}
	return nil
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) error {
	var v struct{ Email, Password string }
	if e := decode(w, r, &v); e != nil {
		return e
	}
	if len(v.Password) > 72 {
		return bad("InvalidCredentials")
	}
	select {
	case s.loginSlots <- struct{}{}:
		defer func() { <-s.loginSlots }()
	default:
		return &apiError{429, "TooManyRequests"}
	}
	subject := hashToken(strings.ToLower(strings.TrimSpace(v.Email)))
	var attempts int
	e := s.db.QueryRow(r.Context(), `INSERT INTO ui_login_attempts(subject_hash,window_start,attempts) VALUES($1,now(),1)
 ON CONFLICT(subject_hash) DO UPDATE SET
 attempts=CASE WHEN ui_login_attempts.window_start<now()-interval '15 minutes' THEN 1 ELSE ui_login_attempts.attempts+1 END,
 window_start=CASE WHEN ui_login_attempts.window_start<now()-interval '15 minutes' THEN now() ELSE ui_login_attempts.window_start END RETURNING attempts`, subject).Scan(&attempts)
	if e != nil {
		return e
	}
	if attempts > 20 {
		return &apiError{429, "TooManyRequests"}
	}
	var id int64
	var h string
	e = s.db.QueryRow(r.Context(), "SELECT id,password_hash FROM ui_users WHERE email=$1 AND enabled", strings.ToLower(strings.TrimSpace(v.Email))).Scan(&id, &h)
	// Always perform a password comparison, including for unknown accounts.
	if e != nil {
		h = "$2a$12$uLgpv5rvKN2ox25oFY3wuewRPq8hUf45QYvRKcTGJ.cj7B1NRlc6y"
	}
	check := bcrypt.CompareHashAndPassword([]byte(h), []byte(v.Password))
	if e != nil || check != nil {
		return &apiError{401, "InvalidCredentials"}
	}
	token := randomToken()
	tx, e := s.db.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if _, e = tx.Exec(r.Context(), "DELETE FROM ui_sessions WHERE expires_at<now()"); e != nil {
		return e
	}
	if _, e = tx.Exec(r.Context(), "DELETE FROM ui_login_attempts WHERE subject_hash=$1 OR window_start<now()-interval '1 day'", subject); e != nil {
		return e
	}
	if _, e = tx.Exec(r.Context(), "INSERT INTO ui_sessions(token_hash,user_id,expires_at) VALUES($1,$2,now()+interval '12 hours')", hashToken(token), id); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	http.SetCookie(w, &http.Cookie{Name: "s3webui-session", Value: token, Path: "/", HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
	writeJSON(w, 200, map[string]bool{"ok": true})
	return nil
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) error {
	_, e := s.db.Exec(r.Context(), "DELETE FROM ui_sessions WHERE token_hash=$1", user(r).TokenHash)
	if e != nil {
		return e
	}
	http.SetCookie(w, &http.Cookie{Name: "s3webui-session", Value: "", Path: "/", HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	w.WriteHeader(204)
	return nil
}
func ptr(v string) *string {
	if v == "" {
		return nil
	}
	return aws.String(v)
}
