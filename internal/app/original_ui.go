// SPDX-License-Identifier: AGPL-3.0-only
package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/wenisch-tech/s3webui/downstream/src/main/resources"
)

type responseBuffer struct {
	header http.Header
	status int
	bytes.Buffer
}

func newResponseBuffer() *responseBuffer {
	return &responseBuffer{header: make(http.Header), status: 200}
}
func (b *responseBuffer) Header() http.Header    { return b.header }
func (b *responseBuffer) WriteHeader(status int) { b.status = status }
func (s *Server) pageAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := newResponseBuffer()
		s.authenticate(next).ServeHTTP(b, r)
		if b.status == 401 {
			http.Redirect(w, r, "/login", 303)
			return
		}
		if b.status == 403 {
			http.Redirect(w, r, "/access-denied", 303)
			return
		}
		for k, v := range b.header {
			w.Header()[k] = v
		}
		w.WriteHeader(b.status)
		_, _ = w.Write(b.Bytes())
	})
}
func (s *Server) formLogin(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	if e := r.ParseForm(); e != nil {
		return bad("InvalidRequest")
	}
	body, _ := json.Marshal(map[string]string{"email": r.Form.Get("email"), "password": r.Form.Get("password")})
	r.Body = io.NopCloser(bytes.NewReader(body))
	b := newResponseBuffer()
	if e := s.login(b, r); e != nil {
		if a, ok := e.(*apiError); ok && a.Status < 500 {
			http.Redirect(w, r, "/login?error", 303)
			return nil
		}
		return e
	}
	for _, v := range b.Header().Values("Set-Cookie") {
		w.Header().Add("Set-Cookie", v)
	}
	http.Redirect(w, r, "/", 303)
	return nil
}
func (s *Server) formLogout(w http.ResponseWriter, r *http.Request) error {
	b := newResponseBuffer()
	if e := s.logout(b, r); e != nil {
		return e
	}
	for _, v := range b.Header().Values("Set-Cookie") {
		w.Header().Add("Set-Cookie", v)
	}
	http.Redirect(w, r, "/login?logout", 303)
	return nil
}
func localeOf(r *http.Request) string {
	if c, e := r.Cookie("s3webui-language"); e == nil && (c.Value == "zh" || c.Value == "en") {
		return c.Value
	}
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Accept-Language")), "zh") {
		return "zh"
	}
	return "en"
}

var chineseCatalog = func() map[string]string {
	out := map[string]string{}
	b, _ := resources.Files.ReadFile("locales/zh.json")
	_ = json.Unmarshal(b, &out)
	return out
}()

