// SPDX-License-Identifier: AGPL-3.0-only
package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/expr-lang/expr"
	"github.com/wenisch-tech/s3webui/downstream/src/main/resources"
	"golang.org/x/net/html"
)

// Render the preserved upstream templates directly. This is a deliberately limited
// adapter for their existing Thymeleaf attributes, not a second set of page layouts.
type originalTemplates struct {
	pages     map[string]*html.Node
	fragments map[string]*html.Node
}

func loadOriginalTemplates() (*originalTemplates, error) {
	t := &originalTemplates{pages: map[string]*html.Node{}, fragments: map[string]*html.Node{}}
	e := fs.WalkDir(resources.Files, "rendered", func(name string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() || !strings.HasSuffix(name, ".html") {
			return nil
		}
		b, e := resources.Files.ReadFile(name)
		if e != nil {
			return e
		}
		n, e := html.Parse(bytes.NewReader(b))
		if e != nil {
			return e
		}
		page := strings.TrimSuffix(strings.TrimPrefix(name, "rendered/"), ".html")
		t.pages[page] = n
		var walk func(*html.Node)
		walk = func(n *html.Node) {
			if f := attr(n, "th:fragment"); f != "" {
				f = strings.SplitN(f, "(", 2)[0]
				t.fragments[page+"::"+f] = n
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(n)
		return nil
	})
	return t, e
}
func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}
func putAttr(n *html.Node, name, value string) {
	for i := range n.Attr {
		if n.Attr[i].Key == name {
			n.Attr[i].Val = value
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: name, Val: value})
}
func text(v any) string {
	if v == nil {
		return ""
	}
	if d, ok := v.(time.Time); ok {
		return d.Format(time.RFC3339)
	}
	return fmt.Sprint(v)
}
func truth(v any) bool {
	if v == nil {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	if s, ok := v.(string); ok {
		return s != ""
	}
	return true
}
func empty(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map, reflect.String:
		return r.Len() == 0
	}
	return false
}
func number(v any) float64 { f, _ := strconv.ParseFloat(text(v), 64); return f }
func plus(a, b any) any {
	_, as := a.(string)
	_, bs := b.(string)
	if as || bs {
		return text(a) + text(b)
	}
	return number(a) + number(b)
}
func splitTop(s string, separator rune) []string {
	var out []string
	start, depth := 0, 0
	var quote rune
	escaped := false
	for i, c := range s {
		if quote != 0 {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		switch c {
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			depth--
		}
		if c == separator && depth == 0 {
			out = append(out, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}

var epochRE = regexp.MustCompile(`([\w.]+)\.toEpochMilli\(\)`)
var emptyRE = regexp.MustCompile(`([\w.]+)\.isEmpty\(\)`)
var literalRE = regexp.MustCompile(`'([^'\\]*(?:\\.[^'\\]*)*)'`)

func modelEnv(model map[string]any) map[string]any {
	env := map[string]any{}
	for k, v := range model {
		env[k] = v
	}
	env["plus"] = plus
	env["empty"] = empty
	env["size"] = func(v any) int {
		if v == nil {
			return 0
		}
		return reflect.ValueOf(v).Len()
	}
	env["epoch"] = func(v any) int64 {
		if d, ok := v.(time.Time); ok {
			return d.UnixMilli()
		}
		return 0
	}
	env["date"] = func(v any, format string) string {
		d, ok := v.(time.Time)
		if !ok {
			return ""
		}
		if format == "yyyy-MM-dd" {
			return d.Format("2006-01-02")
		}
		return d.Format("2006-01-02 15:04")
	}
	env["formatSize"] = func(v any) string {
		n := number(v)
		units := []string{"B", "KB", "MB", "GB", "TB"}
		i := 0
		for n >= 1024 && i < 4 {
			n /= 1024
			i++
		}
		if i == 0 {
			return fmt.Sprintf("%.0f B", n)
		}
		return fmt.Sprintf("%.1f %s", n, units[i])
	}
	env["join"] = func(v any, sep string) string {
		a := reflect.ValueOf(v)
		out := []string{}
		for i := 0; i < a.Len(); i++ {
			out = append(out, text(a.Index(i).Interface()))
		}
		return strings.Join(out, sep)
	}
	return env
}
func evaluate(raw string, model map[string]any) (any, error) {
	raw = strings.TrimSpace(raw)
	if model["locale"] == "zh" {
		raw = literalRE.ReplaceAllStringFunc(raw, func(quoted string) string {
			value := quoted[1 : len(quoted)-1]
			switch value {
			case " bucket", " buckets":
				value = " 个存储桶"
			case " item", " items", " item(s)":
				value = " 个对象"
			case " result", " results":
				value = " 个结果"
			default:
				value = originalTranslate(value, "zh")
			}
			return "'" + strings.ReplaceAll(value, "'", "\\'") + "'"
		})
	}
	if raw == "null" {
		return nil, nil
	}
	if raw == "loading" {
		return "loading", nil
	}
	// URL expressions may appear inside a conditional. Resolve each to a quoted literal first.
	for {
		p := strings.Index(raw, "@{")
		if p < 0 {
			break
		}
		depth := 1
		end := p + 2
		for ; end < len(raw); end++ {
			if raw[end] == '{' {
				depth++
			}
			if raw[end] == '}' {
				depth--
				if depth == 0 {
					break
				}
			}
		}
		if end == len(raw) {
			return nil, fmt.Errorf("unclosed URL: %s", raw)
		}
		u, e := templateURL(raw[p+2:end], model)
		if e != nil {
			return nil, e
		}
		encoded, _ := json.Marshal(u)
		raw = raw[:p] + string(encoded) + raw[end+1:]
	}
	raw = strings.ReplaceAll(strings.ReplaceAll(raw, "${", "("), "}", ")")
	raw = strings.NewReplacer("#lists.isEmpty", "empty", "#lists.size", "size", "#strings.isEmpty", "empty", "#strings.listJoin", "join", "#temporals.format", "date", "@fileSizeFormatter.format", "formatSize", ".name()", "").Replace(raw)
	raw = epochRE.ReplaceAllString(raw, "epoch($1)")
	raw = emptyRE.ReplaceAllString(raw, "empty($1)")
	if len(splitTop(raw, '?')) > 1 && len(splitTop(raw, ':')) == 1 {
		raw += " : ''"
	}
	env := modelEnv(model)
	program, e := expr.Compile(raw, expr.Env(env), expr.AllowUndefinedVariables(), expr.Operator("+", "plus"))
	if e != nil {
		return nil, fmt.Errorf("template expression %q: %w", raw, e)
	}
	v, e := expr.Run(program, env)
	if e != nil {
		return nil, fmt.Errorf("template expression %q: %w", raw, e)
	}
	return v, nil
}
func templateURL(raw string, model map[string]any) (string, error) {
	pathPart := raw
	params := ""
	if p := strings.Index(raw, "("); p >= 0 && strings.HasSuffix(raw, ")") {
		pathPart = raw[:p]
		params = raw[p+1 : len(raw)-1]
	}
	if strings.HasPrefix(pathPart, "${") {
		v, e := evaluate(pathPart, model)
		return text(v), e
	}
	q := url.Values{}
	for _, entry := range splitTop(params, ',') {
		if entry == "" {
			continue
		}
		p := strings.SplitN(entry, "=", 2)
		if len(p) != 2 {
			return "", fmt.Errorf("invalid URL parameter: %s", entry)
		}
		v, e := evaluate(p[1], model)
		if e != nil {
			return "", e
		}
		placeholder := "{" + p[0] + "}"
		if strings.Contains(pathPart, placeholder) {
			pathPart = strings.ReplaceAll(pathPart, placeholder, url.PathEscape(text(v)))
		} else {
			q.Set(p[0], text(v))
		}
	}
	if len(q) > 0 {
		pathPart += "?" + q.Encode()
	}
	return pathPart, nil
}
func (t *originalTemplates) render(page string, model map[string]any) ([]byte, error) {
	root, ok := t.pages[page]
	if !ok {
		return nil, fmt.Errorf("unknown page: %s", page)
	}
	out := &html.Node{Type: html.DocumentNode}
	if e := t.node(root, out, model, false); e != nil {
		return nil, e
	}
	var b bytes.Buffer
	for c := out.FirstChild; c != nil; c = c.NextSibling {
		if e := html.Render(&b, c); e != nil {
			return nil, e
		}
	}
	return b.Bytes(), nil
}

var inlineModelRE = regexp.MustCompile(`/\*\[\[(\$\{[^\n]*?\})\]\]\*/\s*(?:''|\[\]|true|false)`)

func (t *originalTemplates) node(n, parent *html.Node, model map[string]any, skipEach bool) error {
	if n.Type == html.DocumentNode {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if e := t.node(c, parent, model, false); e != nil {
				return e
			}
		}
		return nil
	}
	if each := attr(n, "th:each"); each != "" && !skipEach {
		p := strings.SplitN(each, ":", 2)
		if len(p) != 2 {
			return fmt.Errorf("invalid each: %s", each)
		}
		v, e := evaluate(p[1], model)
		if e != nil {
			return e
		}
		if v == nil {
			return nil
		}
		list := reflect.ValueOf(v)
		for i := 0; i < list.Len(); i++ {
			local := map[string]any{}
			for k, v := range model {
				local[k] = v
			}
			local[strings.TrimSpace(p[0])] = list.Index(i).Interface()
			if e := t.node(n, parent, local, true); e != nil {
				return e
			}
		}
		return nil
	}
	for _, condition := range []string{"th:if", "th:unless"} {
		if raw := attr(n, condition); raw != "" {
			v, e := evaluate(raw, model)
			if e != nil {
				return e
			}
			show := truth(v)
			if condition == "th:unless" {
				show = !show
			}
			if !show {
				return nil
			}
		}
	}
	if a := attr(n, "sec:authorize"); a != "" && !truth(model["authenticated"]) {
		return nil
	}
	if replacement := attr(n, "th:replace"); replacement != "" {
		raw := strings.TrimSuffix(strings.TrimPrefix(replacement, "~{"), "}")
		parts := strings.SplitN(raw, "::", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid fragment: %s", replacement)
		}
		file, name := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		local := map[string]any{}
		for k, v := range model {
			local[k] = v
		}
		if p := strings.Index(name, "("); p >= 0 {
			argument := name[p+1 : len(name)-1]
			name = name[:p]
			v, e := evaluate(argument, model)
			if e != nil {
				return e
			}
			param := "title"
			if name == "topbar" {
				param = "active"
			}
			local[param] = v
		}
		fragment, ok := t.fragments[file+"::"+name]
		if !ok {
			return fmt.Errorf("missing fragment %s::%s", file, name)
		}
		return t.node(fragment, parent, local, false)
	}
	if n.Type == html.ElementNode && n.Data == "th:block" {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if e := t.node(c, parent, model, false); e != nil {
				return e
			}
		}
		return nil
	}
	copy := &html.Node{Type: n.Type, DataAtom: n.DataAtom, Data: n.Data, Namespace: n.Namespace}
	for _, a := range n.Attr {
		if !strings.HasPrefix(a.Key, "th:") && !strings.HasPrefix(a.Key, "sec:") && !strings.HasPrefix(a.Key, "xmlns:") {
			copy.Attr = append(copy.Attr, a)
		}
	}
	parent.AppendChild(copy)
	if raw := attr(n, "th:selected"); raw != "" {
		v, e := evaluate(raw, model)
		if e != nil {
			return e
		}
		if truth(v) {
			putAttr(copy, "selected", "")
		}
	}
	if copy.Type == html.TextNode && (n.Parent == nil || (n.Parent.Data != "script" && n.Parent.Data != "style")) {
		copy.Data = originalTranslate(copy.Data, text(model["locale"]))
	}
	if copy.Type == html.ElementNode {
		if copy.Data == "html" {
			putAttr(copy, "lang", text(model["locale"]))
		}
		for i := range copy.Attr {
			if copy.Attr[i].Key == "title" || copy.Attr[i].Key == "aria-label" || copy.Attr[i].Key == "placeholder" || copy.Attr[i].Key == "alt" {
				copy.Attr[i].Val = originalTranslate(copy.Attr[i].Val, text(model["locale"]))
			}
		}
	}
	for _, name := range []string{"href", "src", "action", "value", "content", "classappend"} {
		if raw := attr(n, "th:"+name); raw != "" {
			v, e := evaluate(raw, model)
			if e != nil {
				return e
			}
			if name == "classappend" {
				putAttr(copy, "class", attr(copy, "class")+" "+text(v))
			} else {
				putAttr(copy, name, text(v))
			}
		}
	}
	if raw := attr(n, "th:attr"); raw != "" {
		for _, part := range splitTop(raw, ',') {
			p := strings.SplitN(part, "=", 2)
			if len(p) != 2 {
				return fmt.Errorf("invalid attribute %s", part)
			}
			v, e := evaluate(p[1], model)
			if e != nil {
				return e
			}
			putAttr(copy, p[0], text(v))
		}
	}
	if raw := attr(n, "th:text"); raw != "" {
		v, e := evaluate(raw, model)
		if e != nil {
			return e
		}
		value := text(v)
		if copy.Data == "title" {
			parts := strings.SplitN(value, " · ", 2)
			parts[0] = originalTranslate(parts[0], text(model["locale"]))
			value = strings.Join(parts, " · ")
		}
		copy.AppendChild(&html.Node{Type: html.TextNode, Data: value})
		return nil
	}
	if attr(n, "sec:authentication") != "" {
		copy.AppendChild(&html.Node{Type: html.TextNode, Data: text(model["username"])})
		return nil
	}
	if n.Type == html.TextNode && n.Parent != nil && n.Parent.Data == "script" {
		var inlineErr error
		copy.Data = inlineModelRE.ReplaceAllStringFunc(n.Data, func(match string) string {
			raw := inlineModelRE.FindStringSubmatch(match)[1]
			v, e := evaluate(raw, model)
			if e != nil {
				inlineErr = e
				return "null"
			}
			b, _ := json.Marshal(v)
			return string(b)
		})
		return inlineErr
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if e := t.node(c, copy, model, false); e != nil {
			return e
		}
	}
	return nil
}
