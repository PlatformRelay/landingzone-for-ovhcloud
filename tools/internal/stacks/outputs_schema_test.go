package stacks

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Controls of 005 T018: the published JSON Schemas under schemas/outputs/ and the Go types the
// validator decodes into describe the same contract, and the strict decoding is not fooled by
// encoding/json's case-insensitive field matching.

// outputsSchemaNode is the part of JSON Schema the outputs schemas may use. It is decoded with
// unknown members rejected, so a schema keyword the code does not mirror (maxLength, enum,
// minItems, format, ...) fails the agreement test instead of being dropped silently.
type outputsSchemaNode struct {
	Schema               string                       `json:"$schema"`
	ID                   string                       `json:"$id"`
	Title                string                       `json:"title"`
	Description          string                       `json:"description"`
	Type                 string                       `json:"type"`
	Const                *string                      `json:"const"`
	Pattern              string                       `json:"pattern"`
	MinLength            *int                         `json:"minLength"`
	Required             []string                     `json:"required"`
	Properties           map[string]outputsSchemaNode `json:"properties"`
	AdditionalProperties json.RawMessage              `json:"additionalProperties"`
	Items                *outputsSchemaNode           `json:"items"`
}

// schemaConsts are the constants a schema pins with `const`, by path, on the Go side.
var schemaConsts = map[string]string{
	"envelope.apiVersion": OutputsAPIVersion,
	"envelope.kind":       OutputsKind,
	"runtime.kind":        RuntimeKindManagedOnly,
}

// schemaPatterns are the patterns a schema pins, by path, on the Go side; no other string has one.
var schemaPatterns = map[string]string{
	"envelope.source_revision": revisionPattern,
	"runtime.slot":             slotPattern,
}

// keywordsOf lists the validation keywords set on a node (type excluded).
func keywordsOf(n outputsSchemaNode) []string {
	var ks []string
	for k, set := range map[string]bool{
		"const": n.Const != nil, "pattern": n.Pattern != "", "minLength": n.MinLength != nil,
		"required": n.Required != nil, "properties": n.Properties != nil,
		"additionalProperties": len(n.AdditionalProperties) > 0, "items": n.Items != nil,
	} {
		if set {
			ks = append(ks, k)
		}
	}
	sort.Strings(ks)
	return ks
}

// agree reports every difference between a schema node and the Go type t, under path.
func agree(node outputsSchemaNode, t reflect.Type, path string) []string {
	var diffs []string
	if c, ok := schemaConsts[path]; ok || node.Const != nil {
		if !ok || node.Const == nil || *node.Const != c {
			diffs = append(diffs, path+": const differs between schema and code")
		}
	}
	if t.Kind() != reflect.Pointer {
		if node.Pattern != schemaPatterns[path] {
			diffs = append(diffs, path+": pattern "+node.Pattern+" in the schema, "+schemaPatterns[path]+" in code")
		}
		// Every keyword must be one the branch below compares; any other is a constraint the code
		// does not mirror (a const string with a minLength, required on a map, items on an object).
		branch := t.Kind().String()
		if t == reflect.TypeOf(map[string]json.RawMessage{}) {
			branch = "raw"
		}
		allowed := map[string]string{"raw": "", "string": "const pattern minLength", "slice": "items",
			"map": "additionalProperties", "struct": "required properties additionalProperties"}[branch]
		for _, k := range keywordsOf(node) {
			if !strings.Contains(" "+allowed+" ", " "+k+" ") || (k == "minLength" && node.Const != nil) {
				diffs = append(diffs, path+": keyword "+k+" not mirrored by the code for "+branch)
			}
		}
	}
	if t == reflect.TypeOf(map[string]json.RawMessage{}) { // the envelope's values, typed per stage
		if node.Type != "object" {
			diffs = append(diffs, path+": raw values must be an object in the schema")
		}
		return diffs
	}
	switch t.Kind() {
	case reflect.Pointer:
		return append(diffs, agree(node, t.Elem(), path)...)
	case reflect.String:
		if node.Type != "string" {
			diffs = append(diffs, path+": schema type "+node.Type+", code string")
		}
		// The code refuses "" anywhere (header and values) and imposes no other length limit.
		if node.Const == nil && (node.MinLength == nil || *node.MinLength != 1) {
			diffs = append(diffs, path+": schema minLength is not 1; code refuses \"\" and nothing shorter")
		}
	case reflect.Slice:
		if node.Type != "array" || node.Items == nil {
			return append(diffs, path+": schema type "+node.Type+", code array")
		}
		diffs = append(diffs, agree(*node.Items, t.Elem(), path+"[]")...)
	case reflect.Map:
		var extra outputsSchemaNode
		if node.Type != "object" || node.Properties != nil || jsonv2.Unmarshal(node.AdditionalProperties, &extra, strict) != nil || extra.Type == "" {
			return append(diffs, path+": code map needs an object schema with typed additionalProperties only")
		}
		diffs = append(diffs, agree(extra, t.Elem(), path+"{}")...)
	case reflect.Struct:
		if node.Type != "object" || strings.TrimSpace(string(node.AdditionalProperties)) != "false" {
			diffs = append(diffs, path+": code struct needs an object schema with additionalProperties false")
		}
		var names, required []string
		for i := 0; i < t.NumField(); i++ {
			name, options, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
			names = append(names, name)
			if !strings.Contains(options, "omitempty") {
				required = append(required, name)
			}
			if sub, ok := node.Properties[name]; ok {
				diffs = append(diffs, agree(sub, t.Field(i).Type, path+"."+name)...)
			}
		}
		var props []string
		for name := range node.Properties {
			props = append(props, name)
		}
		schemaRequired := append([]string{}, node.Required...)
		for _, l := range [][]string{names, required, props, schemaRequired} {
			sort.Strings(l)
		}
		if !reflect.DeepEqual(names, props) {
			diffs = append(diffs, path+": properties differ: schema "+strings.Join(props, ",")+"; code "+strings.Join(names, ","))
		}
		if !reflect.DeepEqual(required, schemaRequired) {
			diffs = append(diffs, path+": required differs: schema "+strings.Join(schemaRequired, ",")+"; code "+strings.Join(required, ","))
		}
	default:
		diffs = append(diffs, path+": code type "+t.String()+" has no schema mapping")
	}
	return diffs
}

