package templates

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func loadGrafanaFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/grafana_firing.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return raw
}

func TestNormalizeGrafanaFixture(t *testing.T) {
	vars, err := Normalize("grafana", loadGrafanaFixture(t))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if status, _ := vars["status"].(string); status != "firing" {
		t.Fatalf("status = %v, want firing", vars["status"])
	}
	alerts, ok := vars["alerts"].([]any)
	if !ok || len(alerts) != 1 {
		t.Fatalf("alerts = %#v, want one element", vars["alerts"])
	}
	alert, ok := alerts[0].(map[string]any)
	if !ok {
		t.Fatalf("alert[0] = %#v, want map", alerts[0])
	}
	labels, ok := alert["labels"].(map[string]any)
	if !ok {
		t.Fatalf("alert.labels = %#v, want map", alert["labels"])
	}
	if labels["alertname"] != "SystemdUnitFailed" {
		t.Fatalf("alertname = %v", labels["alertname"])
	}
	if labels["instance"] != "192.168.62.138:9100" {
		t.Fatalf("instance = %v", labels["instance"])
	}
	if alert["fingerprint"] != "a1b2c3d4e5f60718" {
		t.Fatalf("fingerprint = %v", alert["fingerprint"])
	}
	payload, ok := vars["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload = %#v, want map", vars["payload"])
	}
	if payload["status"] != "firing" {
		t.Fatalf("payload.status = %v", payload["status"])
	}
}

func TestNormalizeGrafanaMissingFieldsGetSafeDefaults(t *testing.T) {
	vars, err := Normalize("grafana", []byte(`{"status":"firing"}`))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	alerts, ok := vars["alerts"].([]any)
	if !ok || len(alerts) != 0 {
		t.Fatalf("alerts = %#v, want empty slice", vars["alerts"])
	}
	if _, ok := vars["commonLabels"].(map[string]any); !ok {
		t.Fatalf("commonLabels = %#v, want empty map", vars["commonLabels"])
	}
}

func TestNormalizeGrafanaInvalidJSON(t *testing.T) {
	if _, err := Normalize("grafana", []byte(`not json`)); err == nil {
		t.Fatal("want error for invalid grafana payload")
	}
}

func TestNormalizeGenericDecodedJSON(t *testing.T) {
	vars, err := Normalize("generic", []byte(`{"a":1,"b":"two"}`))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	payload, ok := vars["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload = %#v, want map", vars["payload"])
	}
	if payload["b"] != "two" {
		t.Fatalf("payload.b = %v", payload["b"])
	}
}

func TestNormalizeGenericFallsBackToRawString(t *testing.T) {
	vars, err := Normalize("generic", []byte(`not json at all`))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	payload, ok := vars["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload = %#v, want map", vars["payload"])
	}
	if payload["raw"] != "not json at all" {
		t.Fatalf("payload.raw = %v", payload["raw"])
	}
}

func TestNormalizeGitHubLiftsFields(t *testing.T) {
	vars, err := Normalize("github", SamplePayload("github"))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if vars["action"] != "opened" {
		t.Fatalf("action = %v", vars["action"])
	}
	if vars["repo"] != "jonasthim/styr" {
		t.Fatalf("repo = %v", vars["repo"])
	}
	if vars["sender"] != "jonasthim" {
		t.Fatalf("sender = %v", vars["sender"])
	}
	numF, ok := vars["number"].(float64)
	if !ok || numF != 42 {
		t.Fatalf("number = %#v, want 42", vars["number"])
	}
	if _, ok := vars["payload"].(map[string]any); !ok {
		t.Fatalf("payload = %#v, want map", vars["payload"])
	}
}

func TestNormalizeGitHubIssueNumberFallback(t *testing.T) {
	vars, err := Normalize("github", []byte(`{"action":"closed","issue":{"number":7},"repository":{"full_name":"a/b"},"sender":{"login":"me"}}`))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	numF, ok := vars["number"].(float64)
	if !ok || numF != 7 {
		t.Fatalf("number = %#v, want 7", vars["number"])
	}
}

func TestNormalizeGitHubInvalidJSON(t *testing.T) {
	if _, err := Normalize("github", []byte(`{`)); err == nil {
		t.Fatal("want error for invalid github payload")
	}
}

