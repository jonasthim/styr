package main

import (
	"fmt"
	"sort"
	"strings"
)

// httpMethods are the OpenAPI operation keys this tool looks for under each
// path item. Anything else (parameters, a $ref, summary, ...) is ignored.
var httpMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

// report is the outcome of comparing an old and a new OpenAPI document:
// every breaking change found, plus a few counts for the printed summary.
type report struct {
	Breaking       []string
	PathsChecked   int
	SchemasChecked int
	BodiesChecked  int
}

func (r *report) breakingf(format string, args ...any) {
	r.Breaking = append(r.Breaking, fmt.Sprintf(format, args...))
}

// compareDocs diffs old against new and returns every breaking change it
// finds: a path or method removed, a schema property removed, a property's
// type changed, or a new required field added to a request body schema
// that already existed. Anything else — a new path, a new optional field,
// a new response, a new enum value, a schema that only grew — is additive
// and not reported.
func compareDocs(oldDoc, newDoc map[string]any) *report {
	r := &report{}

	oldPaths := pathItems(oldDoc)
	newPaths := pathItems(newDoc)
	comparePaths(r, oldPaths, newPaths)

	oldSchemas := componentSchemas(oldDoc)
	newSchemas := componentSchemas(newDoc)
	compareComponentSchemas(r, oldSchemas, newSchemas)

	compareRequestBodies(r, oldPaths, newPaths, oldSchemas, newSchemas)

	return r
}

// pathItems returns doc's "paths" map, or an empty map when the document has
// none (a malformed or minimal fixture, not itself an error here).
func pathItems(doc map[string]any) map[string]any {
	paths, _ := doc["paths"].(map[string]any)
	if paths == nil {
		return map[string]any{}
	}
	return paths
}

// componentSchemas returns doc's "components.schemas" map, or an empty map.
func componentSchemas(doc map[string]any) map[string]any {
	components, _ := doc["components"].(map[string]any)
	if components == nil {
		return map[string]any{}
	}
	schemas, _ := components["schemas"].(map[string]any)
	if schemas == nil {
		return map[string]any{}
	}
	return schemas
}

// comparePaths fails on any path present in old whose method is missing in
// new, or whose path is missing entirely from new.
func comparePaths(r *report, oldPaths, newPaths map[string]any) {
	for _, path := range sortedKeys(oldPaths) {
		oldItem, _ := oldPaths[path].(map[string]any)
		newItem, hasPath := newPaths[path].(map[string]any)
		for _, method := range httpMethods {
			if _, ok := oldItem[method]; !ok {
				continue
			}
			r.PathsChecked++
			if !hasPath {
				r.breakingf("removed path: %s %s", strings.ToUpper(method), path)
				continue
			}
			if _, ok := newItem[method]; !ok {
				r.breakingf("removed operation: %s %s", strings.ToUpper(method), path)
			}
		}
	}
}

// compareComponentSchemas fails on any components.schemas entry removed
// outright, or any property of it that was removed or changed type.
func compareComponentSchemas(r *report, oldSchemas, newSchemas map[string]any) {
	for _, name := range sortedKeys(oldSchemas) {
		oldSchema, _ := oldSchemas[name].(map[string]any)
		r.SchemasChecked++
		newSchema, ok := newSchemas[name].(map[string]any)
		if !ok {
			r.breakingf("removed schema: components.schemas.%s", name)
			continue
		}
		comparePropertiesAndTypes(r, "schema "+name, oldSchema, newSchema, newSchemas)
	}
}

// compareRequestBodies fails when an operation that exists in both old and
// new had a required field added to its JSON request body, or lost a
// property, or a property's type changed there. A request body that
// referenced a components.schemas entry is resolved through it first, so
// e.g. POST /templates's TemplateInput is checked here too, on top of the
// standalone schema check above.
func compareRequestBodies(r *report, oldPaths, newPaths map[string]any, oldSchemas, newSchemas map[string]any) {
	for _, path := range sortedKeys(oldPaths) {
		oldItem, _ := oldPaths[path].(map[string]any)
		newItem, _ := newPaths[path].(map[string]any)
		for _, method := range httpMethods {
			oldOp, ok := oldItem[method].(map[string]any)
			if !ok {
				continue
			}
			newOp, ok := newItem[method].(map[string]any)
			if !ok {
				continue // path/method removal already reported by comparePaths
			}
			oldBody := requestBodySchema(oldOp, oldSchemas)
			if oldBody == nil {
				continue
			}
			newBody := requestBodySchema(newOp, newSchemas)
			if newBody == nil {
				continue // the body itself was dropped; not one of the checks this tool does
			}
			r.BodiesChecked++
			ctx := fmt.Sprintf("request body of %s %s", strings.ToUpper(method), path)

			oldRequired := stringSet(oldBody["required"])
			newRequired := stringSet(newBody["required"])
			for _, name := range sortedKeys(newRequired) {
				if _, hadBefore := oldRequired[name]; !hadBefore {
					r.breakingf("%s: new required field %q", ctx, name)
				}
			}

			comparePropertiesAndTypes(r, ctx, oldBody, newBody, newSchemas)
		}
	}
}

