package stacks

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Reconciler controls added by 005 T036 (review r1): obstructed stack paths are refused before the
// first create, and discovery reads every Terramate file and refuses what it cannot judge.

// A file or symlink where a missing stack, or a directory on its way, should go is
// UNSUPPORTED_CHANGE naming the first such stack, with nothing written although earlier rows are
// also missing.
func TestReconcileRefusesObstructedPath(t *testing.T) {
	want := expectedStacks(t, "sandbox")
	network, runtime := want[4], want[5]
	elsewhere := t.TempDir()
	cases := []struct {
		name    string
		present []stackMeta
		block   func(t *testing.T, root string)
		path    string
	}{
		{"file at the stack path", want[:1], func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, runtime.Path), []byte("not a directory\n"))
		}, runtime.Path},
		{"symlink at the stack path", want[:1], func(t *testing.T, root string) {
			if err := os.MkdirAll(filepath.Dir(filepath.Join(root, runtime.Path)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(elsewhere, filepath.Join(root, runtime.Path)); err != nil {
				t.Fatal(err)
			}
		}, runtime.Path},
		{"file at a parent directory", want[:1], func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, filepath.Dir(network.Path)), []byte("not a directory\n"))
		}, network.Path},
		{"symlink at a parent directory", want[:1], func(t *testing.T, root string) {
			if err := os.MkdirAll(filepath.Dir(filepath.Join(root, filepath.Dir(network.Path))), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(elsewhere, filepath.Join(root, filepath.Dir(network.Path))); err != nil {
				t.Fatal(err)
			}
		}, network.Path},
	}
	for _, c := range cases {
		for _, check := range []bool{false, true} {
			t.Run(c.name+map[bool]string{false: "", true: "/check"}[check], func(t *testing.T) {
				root := scratchRoot(t, manifestOf(t, "sandbox"))
				createStacks(t, root, c.present)
				c.block(t, root)
				before := linkSnapshot(t, root)
				_, err := runReconcile(t, root, check)
				wantUnsupported(t, err, c.path)
				sameTree(t, "refused run", before, linkSnapshot(t, root))
				if entries, _ := os.ReadDir(elsewhere); len(entries) != 0 {
					t.Fatalf("wrote through the link: %v", entries)
				}
			})
		}
	}
}

// (round 2) The working-file rule never hides a Terramate or OpenTofu source file, whatever else
// its name holds.
func TestTofuWorkingFile(t *testing.T) {
	for name, want := range map[string]bool{
		"terraform.tfstate": true, "terraform.tfstate.backup": true, "terraform.tfstate.1700000000.backup": true,
		"plan.tfplan": true, "plan.tfplan.json": true, "crash.log": true, "crash.1.log": true,
		"_lz_backend.tfstate.tf": false, "_lz_main.tfplan.tf": false, "x.tfstate.tf.json": false,
		"crash.tf": false, "a.tfstate.hcl": false, "b.tfstate.tm": false, "c.tfstate.tm.hcl": false,
		"_lz_main.tf": false, "stack.tm.hcl": false, "deployments.yaml": false,
	} {
		if got := tofuWorkingFile(name, false); got != want {
			t.Errorf("tofuWorkingFile(%q) = %v, want %v", name, got, want)
		}
	}
	for name, want := range map[string]bool{".terraform": true, ".terragrunt-cache": true, "terraform": false, "_lz": false} {
		if got := tofuWorkingFile(name, true); got != want {
			t.Errorf("tofuWorkingFile(%q, dir) = %v, want %v", name, got, want)
		}
	}
}

