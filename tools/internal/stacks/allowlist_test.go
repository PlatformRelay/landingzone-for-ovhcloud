package stacks

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// Tenant allowlist agreement (005 T097; research R6, P9): the storage-iam probe exercises the
// tenant deployer's allowlist live, so its tenant policy must be the component's list, action for
// action. Both roots hold the list as the static local `tenant_allow`; each must equal p9Allowlist
// (the list the account-governance plan capture is pinned to), duplicates included, so a grant
// added to one root and not the other fails here.
func TestTenantAllowlistComponentAndProbeAgree(t *testing.T) {
	for _, f := range []string{
		"components/identity/ovh-native/main.tf",
		"tests/live/probes/storage-iam/main.tf",
	} {
		got := staticLocalStrings(t, filepath.Join(repositoryRoot, f), "tenant_allow")
		slices.Sort(got)
		want := slices.Clone(p9Allowlist)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s: local.tenant_allow = %q, want the P9 allowlist of research R6 %q", f, got, want)
		}
	}
}

// staticLocalStrings returns the one `locals` attribute name of an HCL file as strings; a missing
// or repeated attribute, an expression that is not a static list of strings, fails the test.
func staticLocalStrings(t *testing.T, path, name string) []string {
	t.Helper()
	file, diags := hclsyntax.ParseConfig(readFile(t, path), path, hcl.InitialPos)
	if diags.HasErrors() {
		t.Fatalf("%s: %s", path, diags.Error())
	}
	var found []*hclsyntax.Attribute
	for _, b := range file.Body.(*hclsyntax.Body).Blocks {
		if a, ok := b.Body.Attributes[name]; ok && b.Type == "locals" {
			found = append(found, a)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%s: want one local %s, found %d", path, name, len(found))
	}
	v, diags := found[0].Expr.Value(nil)
	if diags.HasErrors() {
		t.Fatalf("%s: local.%s is not static: %s", path, name, diags.Error())
	}
	if !v.CanIterateElements() || v.IsNull() {
		t.Fatalf("%s: local.%s is not a list", path, name)
	}
	var out []string
	for it := v.ElementIterator(); it.Next(); {
		_, e := it.Element()
		if e.IsNull() || !e.IsKnown() || e.Type() != cty.String {
			t.Fatalf("%s: local.%s holds a non-string %#v", path, name, e)
		}
		out = append(out, e.AsString())
	}
	return out
}
