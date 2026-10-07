package stacks

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Controls of 005 T034, beyond T033's cases: the manifest is the trust boundary of every stack, and
// its names become paths, state keys, bucket names and tags. Every free string is constrained, so
// a name that could leave its directory, collide after joining with "-" or break a bucket name is
// refused before anything is derived from it. Each case is one change to the sandbox fixture.

func sandboxDocument(t *testing.T) map[string]any {
	t.Helper()
	var kept []string
	for _, line := range strings.Split(string(readManifestFixture(t, "sandbox/deployments.yaml")), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			kept = append(kept, line)
		}
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(strings.Join(kept, "\n")), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func obj(v any, key string) map[string]any { return v.(map[string]any)[key].(map[string]any) }
func arr(v any, key string) []any          { return v.(map[string]any)[key].([]any) }

func sandboxSpec(d map[string]any) map[string]any { return obj(d, "spec") }
func sandboxEnv(d map[string]any) map[string]any {
	return arr(arr(sandboxSpec(d), "tenants")[0], "environments")[0].(map[string]any)
}
func sandboxRegion(d map[string]any) map[string]any {
	return arr(sandboxEnv(d), "regions")[0].(map[string]any)
}
func sandboxRow(d map[string]any, i int) map[string]any {
	return arr(sandboxSpec(d), "instances")[i].(map[string]any)
}

func TestManifestRefusesUnsafeValues(t *testing.T) {
	cases := []struct {
		name, code string
		change     func(d map[string]any)
	}{
		{"tenant name leaves its directory", CodeInvalidValue, func(d map[string]any) {
			arr(sandboxSpec(d), "tenants")[0].(map[string]any)["name"] = "../x"
		}},
		{"tenant name with a hyphen collides when joined", CodeInvalidValue, func(d map[string]any) {
			arr(sandboxSpec(d), "tenants")[0].(map[string]any)["name"] = "de-mo"
		}},
		{"environment name upper case", CodeInvalidValue, func(d map[string]any) { sandboxEnv(d)["name"] = "Dev" }},
		{"row tenant alone with a slash", CodeInvalidValue, func(d map[string]any) { sandboxRow(d, 2)["tenant"] = "de/mo" }},
		{"row environment alone with a dot", CodeInvalidValue, func(d map[string]any) { sandboxRow(d, 3)["environment"] = "d.v" }},
		{"org with a hyphen", CodeInvalidValue, func(d map[string]any) { sandboxSpec(d)["org"] = "l-z" }},
		{"org upper case", CodeInvalidValue, func(d map[string]any) { sandboxSpec(d)["org"] = "LZ" }},
		{"metadata name leaves its directory", CodeInvalidValue, func(d map[string]any) { obj(d, "metadata")["name"] = "../sandbox" }},
		{"forge with a space", CodeInvalidValue, func(d map[string]any) { sandboxSpec(d)["forge"] = "example.org/a b" }},
		{"forge without a path", CodeInvalidValue, func(d map[string]any) { sandboxSpec(d)["forge"] = "example.org" }},
		{"stage source kind unknown", CodeInvalidValue, func(d map[string]any) { obj(sandboxSpec(d), "stage_source")["kind"] = "zip" }},
		{"state project ref lower case", CodeInvalidValue, func(d map[string]any) { obj(obj(sandboxSpec(d), "state"), "project")["ref"] = "sandbox" }},
		{"environment project ref with a hyphen", CodeInvalidValue, func(d map[string]any) { obj(sandboxEnv(d), "project")["ref"] = "SAND-BOX" }},
		{"state region upper case", CodeInvalidValue, func(d map[string]any) { obj(sandboxSpec(d), "state")["region"] = "GRA" }},
		{"state endpoint over http", CodeInvalidValue, func(d map[string]any) {
			obj(sandboxSpec(d), "state")["endpoint"] = "http://s3.gra.io.cloud.ovh.net"
		}},
		{"state endpoint with a path", CodeInvalidValue, func(d map[string]any) {
			obj(sandboxSpec(d), "state")["endpoint"] = "https://s3.gra.io.cloud.ovh.net/x"
		}},
		{"network cidr without a prefix length", CodeInvalidValue, func(d map[string]any) { obj(sandboxRegion(d), "network")["cidr"] = "10.20.0.0" }},
		{"empty spec.scope", CodeInvalidValue, func(d map[string]any) { sandboxSpec(d)["scope"] = "" }},
		{"vlan id as a string", CodeInvalidValue, func(d map[string]any) { obj(sandboxRegion(d), "network")["vlan_id"] = "0" }},
		{"shared state flag as a string", CodeInvalidValue, func(d map[string]any) { obj(sandboxSpec(d), "sandbox")["shared_state_project"] = "yes" }},
		// The second tenant and environment differ below the repeated name, so only the name repeats.
		{"tenant named twice", CodeDuplicateName, func(d map[string]any) {
			s := sandboxSpec(d)
			s["tenants"] = append(arr(s, "tenants"), map[string]any{"name": "demo", "environments": []any{
				map[string]any{"name": "prod", "project": map[string]any{"mode": "adopt", "ref": "DEMO_PROD"}, "regions": []any{}}}})
		}},
		{"environment named twice", CodeDuplicateName, func(d map[string]any) {
			te := arr(sandboxSpec(d), "tenants")[0].(map[string]any)
			te["environments"] = append(arr(te, "environments"),
				map[string]any{"name": "dev", "project": map[string]any{"mode": "adopt", "ref": "DEMO_DEV2"}, "regions": []any{}})
		}},
		{"region named twice", CodeDuplicateName, func(d map[string]any) {
			e := sandboxEnv(d)
			e["regions"] = append(arr(e, "regions"), arr(e, "regions")[0])
		}},
		// (r1) A JSON type mismatch on an object is a value error, not a missing field inside it.
		{"state block as a boolean", CodeInvalidValue, func(d map[string]any) { sandboxSpec(d)["state"] = false }},
		{"spec as an array", CodeInvalidValue, func(d map[string]any) { d["spec"] = []any{} }},
		// (r1) Two environments owning one project would give two project stacks one project.
		{"two environments, one project", CodeDuplicateName, func(d map[string]any) {
			te := arr(sandboxSpec(d), "tenants")[0].(map[string]any)
			te["environments"] = append(arr(te, "environments"),
				map[string]any{"name": "prod", "project": map[string]any{"mode": "adopt", "ref": "SANDBOX"}, "regions": []any{}})
		}},
		{"external entry in a platform manifest", CodePlatformRows, func(d map[string]any) {
			sandboxSpec(d)["external"] = []any{map[string]any{"id": "demo-dev-gra11-runtime-blue", "stage": "runtime",
				"tenant": "demo", "environment": "dev", "region": "GRA11", "slot": "blue"}}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := sandboxDocument(t)
			c.change(doc)
			data, err := json.MarshalIndent(doc, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			_, err = DecodeManifest(data)
			var me *ManifestError
			if !errors.As(err, &me) {
				t.Fatalf("want refusal %s, got %v", c.code, err)
			}
			if me.Code != c.code || strings.TrimSpace(me.Detail) == "" {
				t.Errorf("got %s %q, want %s with a detail", me.Code, me.Detail, c.code)
			}
		})
	}
	// The unchanged document, re-encoded the same way, still decodes: the cases fail for their change.
	data, _ := json.Marshal(sandboxDocument(t))
	if _, err := DecodeManifest(data); err != nil {
		t.Fatalf("unchanged sandbox document: %v", err)
	}
}

// TestManifestRefusalDeterministic (r1): a document breaking two decoding rules in different
// places is refused with the same code on every run (stacks:reconcile reports it). Here an unknown
// key in spec.state and a missing regions list under spec.tenants: keys are walked in sorted
// order, so state comes first.
func TestManifestRefusalDeterministic(t *testing.T) {
	doc := sandboxDocument(t)
	obj(sandboxSpec(doc), "state")["bucket"] = "x"
	delete(sandboxEnv(doc), "regions")
	data, _ := json.Marshal(doc)
	seen := map[string]int{}
	for i := 0; i < 64; i++ {
		_, err := DecodeManifest(data)
		var me *ManifestError
		if !errors.As(err, &me) {
			t.Fatalf("want a refusal, got %v", err)
		}
		seen[me.Code]++
	}
	if len(seen) != 1 || seen[CodeUnknownField] != 64 {
		t.Errorf("codes over 64 runs %v, want UNKNOWN_FIELD every time", seen)
	}
}

// TestManifestExternalSlot (r1): an external entry's slot is kept, so two external runtime slots
// stay distinguishable for their consumers.
func TestManifestExternalSlot(t *testing.T) {
	var kept []string
	for _, line := range strings.Split(string(readManifestFixture(t, "tenant-only/deployments.yaml")), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			kept = append(kept, line)
		}
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(strings.Join(kept, "\n")), &doc); err != nil {
		t.Fatal(err)
	}
	s := obj(doc, "spec")
	s["external"] = append(arr(s, "external"), map[string]any{"id": "demo-dev-gra11-runtime-blue", "stage": "runtime",
		"tenant": "demo", "environment": "dev", "region": "GRA11", "slot": "blue"})
	data, _ := json.Marshal(doc)
	m, err := DecodeManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	if last := m.External[len(m.External)-1]; last.Slot != "blue" {
		t.Errorf("external %+v, want slot blue", last)
	}
}

// TestManifestDataModelExampleDecodes:the example manifest data-model.md publishes is one the
// decoder accepts, so the document cannot drift back to a syntax the decoder refuses.
func TestManifestDataModelExampleDecodes(t *testing.T) {
	doc := string(readFile(t, dataModelPath))
	_, section, _ := strings.Cut(doc, "\n## Deployment manifest")
	_, block, found := strings.Cut(section, "```yaml\n")
	block, _, _ = strings.Cut(block, "```")
	if !found {
		t.Fatal("data-model.md has no example manifest")
	}
	m, err := DecodeManifest([]byte(block))
	if err != nil {
		t.Fatalf("data-model example: %v", err)
	}
	if len(m.Instances) != 6 {
		t.Errorf("data-model example: %d instances, want 6", len(m.Instances))
	}
}

// TestManifestStageSource:the stage source kind is exposed for generation, which refuses the git
// seam (STAGE_SOURCE_NOT_IMPLEMENTED, T038); decoding accepts both enum values.
func TestManifestStageSource(t *testing.T) {
	doc := sandboxDocument(t)
	obj(sandboxSpec(doc), "stage_source")["kind"] = "git"
	data, _ := json.Marshal(doc)
	m, err := DecodeManifest(data)
	if err != nil || m.StageSource != "git" {
		t.Fatalf("git stage source: %v, kind %q", err, func() string {
			if m == nil {
				return ""
			}
			return m.StageSource
		}())
	}
	if m := decodeFixture(t, "sandbox"); m.StageSource != "local" {
		t.Errorf("sandbox stage source %q, want local", m.StageSource)
	}
}