func TestNormalizeUnknownKind(t *testing.T) {
	if _, err := Normalize("carrier-pigeon", []byte(`{}`)); err == nil {
		t.Fatal("want error for unknown kind")
	}
}

func TestRenderSeededGrafanaPrompt(t *testing.T) {
	vars, err := Normalize("grafana", loadGrafanaFixture(t))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	seeds := Seeded()
	if len(seeds) != 1 {
		t.Fatalf("Seeded() = %d entries, want 1", len(seeds))
	}
	seed := seeds[0]
	if seed.Name != "Grafana alert investigation" || seed.Kind != "grafana" || seed.ProfileID != "investigate" {
		t.Fatalf("unexpected seed: %+v", seed)
	}

	prompt, err := Render(seed.PromptTemplate, vars)
	if err != nil {
		t.Fatalf("Render prompt: %v", err)
	}
	if !strings.Contains(prompt, "SystemdUnitFailed") {
		t.Fatalf("prompt missing alertname:\n%s", prompt)
	}
	if !strings.Contains(prompt, "192.168.62.138:9100") {
		t.Fatalf("prompt missing instance:\n%s", prompt)
	}
	if !strings.Contains(prompt, "firing") {
		t.Fatalf("prompt missing status:\n%s", prompt)
	}

	title, err := Render(seed.TitleTemplate, vars)
	if err != nil {
		t.Fatalf("Render title: %v", err)
	}
	if title != "firing: SystemdUnitFailed" {
		t.Fatalf("title = %q, want %q", title, "firing: SystemdUnitFailed")
	}

	if !json.Valid([]byte(seed.ReportSchema)) {
		t.Fatalf("ReportSchema is not valid JSON: %s", seed.ReportSchema)
	}
}

