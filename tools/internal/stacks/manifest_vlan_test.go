package stacks

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// Controls of 005 T098: a project holds one private network per VLAN id (T010 run
// 20261010T184645Z-f7de: POST /cloud/project/<p>/network/private answered 400 `A private network
// already exists for this vlan ID`). An environment owns one project, so the VLAN ids of its region
// networks must differ; the decoder refuses a repeat (DUPLICATE_VLAN) and an id outside 0..4000
// (INVALID_VALUE). The bounds: the API admits 0..4095, 0 meaning no VLAN
// (cloud.ProjectNetworkPrivateCreation.vlanId, kb api/v1/cloud.json), the Control Panel 0..4000
// (vRack guide, public-cloud/network-services/vrack.mdx); the slice takes the narrower.

const codeDuplicateVLAN = "DUPLICATE_VLAN"

func networkRegion(name string, vlan any) map[string]any {
	return map[string]any{"name": name, "network": map[string]any{"cidr": "10.20.1.0/24", "vlan_id": vlan}}
}

func decodeChanged(t *testing.T, change func(d map[string]any)) (*Manifest, error) {
	t.Helper()
	doc := sandboxDocument(t)
	change(doc)
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return DecodeManifest(data)
}

func TestManifestVLANRefusals(t *testing.T) {
	cases := []struct {
		name, code, detail string
		change             func(d map[string]any)
	}{
		{"vlan id below 0", CodeInvalidValue, "vlan_id", func(d map[string]any) { obj(sandboxRegion(d), "network")["vlan_id"] = -1 }},
		{"vlan id past 4000", CodeInvalidValue, "vlan_id", func(d map[string]any) { obj(sandboxRegion(d), "network")["vlan_id"] = 4001 }},
		{"vlan id 4095, admitted by the API, past the Control Panel's 4000", CodeInvalidValue, "vlan_id", func(d map[string]any) {
			obj(sandboxRegion(d), "network")["vlan_id"] = 4095
		}},
		{"two regions of one environment, one vlan id", codeDuplicateVLAN, "GRA11", func(d map[string]any) {
			e := sandboxEnv(d)
			e["regions"] = append(arr(e, "regions"), networkRegion("SBG5", 0))
		}},
		{"a repeat after a distinct id", codeDuplicateVLAN, "SBG5", func(d map[string]any) {
			e := sandboxEnv(d)
			e["regions"] = append(arr(e, "regions"), networkRegion("SBG5", 7), networkRegion("BHS5", 7))
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := decodeChanged(t, c.change)
			var me *ManifestError
			if !errors.As(err, &me) {
				t.Fatalf("want refusal %s, got %v", c.code, err)
			}
			if me.Code != c.code || !strings.Contains(me.Detail, c.detail) {
				t.Errorf("got %s %q, want %s naming %s", me.Code, me.Detail, c.code, c.detail)
			}
		})
	}
}

func TestManifestVLANAdmitted(t *testing.T) {
	cases := []struct {
		name   string
		change func(d map[string]any)
	}{
		{"vlan id 0, the lower bound", func(d map[string]any) { obj(sandboxRegion(d), "network")["vlan_id"] = 0 }},
		{"vlan id 4000, the upper bound", func(d map[string]any) { obj(sandboxRegion(d), "network")["vlan_id"] = 4000 }},
		{"two regions of one environment, distinct ids", func(d map[string]any) {
			e := sandboxEnv(d)
			e["regions"] = append(arr(e, "regions"), networkRegion("SBG5", 1))
		}},
		// A region without a network row creates no private network, so it holds no VLAN id.
		{"a second region without a network", func(d map[string]any) {
			e := sandboxEnv(d)
			e["regions"] = append(arr(e, "regions"), map[string]any{"name": "SBG5"}, map[string]any{"name": "BHS5"})
		}},
		// Another environment owns another project: the same id there is no collision.
		{"one vlan id in two environments", func(d map[string]any) {
			te := arr(sandboxSpec(d), "tenants")[0].(map[string]any)
			te["environments"] = append(arr(te, "environments"), map[string]any{"name": "prod",
				"project": map[string]any{"mode": "adopt", "ref": "DEMO_PROD"}, "regions": []any{networkRegion("GRA11", 0)}})
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := decodeChanged(t, c.change); err != nil {
				t.Errorf("refused: %v", err)
			}
		})
	}
}

// TestManifestNumberWithoutRangeRefused: as for strings, a number at a path without a range is
// refused (fail closed); vlan_id is today the document's only number, so this is checked directly.
func TestManifestNumberWithoutRangeRefused(t *testing.T) {
	err := checkValues(map[string]any{"spec": map[string]any{"count": 1.0}}, "")
	var me *ManifestError
	if !errors.As(err, &me) || me.Code != CodeInvalidValue || !strings.Contains(me.Detail, "spec.count") {
		t.Errorf("got %v, want INVALID_VALUE naming spec.count", err)
	}
}

// TestManifestVLANRequiredDownstream: the manifest requires `vlan_id` on every network row, and no
// layer below it supplies one: modules/private-network and components/network/island declare
// `vlan_id` without a default. A default would hand every network that omits it the same id,
// which is how every probe network ended up on VLAN 0. (The stage's `network.vlan_id` refusal is
// a run in stages/project-network/tests: a row without it is refused by validation.)
func TestManifestVLANRequiredDownstream(t *testing.T) {
	for _, dir := range []string{"modules/private-network", "components/network/island"} {
		t.Run(dir, func(t *testing.T) {
			path := filepath.Join("../../..", dir, "variables.tf")
			file, diags := hclsyntax.ParseConfig(readFile(t, path), path, hcl.InitialPos)
			if diags.HasErrors() {
				t.Fatal(diags)
			}
			found := false
			for _, b := range file.Body.(*hclsyntax.Body).Blocks {
				if b.Type != "variable" || len(b.Labels) != 1 || b.Labels[0] != "vlan_id" {
					continue
				}
				found = true
				if _, ok := b.Body.Attributes["default"]; ok {
					t.Errorf("%s: variable vlan_id has a default; it must be required", path)
				}
				nullable, ok := b.Body.Attributes["nullable"]
				if !ok {
					t.Errorf("%s: variable vlan_id must set nullable = false", path)
				} else if v, d := nullable.Expr.Value(nil); d.HasErrors() || v.True() {
					t.Errorf("%s: variable vlan_id must set nullable = false", path)
				}
			}
			if !found {
				t.Errorf("%s: no variable vlan_id", path)
			}
		})
	}
}
