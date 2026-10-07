package stacks

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
)

// Controls of 005 T034: schemas/deployments.schema.json (the published contract) and the Go
// document types plus manifestRules (what DecodeManifest enforces) describe the same manifest,
// as TestOutputsSchemaMatchesTypes does for outputs.json (T018).

const manifestSchemaPath = "../../../schemas/deployments.schema.json"

// manifestSchemaNode is the part of JSON Schema the manifest schema may use, decoded with unknown
// members rejected: a keyword the code does not mirror (minItems, format, maximum, ...) fails the
// test instead of being dropped silently.
type manifestSchemaNode struct {
	Schema               string                        `json:"$schema"`
	ID                   string                        `json:"$id"`
	Title                string                        `json:"title"`
	Description          string                        `json:"description"`
	Type                 string                        `json:"type"`
	Const                json.RawMessage               `json:"const"` // raw, so `"const": null` is seen
	Enum                 []string                      `json:"enum"`
	Pattern              string                        `json:"pattern"`
	Required             []string                      `json:"required"`
	Properties           map[string]manifestSchemaNode `json:"properties"`
	AdditionalProperties json.RawMessage               `json:"additionalProperties"`
	Items                *manifestSchemaNode           `json:"items"`
}

// manifestConsts are the header constants the schema pins with `const`.
var manifestConsts = map[string]string{"apiVersion": ManifestAPIVersion, "kind": ManifestKind}

// manifestAgree reports every difference between a schema node and the Go type t at path, and
// records the string paths it visited.
func manifestAgree(node manifestSchemaNode, t reflect.Type, path string, visited map[string]bool) []string {
	var diffs []string
	set := map[string]bool{
		"const": len(node.Const) > 0, "enum": node.Enum != nil, "pattern": node.Pattern != "",
		"required": node.Required != nil, "properties": node.Properties != nil,
		"additionalProperties": len(node.AdditionalProperties) > 0, "items": node.Items != nil,
	}
	allowed := map[reflect.Kind]string{reflect.String: "const enum pattern", reflect.Slice: "items",
		reflect.Struct: "required properties additionalProperties"}[t.Kind()]
	for k, on := range set {
		if on && !strings.Contains(" "+allowed+" ", " "+k+" ") {
			diffs = append(diffs, path+": keyword "+k+" not mirrored by the code for "+t.Kind().String())
		}
	}
	switch t.Kind() {
	case reflect.String:
		if node.Type != "string" {
			diffs = append(diffs, path+": schema type "+node.Type+", code string")
		}
		if c, ok := manifestConsts[path]; ok || len(node.Const) > 0 {
			var got string
			if !ok || json.Unmarshal(node.Const, &got) != nil || got != c ||
				node.Enum != nil || node.Pattern != "" {
				diffs = append(diffs, path+": const differs between schema and code")
			}
			return diffs
		}
		visited[path] = true
		rule, ok := manifestRules[path]
		if !ok || (rule.Pattern == "") == (rule.Enum == nil) {
			diffs = append(diffs, path+": code has no single value rule (a pattern or an enum) for this string")
		}
		if node.Pattern != rule.Pattern {
			diffs = append(diffs, path+": pattern "+node.Pattern+" in the schema, "+rule.Pattern+" in code")
		}
		if !slices.Equal(node.Enum, rule.Enum) {
			diffs = append(diffs, path+": enum "+strings.Join(node.Enum, ",")+" in the schema, "+strings.Join(rule.Enum, ",")+" in code")
		}
	case reflect.Bool:
		if node.Type != "boolean" {
			diffs = append(diffs, path+": schema type "+node.Type+", code bool")
		}
	case reflect.Int:
		if node.Type != "integer" {
			diffs = append(diffs, path+": schema type "+node.Type+", code int")
		}
	case reflect.Slice:
		if node.Type != "array" || node.Items == nil {
			return append(diffs, path+": schema type "+node.Type+", code array")
		}
		diffs = append(diffs, manifestAgree(*node.Items, t.Elem(), path+"[]", visited)...)
	case reflect.Struct:
		if node.Type != "object" || strings.TrimSpace(string(node.AdditionalProperties)) != "false" {
			diffs = append(diffs, path+": code struct needs an object schema with additionalProperties false")
		}
		var names, required, props []string
		for i := 0; i < t.NumField(); i++ {
			name, options, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
			names = append(names, name)
			if !strings.Contains(options, "omitempty") {
				required = append(required, name)
			}
			sub := path + "." + name
			if path == "" {
				sub = name
			}
			if n, ok := node.Properties[name]; ok {
				diffs = append(diffs, manifestAgree(n, t.Field(i).Type, sub, visited)...)
			}
		}
		for name := range node.Properties {
			props = append(props, name)
		}
		schemaRequired := append([]string{}, node.Required...)
		for _, l := range [][]string{names, required, props, schemaRequired} {
			sort.Strings(l)
		}
		if !slices.Equal(names, props) {
			diffs = append(diffs, path+": properties differ: schema "+strings.Join(props, ",")+"; code "+strings.Join(names, ","))
		}
		if !slices.Equal(required, schemaRequired) {
			diffs = append(diffs, path+": required differs: schema "+strings.Join(schemaRequired, ",")+"; code "+strings.Join(required, ","))
		}
	default:
		diffs = append(diffs, path+": code type "+t.String()+" has no schema mapping")
	}
	return diffs
}

// TestManifestSchemaMatchesTypes: the schema and the code agree on every property, required set,
// type, closed object, header constant, pattern and enum; every string of the document has exactly
// one value rule, and the code holds no rule for a path the document does not have.
func TestManifestSchemaMatchesTypes(t *testing.T) {
	var root manifestSchemaNode
	if err := jsonv2.Unmarshal(readFile(t, manifestSchemaPath), &root, strict); err != nil {
		t.Fatalf("schema: %v", err)
	}
	visited := map[string]bool{}
	for _, d := range manifestAgree(root, reflect.TypeOf(manifestDoc{}), "", visited) {
		t.Error(d)
	}
	for path := range manifestRules {
		if !visited[path] {
			t.Errorf("manifestRules has %s, which is no string of the document", path)
		}
	}
}

// TestManifestSchemaAgreementSeesNullConst (r1): `"const": null` is a keyword, not its absence; on
// a string the code checks by pattern it is a disagreement.
func TestManifestSchemaAgreementSeesNullConst(t *testing.T) {
	var node manifestSchemaNode
	if err := jsonv2.Unmarshal([]byte(`{"type": "string", "const": null, "pattern": "`+metadataNamePattern+`"}`), &node, strict); err != nil {
		t.Fatal(err)
	}
	if diffs := manifestAgree(node, reflect.TypeOf(""), "metadata.name", map[string]bool{}); len(diffs) == 0 {
		t.Error(`"const": null on metadata.name passed the agreement check`)
	}
}

// TestManifestSchemaStageEnum:the stage enum of rows and external entries is the stage table's,
// in its order, reserved stages included (they decode as values and are refused afterwards).
func TestManifestSchemaStageEnum(t *testing.T) {
	var names []string
	for _, s := range stageTable {
		names = append(names, s.Name)
	}
	for _, path := range []string{"spec.instances[].stage", "spec.external[].stage"} {
		if got := manifestRules[path].Enum; len(names) == 0 || !slices.Equal(got, names) {
			t.Errorf("%s: enum %v, stage table %v", path, got, names)
		}
	}
}
