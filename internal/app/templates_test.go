// SPDX-License-Identifier: AGPL-3.0-only
package app

import (
	"strings"
	"testing"
	"time"
)

func TestPreservedOriginalTemplates(t *testing.T) {
	templates, e := loadOriginalTemplates()
	if e != nil {
		t.Fatal(e)
	}
	model := map[string]any{"locale": "en", "basePath": "", "appVersion": "test", "sourceURL": "https://example.com/source", "licensePageUrl": "/LICENSE", "releasePageUrl": "https://example.com/releases", "isAdmin": true, "authenticated": true, "username": "user@example.com", "authenticationDisabled": false, "activeCredentialName": "test key", "s3BreadcrumbLabel": "test key", "oidcEnabled": false, "oidcProviders": []any{}, "oidcRequiredRole": "", "error": nil, "loginError": false, "loggedOut": false, "oidcError": false, "bucket": "test-bucket", "prefix": "中文//../", "highlight": "", "breadcrumbs": []map[string]any{{"name": "中文", "prefix": "中文/"}}, "objects": []map[string]any{{"name": "<img>.txt", "key": "中文//../<img>.txt", "directory": false, "size": 1024, "lastModified": time.Now()}}, "buckets": []map[string]any{{"name": "test-bucket", "creationDate": time.Now()}}, "userRoles": []string{"ADMIN", "USER"}, "grantTypes": []string{"USER", "ROLE", "ALL_AUTHENTICATED"}, "query": "test", "currentPage": 1, "pageCount": 1, "searchResults": map[string]any{"total": 1, "incomplete": false, "results": []map[string]any{{"bucket": "test-bucket", "key": "test.txt", "name": "test.txt", "type": "FILE", "size": 1, "lastModified": time.Now(), "parentPrefix": ""}}}}
	for _, page := range []string{"login", "buckets", "bucket", "history", "search", "admin/settings"} {
		t.Run(page, func(t *testing.T) {
			b, e := templates.render(page, model)
			if e != nil {
				t.Fatal(e)
			}
			if strings.Contains(string(b), " th:") {
				t.Fatal("unresolved template directive")
			}
			if page == "bucket" && !strings.Contains(string(b), "&lt;img&gt;.txt") {
				t.Fatal("object key not safely escaped")
			}
			if page != "login" && !strings.Contains(string(b), `class="app-topbar"`) {
				t.Fatal("original topbar missing")
			}
		})
	}
	model["locale"] = "zh"
	for _, page := range []string{"login", "buckets", "bucket", "history", "search", "admin/settings"} {
		t.Run(page+"-zh", func(t *testing.T) {
			b, e := templates.render(page, model)
			if e != nil {
				t.Fatal(e)
			}
			if !strings.Contains(string(b), `lang="zh"`) {
				t.Fatal("language not applied")
			}
			if page == "bucket" && (!strings.Contains(string(b), "&lt;img&gt;.txt") || !strings.Contains(string(b), "个对象")) {
				t.Fatal("data was translated or count is not localized")
			}
		})
	}
}