// requestBodySchema returns op's application/json request body schema,
// resolved through a $ref when it has one, or nil when op has no JSON
// request body at all.
func requestBodySchema(op map[string]any, schemas map[string]any) map[string]any {
	body, _ := op["requestBody"].(map[string]any)
	if body == nil {
		return nil
	}
	content, _ := body["content"].(map[string]any)
	if content == nil {
		return nil
	}
	media, _ := content["application/json"].(map[string]any)
	if media == nil {
		return nil
	}
	schema, _ := media["schema"].(map[string]any)
	if schema == nil {
		return nil
	}
	return resolveSchema(schema, schemas)
}

// resolveSchema follows a single "$ref: #/components/schemas/Name"
// indirection; anything else (an inline schema, or a $ref this tool does
// not understand) is returned unchanged.
func resolveSchema(schema map[string]any, schemas map[string]any) map[string]any {
	ref, ok := schema["$ref"].(string)
	if !ok {
		return schema
	}
	const prefix = "#/components/schemas/"
	if !strings.HasPrefix(ref, prefix) {
		return schema
	}
	name := strings.TrimPrefix(ref, prefix)
	resolved, ok := schemas[name].(map[string]any)
	if !ok {
		return schema
	}
	return resolved
}

// comparePropertiesAndTypes fails on any property.properties entry present
// in old but missing from new, and on any property present in both whose
// normalized "type" differs. It does not recurse into nested object
// properties — one level is enough to catch the changes this tool guards
// against, and keeps the tool simple to reason about.
func comparePropertiesAndTypes(r *report, ctx string, oldSchema, newSchema map[string]any, schemas map[string]any) {
	oldProps, _ := oldSchema["properties"].(map[string]any)
	newProps, _ := newSchema["properties"].(map[string]any)
	for _, name := range sortedKeys(oldProps) {
		oldProp, _ := oldProps[name].(map[string]any)
		newPropRaw, ok := newProps[name]
		if !ok {
			r.breakingf("%s: removed property %q", ctx, name)
			continue
		}
		newProp, _ := newPropRaw.(map[string]any)

		oldType := typeSignature(oldProp, schemas)
		newType := typeSignature(newProp, schemas)
		if oldType != "" && newType != "" && oldType != newType {
			r.breakingf("%s: property %q changed type from %q to %q", ctx, name, oldType, newType)
		}
	}
}

// typeSignature normalizes a property's "type" (a bare string, e.g.
// "string", or a nullable union like ["string", "null"]) into a stable,
// comparable string. A property with no "type" at all (a bare $ref, or an
// allOf/oneOf composition) resolves through one $ref indirection and
// otherwise returns "" — "unknown, don't flag it" rather than a false
// positive.
func typeSignature(prop map[string]any, schemas map[string]any) string {
	if prop == nil {
		return ""
	}
	prop = resolveSchema(prop, schemas)
	switch t := prop["type"].(type) {
	case string:
		return t
	case []any:
		parts := make([]string, 0, len(t))
		for _, v := range t {
			if s, ok := v.(string); ok {
				parts = append(parts, s)
			}
		}
		sort.Strings(parts)
		return strings.Join(parts, ",")
	default:
		return ""
	}
}

// stringSet turns a YAML "required: [a, b]" list (decoded as []any of
// strings) into a set for membership checks; a missing or malformed list
// is an empty set.
func stringSet(v any) map[string]struct{} {
	out := map[string]struct{}{}
	items, _ := v.([]any)
	for _, item := range items {
		if s, ok := item.(string); ok {
			out[s] = struct{}{}
		}
	}
	return out
}

// sortedKeys returns m's keys (string- or struct{}-valued maps) in sorted
// order, so report output is deterministic.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
