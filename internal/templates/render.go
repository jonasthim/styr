package templates

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"text/template"
	"time"
)

// Render executes tmpl (Go text/template syntax) against vars and returns
// the result. It never panics: parse errors and execution errors are both
// returned as an error. A nil vars map, and any lookup of a key missing from
// a map in vars, renders as an empty string rather than "<no value>" or
// "<nil>" (Option("missingkey=zero") plus a cleanup pass, since Go's default
// zero value for an interface{} map element still prints as "<nil>").
func Render(tmpl string, vars Vars) (string, error) {
	t, err := template.New("styr").Option("missingkey=zero").Funcs(funcMap()).Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("templates: parse template: %w", err)
	}
	if vars == nil {
		vars = Vars{}
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("templates: render template: %w", err)
	}
	out := buf.String()
	out = strings.ReplaceAll(out, "<no value>", "")
	out = strings.ReplaceAll(out, "<nil>", "")
	return out, nil
}

// funcMap is the function table available to every template rendered by
// this package: prompts, titles and dedupe keys alike.
func funcMap() template.FuncMap {
	return template.FuncMap{
		"lower":      strings.ToLower,
		"upper":      strings.ToUpper,
		"join":       joinFunc,
		"default":    defaultFunc,
		"truncate":   truncateFunc,
		"json":       jsonFunc,
		"now":        nowFunc,
		"sha256":     sha256Func,
		"alertnames": alertnamesFunc,
	}
}

// joinFunc joins list with sep. list may be []string (e.g. the result of
// alertnames) or []any (e.g. a JSON array decoded generically), so it
// covers both payload-derived and helper-derived lists.
func joinFunc(sep string, list any) (string, error) {
	switch v := list.(type) {
	case nil:
		return "", nil
	case []string:
		return strings.Join(v, sep), nil
	case []any:
		parts := make([]string, len(v))
		for i, item := range v {
			parts[i] = fmt.Sprint(item)
		}
		return strings.Join(parts, sep), nil
	default:
		return "", fmt.Errorf("templates: join: unsupported list type %T", list)
	}
}

// defaultFunc returns fallback when value is nil or the zero value for its
// type (empty string, empty slice/map, nil pointer), otherwise value.
func defaultFunc(fallback, value any) any {
	if isEmptyValue(value) {
		return fallback
	}
	return value
}

func isEmptyValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String, reflect.Array, reflect.Map, reflect.Slice:
		return rv.Len() == 0
	case reflect.Pointer, reflect.Interface:
		return rv.IsNil()
	default:
		return false
	}
}

// truncateFunc returns the first n runes of s (or s unchanged when it is
// already n runes or shorter). A negative n is treated as zero.
func truncateFunc(n int, s string) string {
	if n < 0 {
		n = 0
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func jsonFunc(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("templates: json: %w", err)
	}
	return string(b), nil
}

// nowFunc returns the current time formatted RFC3339, in UTC.
func nowFunc() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// sha256Func returns the hex-encoded SHA-256 digest of s, used to build a
// stable dedupe key from a hashed payload (see DefaultDedupeKey).
func sha256Func(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// alertnamesFunc extracts labels.alertname from each element of alerts
// (the Normalize "alerts" field: []any of map[string]any, or []map[string]any
// built directly by a caller), skipping any entry missing a string
// alertname label. It never errors or panics on unexpected shapes.
func alertnamesFunc(alerts any) []string {
	var out []string
	switch v := alerts.(type) {
	case []any:
		for _, item := range v {
			if name, ok := alertnameOf(item); ok {
				out = append(out, name)
			}
		}
	case []map[string]any:
		for _, item := range v {
			if name, ok := alertnameOf(item); ok {
				out = append(out, name)
			}
		}
	}
	return out
}

func alertnameOf(item any) (string, bool) {
	m, ok := item.(map[string]any)
	if !ok {
		return "", false
	}
	labels, ok := m["labels"].(map[string]any)
	if !ok {
		return "", false
	}
	name, ok := labels["alertname"].(string)
	return name, ok
}
