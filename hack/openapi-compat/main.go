// Command openapi-compat is Styr's API-freeze CI guard (card T62): it
// compares two OpenAPI 3.1 documents — the previous release's and the
// working tree's — and fails when the new one changed in a way that would
// break an existing client. Within Styr's 1.x compatibility promise (see
// docs/API.md), the API only changes additively; this tool is what enforces
// that in CI rather than leaving it to review.
//
// A change is breaking when:
//   - a path present in the old document is missing from the new one
//   - a method on a path present in the old document is missing in the new
//     one
//   - a components.schemas entry, or one of its properties, present in the
//     old document is missing from the new one
//   - a property's "type" changed between the two documents
//   - an operation's JSON request body gained a required field it did not
//     require before (a client written against the old document would now
//     fail validation)
//
// Anything else — a new path, a new optional field, a new response code, a
// new enum value, a schema that only grew — is additive and not reported.
//
// Usage:
//
//	openapi-compat <old.yaml> <new.yaml>
//
// Exit status is 0 when the new document is backward compatible with the
// old one (a summary of what was checked is printed to stdout), and 1 when
// at least one breaking change was found (each one printed to stderr).
package main

import (
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, "usage: openapi-compat <old.yaml> <new.yaml>")
		return 2
	}
	oldDoc, err := loadDoc(args[0])
	if err != nil {
		fmt.Fprintf(stderr, "openapi-compat: reading %s: %v\n", args[0], err)
		return 2
	}
	newDoc, err := loadDoc(args[1])
	if err != nil {
		fmt.Fprintf(stderr, "openapi-compat: reading %s: %v\n", args[1], err)
		return 2
	}

	r := compareDocs(oldDoc, newDoc)

	fmt.Fprintf(stdout, "openapi-compat: %s -> %s\n", args[0], args[1])
	fmt.Fprintf(stdout, "  paths/methods checked: %d\n", r.PathsChecked)
	fmt.Fprintf(stdout, "  component schemas checked: %d\n", r.SchemasChecked)
	fmt.Fprintf(stdout, "  request bodies checked: %d\n", r.BodiesChecked)

	if len(r.Breaking) == 0 {
		fmt.Fprintln(stdout, "  OK: no breaking changes")
		return 0
	}

	fmt.Fprintf(stderr, "openapi-compat: %d breaking change(s) found\n", len(r.Breaking))
	for _, b := range r.Breaking {
		fmt.Fprintf(stderr, "  BREAKING: %s\n", b)
	}
	return 1
}

// loadDoc reads and parses path as a generic YAML document. Everything
// downstream works off map[string]any rather than typed OpenAPI structs,
// since this tool only ever looks at a handful of well-known keys
// (paths, components.schemas, requestBody, properties, required, type,
// $ref) and would otherwise have to model the rest of OpenAPI 3.1 for no
// benefit.
func loadDoc(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing yaml: %w", err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}