func TestRenderMissingKeyIsEmptyString(t *testing.T) {
	out, err := Render("[{{ .nope }}][{{ default \"unknown\" .missingLabel }}]", Vars{
		"a":            1,
		"missingLabel": nil,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if out != "[][unknown]" {
		t.Fatalf("out = %q, want %q", out, "[][unknown]")
	}
}

func TestRenderMissingNestedKeyIsEmptyString(t *testing.T) {
	// A single missing leaf key on a map that IS present (the common case
	// for grafana labels/annotations) renders empty, same as a missing
	// top-level key.
	vars := Vars{"labels": map[string]any{"alertname": "X"}}
	out, err := Render("[{{ .labels.instance }}]", vars)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if out != "[]" {
		t.Fatalf("out = %q, want %q", out, "[]")
	}
}

func TestRenderNilVars(t *testing.T) {
	out, err := Render("value=[{{ .missing }}]", nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if out != "value=[]" {
		t.Fatalf("out = %q, want %q", out, "value=[]")
	}
}

func TestRenderSyntaxErrorReturnsError(t *testing.T) {
	_, err := Render("{{ .Foo ", Vars{})
	if err == nil {
		t.Fatal("want error for malformed template")
	}
}

func TestRenderFuncTable(t *testing.T) {
	cases := []struct {
		name string
		tmpl string
		vars Vars
		want string
	}{
		{"lower", `{{ lower "HELLO" }}`, nil, "hello"},
		{"upper", `{{ upper "hello" }}`, nil, "HELLO"},
		{"join strings", `{{ join ", " .list }}`, Vars{"list": []string{"a", "b", "c"}}, "a, b, c"},
		{"join any", `{{ join "-" .list }}`, Vars{"list": []any{"a", 1, true}}, "a-1-true"},
		{"default present", `{{ default "x" .v }}`, Vars{"v": "present"}, "present"},
		{"default missing", `{{ default "x" .v }}`, Vars{}, "x"},
		{"truncate short", `{{ truncate 10 "hi" }}`, nil, "hi"},
		{"truncate long", `{{ truncate 3 "hello" }}`, nil, "hel"},
		{"json", `{{ json .v }}`, Vars{"v": map[string]any{"a": 1}}, `{"a":1}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Render(tc.tmpl, tc.vars)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if out != tc.want {
				t.Fatalf("out = %q, want %q", out, tc.want)
			}
		})
	}
}

func TestRenderNowIsRFC3339(t *testing.T) {
	out, err := Render(`{{ now }}`, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if _, err := time.Parse(time.RFC3339, out); err != nil {
		t.Fatalf("now() output %q not RFC3339: %v", out, err)
	}
}

func TestRenderSha256(t *testing.T) {
	out, err := Render(`{{ sha256 "abc" }}`, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if out != want {
		t.Fatalf("sha256(abc) = %q, want %q", out, want)
	}
}

func TestRenderAlertnames(t *testing.T) {
	vars, err := Normalize("grafana", loadGrafanaFixture(t))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	out, err := Render(`{{ join "," (alertnames .alerts) }}`, vars)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if out != "SystemdUnitFailed" {
		t.Fatalf("out = %q", out)
	}
}

func TestRenderJoinUnsupportedTypeReturnsError(t *testing.T) {
	_, err := Render(`{{ join "," .v }}`, Vars{"v": 42})
	if err == nil {
		t.Fatal("want error, not panic, for join on unsupported type")
	}
}

func TestDefaultDedupeKeyGrafana(t *testing.T) {
	if got := DefaultDedupeKey("grafana"); got == "" {
		t.Fatal("DefaultDedupeKey(grafana) is empty")
	}
	if got := DefaultDedupeKey("generic"); !strings.Contains(got, "sha256") {
		t.Fatalf("DefaultDedupeKey(generic) = %q, want it to use sha256", got)
	}
}

func TestRenderDedupeStabilityGrafana(t *testing.T) {
	vars, err := Normalize("grafana", loadGrafanaFixture(t))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	key1, err := RenderDedupe("", "grafana", vars)
	if err != nil {
		t.Fatalf("RenderDedupe: %v", err)
	}
	key2, err := RenderDedupe("", "grafana", vars)
	if err != nil {
		t.Fatalf("RenderDedupe: %v", err)
	}
	if key1 != key2 {
		t.Fatalf("dedupe key not stable: %q != %q", key1, key2)
	}

	resolvedVars, err := Normalize("grafana", []byte(strings.Replace(string(loadGrafanaFixture(t)), `"status": "firing"`, `"status": "resolved"`, 1)))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	key3, err := RenderDedupe("", "grafana", resolvedVars)
	if err != nil {
		t.Fatalf("RenderDedupe: %v", err)
	}
	if key1 == key3 {
		t.Fatalf("dedupe key did not change with status: %q", key1)
	}
}

func TestRenderDedupeStabilityGeneric(t *testing.T) {
	vars1, err := Normalize("generic", []byte(`{"a":1}`))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	vars2, err := Normalize("generic", []byte(`{"a":1}`))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	key1, err := RenderDedupe("", "generic", vars1)
	if err != nil {
		t.Fatalf("RenderDedupe: %v", err)
	}
	key2, err := RenderDedupe("", "generic", vars2)
	if err != nil {
		t.Fatalf("RenderDedupe: %v", err)
	}
	if key1 != key2 {
		t.Fatalf("dedupe key not stable across identical payloads: %q != %q", key1, key2)
	}
	if len(key1) != 64 {
		t.Fatalf("dedupe key %q not a sha256 hex digest", key1)
	}

	vars3, err := Normalize("generic", []byte(`{"a":2}`))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	key3, err := RenderDedupe("", "generic", vars3)
	if err != nil {
		t.Fatalf("RenderDedupe: %v", err)
	}
	if key1 == key3 {
		t.Fatalf("dedupe key did not change with payload: %q", key1)
	}
}

func TestRenderDedupeCustomTemplate(t *testing.T) {
	key, err := RenderDedupe(`{{ .payload.a }}`, "generic", Vars{"payload": map[string]any{"a": strconv.Itoa(9)}})
	if err != nil {
		t.Fatalf("RenderDedupe: %v", err)
	}
	if key != "9" {
		t.Fatalf("key = %q, want 9", key)
	}
}

func TestSamplePayloadIsValidJSONPerKind(t *testing.T) {
	for _, kind := range []string{"grafana", "github", "generic", "unknown-kind"} {
		t.Run(kind, func(t *testing.T) {
			raw := SamplePayload(kind)
			if len(raw) == 0 {
				t.Fatal("empty sample payload")
			}
			if !json.Valid(raw) {
				t.Fatalf("sample payload for %q is not valid JSON: %s", kind, raw)
			}
		})
	}
}

func TestSampleGrafanaPayloadNormalizes(t *testing.T) {
	vars, err := Normalize("grafana", SamplePayload("grafana"))
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if vars["status"] != "firing" {
		t.Fatalf("status = %v", vars["status"])
	}
}
