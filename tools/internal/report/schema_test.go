package report

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type schemaNode struct {
	Required   []string              `json:"required"`
	Properties map[string]schemaNode `json:"properties"`
	Items      *schemaNode           `json:"items"`
	Enum       []string              `json:"enum"`
}

// jsonFields lists a struct's JSON names, and those without omitempty.
func jsonFields(t reflect.Type) (all, required []string) {
	for i := 0; i < t.NumField(); i++ {
		name, options, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		all = append(all, name)
		if !strings.Contains(options, "omitempty") {
			required = append(required, name)
		}
	}
	sort.Strings(all)
	sort.Strings(required)
	return all, required
}

func keys(m map[string]schemaNode) []string {
	var names []string
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// The schema documents exactly what Evaluate emits: the same fields, the same
// required set, and every reason constant.
func TestSchemaMatchesObservation(t *testing.T) {
	data, err := os.ReadFile("../../../schemas/check-report.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema schemaNode
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("schema is not JSON: %v", err)
	}
	for name, pair := range map[string]struct {
		node schemaNode
		typ  reflect.Type
	}{
		"observation": {schema, reflect.TypeOf(Observation{})},
		"run":         {*schema.Properties["runs"].Items, reflect.TypeOf(Run{})},
		"diagnostic":  {*schema.Properties["diagnostics"].Items, reflect.TypeOf(Diagnostic{})},
	} {
		all, required := jsonFields(pair.typ)
		schemaRequired := append([]string{}, pair.node.Required...)
		sort.Strings(schemaRequired)
		if !reflect.DeepEqual(keys(pair.node.Properties), all) || !reflect.DeepEqual(schemaRequired, required) {
			t.Errorf("%s schema drifted: properties %v required %v; struct %v required %v", name, keys(pair.node.Properties), schemaRequired, all, required)
		}
	}
	reasons := []string{ReasonEmpty, ReasonTruncated, ReasonMalformed, ReasonMissingVersion, ReasonMissingSummary,
		ReasonExitStatus, ReasonZeroTests, ReasonSkipped, ReasonFailed, ReasonCountMismatch, ReasonErrorDiagnostic, ReasonCleanupFailed}
	enum := append([]string{}, schema.Properties["reasons"].Items.Enum...)
	sort.Strings(reasons)
	sort.Strings(enum)
	if !reflect.DeepEqual(reasons, enum) {
		t.Errorf("reason enum drifted: schema %v, code %v", enum, reasons)
	}
}

// Every fixture's observation serialises with exactly the schema's fields.
func TestObservationSerialisesRequiredFields(t *testing.T) {
	for _, name := range []string{"pass", "cleanup", "zero"} {
		o := evaluateFixture(t, name)
		data, err := json.Marshal(o)
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]json.RawMessage
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"runs", "diagnostics", "reasons"} {
			if string(decoded[field]) == "null" {
				t.Errorf("%s: %s serialises as null, schema requires an array", name, field)
			}
		}
	}
}
