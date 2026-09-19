package main

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// parseDoc is the test-only equivalent of loadDoc, for an inline fixture
// string instead of a file on disk.
func parseDoc(t *testing.T, src string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatalf("parsing fixture: %v", err)
	}
	return doc
}

func containsBreaking(r *report, substr string) bool {
	for _, b := range r.Breaking {
		if strings.Contains(b, substr) {
			return true
		}
	}
	return false
}

const baseFixture = `
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses:
        "200":
          description: OK
    post:
      operationId: createWidget
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                name:
                  type: string
                color:
                  type: string
              required: [name]
      responses:
        "201":
          description: Created
  /widgets/{id}:
    get:
      operationId: getWidget
      responses:
        "200":
          description: OK
components:
  schemas:
    Widget:
      type: object
      properties:
        id:
          type: string
        name:
          type: string
        count:
          type: integer
      required: [id, name]
`

func TestCompareDocs_IdenticalDocsAreCompatible(t *testing.T) {
	old := parseDoc(t, baseFixture)
	newer := parseDoc(t, baseFixture)

	r := compareDocs(old, newer)
	if len(r.Breaking) != 0 {
		t.Fatalf("identical docs reported breaking changes: %v", r.Breaking)
	}
	if r.PathsChecked == 0 || r.SchemasChecked == 0 || r.BodiesChecked == 0 {
		t.Fatalf("expected non-zero counts, got %+v", r)
	}
}

func TestCompareDocs_AdditiveChangesAreOK(t *testing.T) {
	old := parseDoc(t, baseFixture)
	newer := parseDoc(t, `
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses:
        "200":
          description: OK
    post:
      operationId: createWidget
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                name:
                  type: string
                color:
                  type: string
                size:
                  type: string
              required: [name]
      responses:
        "201":
          description: Created
  /widgets/{id}:
    get:
      operationId: getWidget
      responses:
        "200":
          description: OK
  /widgets/{id}/archive:
    post:
      operationId: archiveWidget
      responses:
        "202":
          description: Accepted
components:
  schemas:
    Widget:
      type: object
      properties:
        id:
          type: string
        name:
          type: string
        count:
          type: integer
        archived:
          type: boolean
      required: [id, name]
`)

	r := compareDocs(old, newer)
	if len(r.Breaking) != 0 {
		t.Fatalf("additive-only changes reported as breaking: %v", r.Breaking)
	}
}

func TestCompareDocs_RemovedPathIsBreaking(t *testing.T) {
	old := parseDoc(t, baseFixture)
	newer := parseDoc(t, `
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses:
        "200":
          description: OK
    post:
      operationId: createWidget
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                name:
                  type: string
              required: [name]
      responses:
        "201":
          description: Created
components:
  schemas:
    Widget:
      type: object
      properties:
        id:
          type: string
        name:
          type: string
        count:
          type: integer
      required: [id, name]
`)

	r := compareDocs(old, newer)
	if !containsBreaking(r, "removed path: GET /widgets/{id}") {
		t.Fatalf("expected a removed-path breaking change, got: %v", r.Breaking)
	}
}

func TestCompareDocs_RemovedMethodIsBreaking(t *testing.T) {
	old := parseDoc(t, baseFixture)
	newer := parseDoc(t, strings.Replace(baseFixture, "    post:", "    delete:", 1))

	r := compareDocs(old, newer)
	if !containsBreaking(r, "removed operation: POST /widgets") {
		t.Fatalf("expected a removed-operation breaking change, got: %v", r.Breaking)
	}
}

func TestCompareDocs_RemovedSchemaPropertyIsBreaking(t *testing.T) {
	old := parseDoc(t, baseFixture)
	newer := parseDoc(t, `
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses:
        "200":
          description: OK
    post:
      operationId: createWidget
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                name:
                  type: string
                color:
                  type: string
              required: [name]
      responses:
        "201":
          description: Created
  /widgets/{id}:
    get:
      operationId: getWidget
      responses:
        "200":
          description: OK
components:
  schemas:
    Widget:
      type: object
      properties:
        id:
          type: string
        name:
          type: string
      required: [id, name]
`)

	r := compareDocs(old, newer)
	if !containsBreaking(r, `schema Widget: removed property "count"`) {
		t.Fatalf("expected a removed-property breaking change, got: %v", r.Breaking)
	}
}

func TestCompareDocs_NewRequiredRequestBodyFieldIsBreaking(t *testing.T) {
	old := parseDoc(t, baseFixture)
	newer := parseDoc(t, `
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses:
        "200":
          description: OK
    post:
      operationId: createWidget
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                name:
                  type: string
                color:
                  type: string
              required: [name, color]
      responses:
        "201":
          description: Created
  /widgets/{id}:
    get:
      operationId: getWidget
      responses:
        "200":
          description: OK
components:
  schemas:
    Widget:
      type: object
      properties:
        id:
          type: string
        name:
          type: string
        count:
          type: integer
      required: [id, name]
`)

	r := compareDocs(old, newer)
	if !containsBreaking(r, `request body of POST /widgets: new required field "color"`) {
		t.Fatalf("expected a new-required-field breaking change, got: %v", r.Breaking)
	}
}

func TestCompareDocs_PropertyTypeChangeIsBreaking(t *testing.T) {
	old := parseDoc(t, baseFixture)
	newer := parseDoc(t, `
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses:
        "200":
          description: OK
    post:
      operationId: createWidget
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                name:
                  type: string
                color:
                  type: string
              required: [name]
      responses:
        "201":
          description: Created
  /widgets/{id}:
    get:
      operationId: getWidget
      responses:
        "200":
          description: OK
components:
  schemas:
    Widget:
      type: object
      properties:
        id:
          type: string
        name:
          type: string
        count:
          type: string
      required: [id, name]
`)

	r := compareDocs(old, newer)
	if !containsBreaking(r, `schema Widget: property "count" changed type from "integer" to "string"`) {
		t.Fatalf("expected a type-change breaking change, got: %v", r.Breaking)
	}
}

func TestCompareDocs_RefRequestBodySchemaIsChecked(t *testing.T) {
	old := parseDoc(t, `
paths:
  /gadgets:
    post:
      operationId: createGadget
      requestBody:
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/GadgetInput"
      responses:
        "201":
          description: Created
components:
  schemas:
    GadgetInput:
      type: object
      properties:
        name:
          type: string
      required: [name]
`)
	newer := parseDoc(t, `
paths:
  /gadgets:
    post:
      operationId: createGadget
      requestBody:
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/GadgetInput"
      responses:
        "201":
          description: Created
components:
  schemas:
    GadgetInput:
      type: object
      properties:
        name:
          type: string
        kind:
          type: string
      required: [name, kind]
`)

	r := compareDocs(old, newer)
	if !containsBreaking(r, `new required field "kind"`) {
		t.Fatalf("expected the $ref'd request body's new required field to be caught, got: %v", r.Breaking)
	}
}

func TestCompareDocs_NullableUnionTypeIsNotFlagged(t *testing.T) {
	old := parseDoc(t, `
paths: {}
components:
  schemas:
    Thing:
      type: object
      properties:
        maybe:
          type: [string, "null"]
`)
	newer := parseDoc(t, `
paths: {}
components:
  schemas:
    Thing:
      type: object
      properties:
        maybe:
          type: ["null", string]
`)

	r := compareDocs(old, newer)
	if len(r.Breaking) != 0 {
		t.Fatalf("a reordered nullable union should not be flagged as a type change, got: %v", r.Breaking)
	}
}
