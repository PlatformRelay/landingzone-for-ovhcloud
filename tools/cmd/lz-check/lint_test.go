//go:build offlinetools

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// With the pinned tools inside the offline entry, lint passes a valid module
// and fails a linter offence, keeping the rule name.
func TestLintWithTools(t *testing.T) {
	config, err := os.ReadFile("../../../.tflint.hcl")
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		code int
		want []string
	}{
		"valid": {0, []string{"pass tflint", "LINT_PASS modules/x files=1"}},
		"lint":  {1, []string{"fail tflint LINT_ISSUES", "terraform_typed_variables", "LINT_FAIL modules/x"}},
	} {
		t.Run(name, func(t *testing.T) {
			module, err := os.ReadFile(filepath.Join("../../../tests/check/fixtures/static", name, "main.tf"))
			if err != nil {
				t.Fatal(err)
			}
			root := repo(t, map[string]string{"modules/x/main.tf": string(module), ".tflint.hcl": string(config)})
			code, out := lzCheck(root, "lint", "modules/x")
			for _, w := range c.want {
				if !strings.Contains(out, w) {
					t.Errorf("BEHAVIORAL_RED: %q missing:\n%s", w, out)
				}
			}
			if code != c.code {
				t.Errorf("BEHAVIORAL_RED: exit %d, want %d:\n%s", code, c.code, out)
			}
		})
	}
}
