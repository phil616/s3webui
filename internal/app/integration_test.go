// SPDX-License-Identifier: AGPL-3.0-only
package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is unset: PostgreSQL integration tests skipped")
	}
	ctx := context.Background()
	root, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	namespace := "test_" + randomToken()[:16]
	if _, e = root.Exec(ctx, "CREATE SCHEMA "+namespace); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		_, e := root.Exec(ctx, "DROP SCHEMA "+namespace+" CASCADE")
		if e != nil {
			t.Error(e)
		}
		root.Close()
	})
	parsed, e := url.Parse(dsn)
	if e != nil {
		t.Fatal(e)
	}
	q := parsed.Query()
	q.Set("search_path", namespace)
	parsed.RawQuery = q.Encode()
	s, e := New(ctx, Config{DatabaseURL: parsed.String(), AdminEmail: "admin@example.com", AdminPassword: "development-password", EncryptionKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{8}, 32)), SourceURL: "https://example.com/source/v1", PublicURL: "http://ui.test", SecureCookies: false})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	return s
}

type testBrowser struct {
	s      *Server
	cookie *http.Cookie
	t      *testing.T
}

func (b *testBrowser) request(method, path, body string) (int, []byte) {
	b.t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Origin", "http://ui.test")
	if b.cookie != nil {
		r.AddCookie(b.cookie)
	}
	w := httptest.NewRecorder()
	b.s.ServeHTTP(w, r)
	for _, c := range w.Result().Cookies() {
		b.cookie = c
	}
	return w.Code, w.Body.Bytes()
}
func (b *testBrowser) expect(method, path, body string, status int) []byte {
	b.t.Helper()
	got, data := b.request(method, path, body)
	if got != status {
		b.t.Fatalf("%s %s: status %d expected %d: %s", method, path, got, status, data)
	}
	return data
}
func TestPostgresAuthGrantsAndS3(t *testing.T) {
	s := testServer(t)
	b := &testBrowser{s: s, t: t}
	b.expect("GET", "/api/buckets", "", 401)
	r := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"email":"admin@example.com","password":"development-password"}`))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("login CSRF accepted")
	}
	b.expect("POST", "/api/login", `{"email":"admin@example.com","password":"wrong"}`, 401)
	b.expect("POST", "/api/login", `{"email":"admin@example.com","password":"development-password"}`, 200)
	if b.cookie == nil || !b.cookie.HttpOnly || b.cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe session cookie")
	}
	b.expect("GET", "/api/buckets", "", 409)
	var calls atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !strings.Contains(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256") {
			t.Error("S3 request not SigV4 signed")
		}
		if r.Header.Get("X-Amz-Security-Token") != "temporary-token" {
			t.Error("session token missing")
		}
		w.Header().Set("Content-Type", "application/xml")
		switch {
		case r.Method == "GET" && r.URL.Query().Get("list-type") == "2":
			if r.URL.Query().Get("continuation-token") != "opaque+/=" {
				t.Error("pagination token changed")
			}
			if r.URL.Query().Get("prefix") != "中文//../" {
				t.Error("prefix normalized")
			}
			fmt.Fprint(w, `<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>bucket</Name><IsTruncated>true</IsTruncated><NextContinuationToken>next+/=</NextContinuationToken><Contents><Key>中文//../a+b</Key><Size>3</Size></Contents></ListBucketResult>`)
		case r.Method == "GET" && r.URL.Query().Has("versionId"):
			if r.URL.Query().Get("versionId") != "null" || r.URL.Path != "/bucket/中文//../a+b" {
				t.Errorf("key or version changed: %s", r.URL.String())
			}
			w.Header().Set("ETag", `"opaque-etag"`)
			w.Header().Set("Content-Length", "3")
			w.Header().Set("Content-Range", "bytes 0-2/10")
			w.WriteHeader(206)
			io.WriteString(w, "abc")
		case r.Method == "DELETE":
			w.Header().Set("x-amz-delete-marker", "true")
			w.Header().Set("x-amz-version-id", "created-marker")
			w.WriteHeader(204)
		default:
			w.WriteHeader(500)
			fmt.Fprint(w, `<Error><Code>UnexpectedRequest</Code></Error>`)
		}
	}))
	defer fake.Close()
	payload := map[string]any{"name": "test", "endpoint": fake.URL, "region": "us-east-1", "accessKey": "access", "secretKey": "very-secret", "sessionToken": "temporary-token", "pathStyle": true, "enabled": true, "grants": []string{"*"}}
	raw, _ := json.Marshal(payload)
	response := b.expect("POST", "/api/admin/credentials", string(raw), 200)
	if bytes.Contains(response, []byte("very-secret")) || bytes.Contains(response, []byte("temporary-token")) {
		t.Fatal("secret disclosed")
	}
	var c credential
	if e := json.Unmarshal(response, &c); e != nil {
		t.Fatal(e)
	}
	selectBody := fmt.Sprintf(`{"id":%d}`, c.ID)
	b.expect("POST", "/api/credentials/select", selectBody, 204)
	b.expect("GET", "/api/buckets/bucket/objects?"+url.Values{"prefix": {"中文//../"}, "token": {"opaque+/="}}.Encode(), "", 200)
	downloadPath := "/api/buckets/bucket/objects/download?" + url.Values{"key": {"中文//../a+b"}, "versionId": {"null"}}.Encode()
	req := httptest.NewRequest("GET", downloadPath, nil)
	req.AddCookie(b.cookie)
	req.Header.Set("Range", "bytes=0-2")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 206 || rec.Body.String() != "abc" || rec.Header().Get("ETag") != `"opaque-etag"` {
		t.Fatalf("range download failed: %d %s", rec.Code, rec.Body.String())
	}
	b.expect("DELETE", "/api/buckets/bucket/objects?key=a%2Bb", "", 200)
	b.expect("POST", "/api/admin/users", `{"email":"user@example.com","password":"user-development-password","role":"USER","enabled":true}`, 204)
	userBrowser := &testBrowser{s: s, t: t}
	userBrowser.expect("POST", "/api/login", `{"email":"user@example.com","password":"user-development-password"}`, 200)
	userBrowser.expect("GET", "/api/admin/credentials", "", 403)
	userBrowser.expect("POST", "/api/credentials/select", selectBody, 204)
	// Revocation must take effect for existing sessions, without logging out.
	payload["grants"] = []string{}
	payload["secretKey"] = ""
	payload["sessionToken"] = ""
	raw, _ = json.Marshal(payload)
	b.expect("PUT", fmt.Sprintf("/api/admin/credentials/%d", c.ID), string(raw), 200)
	before := calls.Load()
	userBrowser.expect("GET", "/api/buckets/bucket/objects", "", 403)
	if calls.Load() != before {
		t.Fatal("revoked credential still reached S3")
	}
	b.expect("GET", "/api/audit", "", 200)
	b.expect("POST", "/api/logout", "", 204)
	b.expect("GET", "/api/me", "", 401)
}