func readSchema(t *testing.T, name string) outputsSchemaNode {
	t.Helper()
	var node outputsSchemaNode
	if err := jsonv2.Unmarshal(readFile(t, schemaDir+"/"+name+".schema.json"), &node, strict); err != nil {
		t.Fatalf("schema %s: %v", name, err)
	}
	return node
}

// The schema files are the published contract and the Go types are what the validator enforces:
// the same property names, required sets, types, constants and closed objects, for the envelope
// and every in-scope stage, and no stage on one side only.
func TestOutputsSchemaMatchesTypes(t *testing.T) {
	envelope := readSchema(t, "envelope")
	for _, d := range agree(envelope, reflect.TypeOf(Envelope{}), "envelope") {
		t.Error(d)
	}
	var files []string
	entries, err := os.ReadDir(schemaDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".schema.json"); ok && name != "envelope" {
			files = append(files, name)
		}
	}
	var code []string
	for stage, typ := range stageValues {
		code = append(code, stage)
		for _, d := range agree(readSchema(t, stage), typ, stage) {
			t.Error(d)
		}
	}
	sort.Strings(files)
	sort.Strings(code)
	if !reflect.DeepEqual(files, code) {
		t.Errorf("stages differ: schema files %v, code %v", files, code)
	}
}

// encoding/json matches field names case-insensitively, so a case variant of a known key passes
// DisallowUnknownFields and can override the real one. Keys are matched exactly. A slot is a
// manifest slot name (data-model *Manifest*: `^[a-z][a-z0-9]{0,15}$`).
func TestOutputsValidateRefusesExact(t *testing.T) {
	cases := []mutation{
		{"header stage case variant", "bootstrap", func(d map[string]any) { d["Stage"] = "tenant-state" }, nil, ReasonEnvelope},
		{"value key case variant beside the key", "bootstrap", func(d map[string]any) { values(d)["State_Bucket"] = "lz-other" }, nil, ReasonSchema},
		{"value key case variant instead of the key", "bootstrap", func(d map[string]any) {
			values(d)["STATE_BUCKET"] = values(d)["state_bucket"]
			delete(values(d), "state_bucket")
		}, nil, ReasonSchema},
		{"slot not a slot name", "runtime", func(d map[string]any) { values(d)["slot"] = "Blue_1" }, nil, ReasonSchema},
		{"nested key case variant", "runtime", func(d map[string]any) {
			values(d)["scope"].(map[string]any)["Project_ID"] = "00000000000000000000000000000000"
		}, nil, ReasonSchema},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := fixtureDoc(t, c.stage)
			c.mutate(doc)
			_, err := ValidateEnvelope(schemas(), encode(t, doc), expectation(c.stage, map[string]string{"bootstrap": "account-bootstrap", "runtime": "demo-dev-gra11-runtime-blue"}[c.stage]))
			refusal(t, err, c.reason)
		})
	}
}

// A plain tofu output entry must carry a value and a boolean marker.
func TestOutputsBuildEnvelopeRefusesBrokenEntry(t *testing.T) {
	p := Producer{InstanceID: "account-bootstrap", Stage: "bootstrap", SourceRevision: testRev}
	for name, doc := range map[string]string{
		"plain entry without value": `{"state_bucket":{"sensitive":false,"type":"string"}}`,
		"marker not a boolean":      `{"state_bucket":{"sensitive":"false","type":"string","value":"lz-bkt-state"}}`,
		"entry not an object":       `{"state_bucket":"lz-bkt-state"}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := BuildEnvelope([]byte(doc), p)
			refusal(t, err, ReasonMalformed)
		})
	}
}