// linkSnapshot is snapshot that records a symlink by its target instead of following it.
func linkSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		switch {
		case d.IsDir():
			tree[filepath.ToSlash(rel)+"/"] = ""
		case d.Type()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			tree[filepath.ToSlash(rel)] = "symlink to " + target
			return err
		default:
			data, err := os.ReadFile(path)
			tree[filepath.ToSlash(rel)] = string(data)
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// Discovery reads a stack block in a *.tm file, refuses two stacks with one id, a second stack
// block in one directory and a stack id that is not a string, writing nothing.
func TestReconcileDiscovery(t *testing.T) {
	want := expectedStacks(t, "sandbox")
	project := want[3]
	cases := []struct {
		name    string
		prepare func(t *testing.T, root string)
		check   func(t *testing.T, err error)
	}{
		{"stack block in a .tm file", func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, "stacks/tenants/demo/dev/legacy/stack.tm"),
				[]byte("stack {\n  id = \"demo-dev-legacy\"\n  tags = [\"lz-stage-project\"]\n}\n"))
		}, func(t *testing.T, err error) { wantUnsupported(t, err, "stacks/tenants/demo/dev/legacy") }},
		{"two stacks with one id", func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, "stacks/tenants/demo/dev/zz-copy/stack.tm.hcl"),
				readFile(t, filepath.Join(root, project.Path, "stack.tm.hcl")))
		}, func(t *testing.T, err error) {
			wantUnsupported(t, err, "stacks/tenants/demo/dev/zz-copy")
			if !strings.Contains(err.Error(), "also the id of "+project.Path) {
				t.Fatalf("refusal %v, want it to name %s", err, project.Path)
			}
		}},
		{"second stack block", func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, project.Path, "second.tm.hcl"), []byte("stack {\n  id = \"demo-dev-other\"\n}\n"))
		}, func(t *testing.T, err error) {
			var re *ReconcileError
			if err == nil || errors.As(err, &re) || !strings.Contains(err.Error(), "second stack block") {
				t.Fatalf("got %v, want a discovery error naming the second stack block", err)
			}
		}},
		{"id not a string", func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, project.Path, "stack.tm.hcl"), []byte("stack {\n  id = 5\n  tags = []\n}\n"))
		}, func(t *testing.T, err error) {
			if err == nil || !strings.Contains(err.Error(), "stack id is not a string") {
				t.Fatalf("got %v, want a discovery error for the id", err)
			}
		}},
		{"tags not a list of strings", func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, project.Path, "stack.tm.hcl"), []byte("stack {\n  id = \"demo-dev-project\"\n  tags = \"lz-stage-project\"\n}\n"))
		}, func(t *testing.T, err error) {
			if err == nil || !strings.Contains(err.Error(), "stack tags: not a list of strings") {
				t.Fatalf("got %v, want a discovery error for the tags", err)
			}
		}},
	}
	// (round 2) Not refusals: a module cache is not searched, and an attribute the reconciler does
	// not read is not evaluated.
	for _, c := range []struct {
		name    string
		prepare func(t *testing.T, root string)
	}{
		{"stacks inside .terraform are not stacks", func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, project.Path, ".terraform/modules/m/stacks/a/stack.tm.hcl"),
				[]byte("stack {\n  id = \"foreign\"\n}\n"))
		}},
		{"other stack attributes are not evaluated", func(t *testing.T, root string) {
			data := readFile(t, filepath.Join(root, project.Path, "stack.tm.hcl"))
			writeFile(t, filepath.Join(root, project.Path, "stack.tm.hcl"),
				[]byte(strings.Replace(string(data), "stack {", "stack {\n  wanted_by = [lower(\"X\")]", 1)))
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := scratchRoot(t, manifestOf(t, "sandbox"))
			createStacks(t, root, want)
			c.prepare(t, root)
			before := snapshot(t, root)
			if report, err := runReconcile(t, root, false); err != nil || len(report.Created) != 0 {
				t.Fatalf("created %v, err %v; want a no-op", report.Created, err)
			}
			sameTree(t, "no-op run", before, snapshot(t, root))
		})
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := scratchRoot(t, manifestOf(t, "sandbox"))
			createStacks(t, root, want[:5])
			c.prepare(t, root)
			before := snapshot(t, root)
			_, err := runReconcile(t, root, false)
			c.check(t, err)
			sameTree(t, "refused run", before, snapshot(t, root))
		})
	}
}