func TestRestartPreservesSessionsAndSecrets(t *testing.T) {
	s := testServer(t)
	b := &testBrowser{s: s, t: t}
	b.expect("POST", "/api/login", `{"email":"admin@example.com","password":"development-password"}`, 200)
	cfg := s.cfg
	cfg.AdminPassword = "new-bootstrap-password"
	s2, e := New(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer s2.Close()
	b.s = s2
	b.expect("GET", "/api/me", "", 200)
	b.expect("POST", "/api/login", `{"email":"admin@example.com","password":"new-bootstrap-password"}`, 401)
}

func TestMultipartCopyAndBucketConfiguration(t *testing.T) {
	s := testServer(t)
	b := &testBrowser{s: s, t: t}
	b.expect("POST", "/api/login", `{"email":"admin@example.com","password":"development-password"}`, 200)
	var copied, deleted, completed, aborted atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		w.Header().Set("Content-Type", "application/xml")
		switch {
		case r.Method == "POST" && q.Has("uploads"):
			fmt.Fprint(w, `<InitiateMultipartUploadResult><Bucket>bucket</Bucket><Key>key</Key><UploadId>upload+/=</UploadId></InitiateMultipartUploadResult>`)
		case r.Method == "PUT" && q.Has("partNumber"):
			body, _ := io.ReadAll(r.Body)
			if string(body) != "part-data" {
				t.Error("part bytes changed")
			}
			w.Header().Set("ETag", `"opaque-not-md5"`)
		case r.Method == "POST" && q.Has("uploadId"):
			body, _ := io.ReadAll(r.Body)
			if !bytes.Contains(body, []byte("opaque-not-md5")) {
				t.Error("ETag changed")
			}
			completed.Add(1)
			fmt.Fprint(w, `<CompleteMultipartUploadResult><Bucket>bucket</Bucket><Key>key</Key><ETag>"complete"</ETag></CompleteMultipartUploadResult>`)
		case r.Method == "DELETE" && q.Has("uploadId"):
			aborted.Add(1)
			w.WriteHeader(204)
		case r.Method == "PUT" && q.Has("versioning"):
			if r.URL.Path != "/bucket" {
				t.Error("client-selected bucket bypassed route")
			}
			body, _ := io.ReadAll(r.Body)
			if !bytes.Contains(body, []byte("Enabled")) {
				t.Error("versioning configuration lost")
			}
			w.WriteHeader(200)
		case r.Method == "HEAD":
			w.Header().Set("Content-Length", "3")
			w.Header().Set("ETag", `"source-etag"`)
		case r.Method == "PUT" && r.Header.Get("x-amz-copy-source") != "":
			copied.Add(1)
			if r.Header.Get("x-amz-copy-source") != "bucket%2Fa%2Bb%20%23%3F%25" {
				t.Errorf("copy source encoding: %s", r.Header.Get("x-amz-copy-source"))
			}
			if r.Header.Get("x-amz-copy-source-if-match") != `"source-etag"` {
				t.Error("missing source precondition")
			}
			fmt.Fprint(w, `<CopyObjectResult><ETag>"copy-etag"</ETag></CopyObjectResult>`)
		case r.Method == "DELETE":
			deleted.Add(1)
			w.WriteHeader(403)
			fmt.Fprint(w, `<Error><Code>AccessDenied</Code></Error>`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			w.WriteHeader(500)
		}
	}))
	defer fake.Close()
	body, _ := json.Marshal(map[string]any{"name": "multipart", "endpoint": fake.URL, "region": "us-east-1", "accessKey": "test", "secretKey": "test-secret", "pathStyle": true, "enabled": true, "grants": []string{}})
	response := b.expect("POST", "/api/admin/credentials", string(body), 200)
	var c credential
	json.Unmarshal(response, &c)
	b.expect("POST", "/api/credentials/select", fmt.Sprintf(`{"id":%d}`, c.ID), 204)
	b.expect("POST", "/api/buckets/bucket/multipart/initiate?key=key", "", 200)
	suffix := "?key=key&uploadId=upload%2B%2F%3D"
	b.expect("PUT", "/api/buckets/bucket/multipart/part"+suffix+"&partNumber=1", "part-data", 200)
	b.expect("POST", "/api/buckets/bucket/multipart/complete"+suffix, `{"parts":[{"PartNumber":2,"ETag":"x"},{"PartNumber":1,"ETag":"y"}]}`, 400)
	if completed.Load() != 0 {
		t.Fatal("invalid completion reached S3")
	}
	b.expect("POST", "/api/buckets/bucket/multipart/complete"+suffix, `{"parts":[{"PartNumber":1,"ETag":"\"opaque-not-md5\""}]}`, 200)
	b.expect("DELETE", "/api/buckets/bucket/multipart/abort"+suffix, "", 204)
	b.expect("PUT", "/api/buckets/bucket/config/versioning", `{"Bucket":"other-bucket","VersioningConfiguration":{"Status":"Enabled"}}`, 200)
	b.expect("DELETE", "/api/buckets/bucket/config/versioning", "", 405)
	response = b.expect("POST", "/api/buckets/bucket/objects/copy", `{"key":"a+b #?%","destination":"destination","move":true}`, 409)
	if !bytes.Contains(response, []byte("CopiedButSourceNotDeleted")) {
		t.Fatal("partial move success hidden")
	}
	if copied.Load() != 1 || deleted.Load() != 1 || completed.Load() != 1 || aborted.Load() != 1 {
		t.Fatalf("unexpected operation counts: copy=%d delete=%d complete=%d abort=%d", copied.Load(), deleted.Load(), completed.Load(), aborted.Load())
	}
	cfg := s.cfg
	cfg.EncryptionKey = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))
	if other, e := New(context.Background(), cfg); e == nil {
		other.Close()
		t.Fatal("changed encryption key accepted")
	}
}

func TestPublicSourceAndLicense(t *testing.T) {
	s := testServer(t)
	archive := t.TempDir() + "/source.tar.gz"
	content := []byte("test source archive")
	if e := os.WriteFile(archive, content, 0600); e != nil {
		t.Fatal(e)
	}
	s.cfg.SourceArchive = archive
	s.routes()
	b := &testBrowser{s: s, t: t}
	if !bytes.Equal(b.expect("GET", "/source", "", 200), content) {
		t.Fatal("source unavailable without login")
	}
	if !bytes.Equal(b.expect("GET", "/LICENSE", "", 200), []byte(licenseText)) {
		t.Fatal("license text changed")
	}
	original, e := os.ReadFile("../../LICENSE")
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(original, []byte(licenseText)) {
		t.Fatal("embedded license differs from repository LICENSE")
	}
	b.expect("GET", "/livez", "", 200)
	b.expect("GET", "/healthz", "", 200)
}