func originalTranslate(v, locale string) string {
	if locale != "zh" {
		return v
	}
	key := strings.Join(strings.Fields(v), " ")
	if translation, ok := chineseCatalog[key]; ok {
		return strings.TrimSuffix(v, strings.TrimLeft(v, " \r\n\t")) + translation + v[len(strings.TrimRight(v, " \r\n\t")):]
	}
	return v
}
func (s *Server) originalPage(page string) endpoint {
	return func(w http.ResponseWriter, r *http.Request) error {
		locale := localeOf(r)
		appVersion := s.cfg.Version
		if appVersion == "" {
			appVersion = "dev"
		}
		m := map[string]any{"locale": locale, "basePath": "", "appVersion": appVersion, "licensePageUrl": "/LICENSE", "releasePageUrl": "https://github.com/phil616/s3webui/releases", "sourceURL": s.cfg.SourceURL, "authenticationDisabled": false, "authenticated": false, "isAdmin": false, "activeCredentialName": nil, "s3BreadcrumbLabel": "S3", "oidcEnabled": false, "oidcProviders": []any{}, "oidcRequiredRole": "", "loginError": r.URL.Query().Has("error"), "loggedOut": r.URL.Query().Has("logout"), "oidcError": false, "error": nil, "grantTypes": []string{"ALL_AUTHENTICATED", "USER", "ROLE"}, "userRoles": []string{"ADMIN", "USER"}}
		if page != "login" {
			u := user(r)
			m["authenticated"] = true
			m["username"] = u.Email
			m["isAdmin"] = u.Role == "ADMIN"
			if page == "admin/settings" && u.Role != "ADMIN" {
				return &apiError{403, "AccessDenied"}
			}
			if u.CredentialID != nil {
				var name string
				if s.db.QueryRow(r.Context(), `SELECT c.name FROM ui_credentials c WHERE c.id=$3 AND c.enabled AND `+allowedSQL, u.Email, u.Role, *u.CredentialID).Scan(&name) == nil {
					m["activeCredentialName"] = name
					m["s3BreadcrumbLabel"] = name
				}
			}
			switch page {
			case "buckets":
				m["buckets"] = []map[string]any{}
				if u.CredentialID != nil {
					v, e := s.originalBuckets(r)
					if e != nil {
						m["error"] = originalTranslate("Error connecting to S3", locale)
					} else {
						m["buckets"] = v
					}
				}
			case "bucket":
				b, p := r.PathValue("bucket"), r.URL.Query().Get("prefix")
				m["bucket"] = b
				m["prefix"] = p
				m["highlight"] = r.URL.Query().Get("highlight")
				m["objects"] = []map[string]any{}
				crumbs := []map[string]any{}
				for i, c := range p {
					if c == '/' {
						crumbs = append(crumbs, map[string]any{"name": p[strings.LastIndex(p[:i], "/")+1 : i], "prefix": p[:i+1]})
					}
				}
				m["breadcrumbs"] = crumbs
				if u.CredentialID != nil {
					v, e := s.originalObjects(r)
					if e != nil {
						m["error"] = originalTranslate("Error connecting to S3", locale)
					} else {
						m["objects"] = v
					}
				}
			case "search":
				q := strings.TrimSpace(r.URL.Query().Get("q"))
				m["query"] = q
				m["searchResults"] = nil
				m["currentPage"] = 1
				m["pageCount"] = 1
				if u.CredentialID != nil {
					result, e := s.originalSearch(r, q, "")
					if e != nil {
						m["error"] = e.Error()
					} else {
						all := result["results"].([]map[string]any)
						pages := (len(all) + 49) / 50
						if pages < 1 {
							pages = 1
						}
						page, _ := strconv.Atoi(r.URL.Query().Get("page"))
						if page < 1 {
							page = 1
						}
						if page > pages {
							page = pages
						}
						start := (page - 1) * 50
						end := start + 50
						if end > len(all) {
							end = len(all)
						}
						result["results"] = all[start:end]
						m["searchResults"] = result
						m["currentPage"] = page
						m["pageCount"] = pages
					}
				}
			}
		}
		output, e := s.templates.render(page, m)
		if e != nil {
			return e
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Language", locale)
		_, e = w.Write(output)
		return e
	}
}
func (s *Server) originalBuckets(r *http.Request) ([]map[string]any, error) {
	c, e := s.client(r)
	if e != nil {
		return nil, e
	}
	pager := s3.NewListBucketsPaginator(c, &s3.ListBucketsInput{MaxBuckets: aws.Int32(1000)})
	out := []map[string]any{}
	for pager.HasMorePages() {
		p, e := pager.NextPage(r.Context())
		if e != nil {
			return nil, e
		}
		for _, b := range p.Buckets {
			var date any
			if b.CreationDate != nil {
				date = *b.CreationDate
			}
			out = append(out, map[string]any{"name": aws.ToString(b.Name), "creationDate": date})
		}
	}
	return out, nil
}
func (s *Server) originalObjects(r *http.Request) ([]map[string]any, error) {
	c, e := s.client(r)
	if e != nil {
		return nil, e
	}
	prefix := r.URL.Query().Get("prefix")
	pager := s3.NewListObjectsV2Paginator(c, &s3.ListObjectsV2Input{Bucket: bucket(r), Prefix: ptr(prefix), Delimiter: aws.String("/")})
	out := []map[string]any{}
	for pager.HasMorePages() {
		p, e := pager.NextPage(r.Context())
		if e != nil {
			return nil, e
		}
		for _, d := range p.CommonPrefixes {
			k := aws.ToString(d.Prefix)
			out = append(out, map[string]any{"name": strings.TrimSuffix(strings.TrimPrefix(k, prefix), "/"), "prefix": k, "key": k, "directory": true, "size": 0, "lastModified": nil})
		}
		for _, o := range p.Contents {
			k := aws.ToString(o.Key)
			if k == prefix {
				continue
			}
			var date any
			if o.LastModified != nil {
				date = *o.LastModified
			}
			out = append(out, map[string]any{"name": strings.TrimPrefix(k, prefix), "key": k, "prefix": prefix, "directory": false, "size": aws.ToInt64(o.Size), "lastModified": date})
		}
	}
	return out, nil
}
func (s *Server) originalSearch(r *http.Request, q, onlyBucket string) (map[string]any, error) {
	c, e := s.client(r)
	if e != nil {
		return nil, e
	}
	buckets := []map[string]any{{"name": onlyBucket}}
	if onlyBucket == "" {
		buckets, e = s.originalBuckets(r)
		if e != nil {
			return nil, e
		}
	}
	results := []map[string]any{}
	failed := []string{}
	query := strings.ToLower(q)
	for _, b := range buckets {
		name := text(b["name"])
		seen := map[string]bool{}
		pager := s3.NewListObjectsV2Paginator(c, &s3.ListObjectsV2Input{Bucket: aws.String(name), Prefix: ptr(r.URL.Query().Get("prefix"))})
		for page := 0; pager.HasMorePages(); page++ {
			if page >= 100 {
				failed = append(failed, name)
				break
			}
			p, e := pager.NextPage(r.Context())
			if e != nil {
				failed = append(failed, name)
				break
			}
			for _, o := range p.Contents {
				k := aws.ToString(o.Key)
				add := func(key string, folder bool) {
					if seen[key] || !strings.Contains(strings.ToLower(key), query) {
						return
					}
					seen[key] = true
					plain := strings.TrimSuffix(key, "/")
					i := strings.LastIndex(plain, "/")
					kind := "FILE"
					if folder {
						kind = "FOLDER"
					}
					var date any
					if o.LastModified != nil {
						date = *o.LastModified
					}
					results = append(results, map[string]any{"bucket": name, "key": key, "name": plain[i+1:], "parentPrefix": plain[:i+1], "type": kind, "size": aws.ToInt64(o.Size), "lastModified": date})
				}
				for i, c := range k {
					if c == '/' {
						add(k[:i+1], true)
					}
				}
				if !strings.HasSuffix(k, "/") {
					add(k, false)
				}
			}
		}
	}
	return map[string]any{"results": results, "total": len(results), "failedBuckets": failed, "incomplete": len(failed) > 0}, nil
}
func (s *Server) originalSearchAPI(w http.ResponseWriter, r *http.Request) error {
	v, e := s.originalSearch(r, r.URL.Query().Get("q"), r.PathValue("bucket"))
	if e != nil {
		return e
	}
	if n, _ := strconv.Atoi(r.URL.Query().Get("limit")); n > 0 {
		all := v["results"].([]map[string]any)
		if len(all) > n {
			v["results"] = all[:n]
		}
	}
	writeJSON(w, 200, v)
	return nil
}
func (s *Server) originalStats(w http.ResponseWriter, r *http.Request) error {
	c, e := s.client(r)
	if e != nil {
		return e
	}
	var count, size int64
	pager := s3.NewListObjectsV2Paginator(c, &s3.ListObjectsV2Input{Bucket: bucket(r)})
	for pager.HasMorePages() {
		page, e := pager.NextPage(r.Context())
		if e != nil {
			return e
		}
		for _, o := range page.Contents {
			count++
			size += aws.ToInt64(o.Size)
		}
	}
	writeJSON(w, 200, map[string]int64{"objectCount": count, "totalSize": size})
	return nil
}
