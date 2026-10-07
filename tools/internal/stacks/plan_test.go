package stacks

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// test:stack-plans controls of 005 T038 (FR-007, FR-009, SC-002; contracts/checks.md *task
// test:stack-plans*, V005, V006): PlanStacks plans every generated stack of a project root offline,
// with the pinned OpenTofu, under its generated mocked test (tests/_lz_offline.tftest.hcl) and with
// fixture inputs built from tests/fixtures/outputs/envelopes, then refuses one bucket name planned
// twice with NAME_COLLISION. The candidate is never written: planning runs on a scratch copy.
//
// Readings pinned here (evidence/T038.md): plans come back in manifest row order, one per row; the
// plan is the `test_plan` of the offline test's run `plan`; an adopted project is planned without
// its _lz_import.tf (P6: `tofu test` crashes on an import into a mocked resource; the import is
// pinned statically by TestGenerateStackInvariants, coordinator option A); a consumer whose
// producer has no fixture envelope is `blocked`, never planned with a placeholder (V006).

const planFixtures = repositoryRoot + "/tests/fixtures/outputs/envelopes"

// planRoot generates a manifest fixture into a scratch project root whose stages/, components/ and
// modules/ are links to this repository's (or, with stagesCopy, a copy of stages/ the caller may
// edit), as a checkout would hold them.
func planRoot(t *testing.T, manifest []byte, stagesCopy bool) string {
	t.Helper()
	root := generationRoot(t, manifest)
	for _, dir := range []string{"stages", "components", "modules"} {
		src, err := filepath.Abs(filepath.Join(repositoryRoot, dir))
		if err != nil {
			t.Fatal(err)
		}
		if dir == "stages" && stagesCopy {
			copyTree(t, src, filepath.Join(root, dir))
			continue
		}
		if err := os.Symlink(src, filepath.Join(root, dir)); err != nil {
			t.Fatal(err)
		}
	}
	if err := generateIn(t, root); err != nil {
		t.Fatalf("generate: %v", err)
	}
	return root
}

// stackTree is every file under root/stacks with its content: what PlanStacks must leave alone.
func stackTree(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	err := filepath.WalkDir(filepath.Join(root, "stacks"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		data, err := os.ReadFile(path)
		tree[rel] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// planOutputNames are the root output names of a plan and which of them are planned sensitive.
func planOutputNames(t *testing.T, plan []byte) map[string]bool {
	t.Helper()
	var doc struct {
		Outputs map[string]struct {
			AfterSensitive json.RawMessage `json:"after_sensitive"`
		} `json:"output_changes"`
	}
	if err := json.Unmarshal(plan, &doc); err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for name, o := range doc.Outputs {
		out[name] = string(o.AfterSensitive) == "true"
	}
	return out
}

// planAll runs PlanStacks on root with this repository's fixture envelopes.
func planAll(t *testing.T, root, fixtures string) ([]StackPlan, error) {
	t.Helper()
	before := stackTree(t, root)
	plans, err := PlanStacks(PlanOptions{Root: root, Tofu: pinnedTofu(t), Fixtures: fixtures})
	if after := stackTree(t, root); !reflect.DeepEqual(before, after) {
		t.Errorf("PlanStacks wrote under the candidate's stacks/")
	}
	return plans, err
}

// checkPlans holds a fixture's plans to its manifest: one per row in row order, each stack's
// root outputs exactly its stage's with the stage's sensitivity (the generated re-export, planned),
// and the bucket names wantNames gives per stack path (none for the others).
func checkPlans(t *testing.T, m *Manifest, plans []StackPlan, wantNames map[string][]string) {
	t.Helper()
	var paths, got []string
	for _, in := range m.Instances {
		paths = append(paths, in.Path)
	}
	for _, p := range plans {
		got = append(got, p.Stack)
	}
	if !slices.Equal(got, paths) {
		t.Fatalf("planned %q, want every row's stack in manifest order %q", got, paths)
	}
	for i, p := range plans {
		in := m.Instances[i]
		if outputs, want := planOutputNames(t, p.Plan), stageOutputs(t, in.Stage); !reflect.DeepEqual(outputs, want) {
			t.Errorf("%s: planned root outputs %v, want the stage's %v", p.Stack, outputs, want)
		}
		names, err := PlannedBucketNames(p.Plan)
		if err != nil || !slices.Equal(names, wantNames[p.Stack]) {
			t.Errorf("%s: plans buckets %q (%v), want %q", p.Stack, names, err, wantNames[p.Stack])
		}
	}
}

// bucketNames are the bucket names each bucket-planning stack of a manifest plans (research R15).
func bucketNames(m *Manifest) map[string][]string {
	want := map[string][]string{}
	for _, in := range m.Instances {
		switch in.Stage {
		case "bootstrap":
			want[in.Path] = []string{m.Org + "-bkt-state"}
		case "tenant-state":
			want[in.Path] = []string{m.Org + "-" + in.Tenant + "-bkt-state"}
		case "runtime":
			name := strings.Join([]string{m.Org, in.Tenant, in.Environment, strings.ToLower(in.Region), "bkt", "runtime"}, "-")
			if in.Slot != "" {
				name += "-" + in.Slot
			}
			want[in.Path] = []string{name}
		}
	}
	return want
}

func decoded(t *testing.T, data []byte) *Manifest {
	t.Helper()
	m, err := DecodeManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestStackPlans(t *testing.T) {
	// SC-002: the growth fixture (25 stacks, 2 tenants x 2 environments x 2 regions, two runtime
	// slots) plans every stack; the two slots plan distinct bucket names.
	t.Run("growth plans every stack", func(t *testing.T) {
		data := manifestOf(t, "growth")
		m := decoded(t, data)
		want := bucketNames(m)
		if want["stacks/tenants/demo/dev/gra11/runtime-blue"][0] != "lz-demo-dev-gra11-bkt-runtime-blue" ||
			want["stacks/tenants/demo/dev/gra11/runtime-green"][0] != "lz-demo-dev-gra11-bkt-runtime-green" {
			t.Fatalf("growth slots: %q", want)
		}
		plans, err := planAll(t, planRoot(t, data, false), planFixtures)
		if err != nil {
			t.Fatalf("PlanStacks: %v", err)
		}
		if len(plans) != 25 {
			t.Fatalf("%d plans, want 25", len(plans))
		}
		checkPlans(t, m, plans, want)
	})

	// The tenant-only fixture plans its three stacks in a repository of its own, whose stages are
	// supplied from outside it (T037 decision 1), and no account stack.
	t.Run("tenant-only plans its stacks", func(t *testing.T) {
		data := manifestOf(t, "tenant-only")
		m := decoded(t, data)
		root := generationRoot(t, data)
		stages, err := filepath.Abs(filepath.Join(repositoryRoot, "stages"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(stages, filepath.Join(root, "stages")); err != nil {
			t.Fatal(err)
		}
		if err := generateIn(t, root); err != nil {
			t.Fatalf("generate: %v", err)
		}
		plans, err := planAll(t, root, planFixtures)
		if err != nil {
			t.Fatalf("PlanStacks: %v", err)
		}
		checkPlans(t, m, plans, bucketNames(m))
		for _, p := range plans {
			if strings.HasPrefix(p.Stack, "stacks/account/") {
				t.Errorf("tenant-only planned account stack %s", p.Stack)
			}
		}
	})

	// The colliding fixture: with this repository's stages its two slots plan distinct names (the
	// control); against a runtime stage that drops `slot` both plan one name, refused with
	// NAME_COLLISION at the later stack, naming the bucket.
	collision := manifestOf(t, "collision")
	t.Run("colliding fixture plans distinct slot names", func(t *testing.T) {
		m := decoded(t, collision)
		plans, err := planAll(t, planRoot(t, collision, false), planFixtures)
		if err != nil {
			t.Fatalf("PlanStacks: %v", err)
		}
		checkPlans(t, m, plans, bucketNames(m))
	})
	t.Run("colliding fixture refused when the slot is dropped", func(t *testing.T) {
		root := planRoot(t, collision, true)
		main := filepath.Join(root, "stages", "runtime", "main.tf")
		writeFile(t, main, edit(t, readFile(t, main), "slot        = var.slot", "slot        = null", 1))
		_, err := planAll(t, root, planFixtures)
		var ge *GenerateError
		if !errors.As(err, &ge) || ge.Code != CodeNameCollision || ge.Path != "stacks/tenants/demo/dev/gra11/runtime-green" ||
			!strings.Contains(ge.Detail, "lz-demo-dev-gra11-bkt-runtime ") {
			t.Fatalf("want %s at stacks/tenants/demo/dev/gra11/runtime-green naming lz-demo-dev-gra11-bkt-runtime, got %v", CodeNameCollision, err)
		}
	})

	// Zero discovery is never a pass: no manifest is ErrNoManifest (test:stack-plans reports
	// `fail: no manifest` until T039).
	t.Run("no manifest", func(t *testing.T) {
		root := t.TempDir()
		if _, err := PlanStacks(PlanOptions{Root: root, Tofu: pinnedTofu(t), Fixtures: planFixtures}); !errors.Is(err, ErrNoManifest) {
			t.Fatalf("want ErrNoManifest, got %v", err)
		}
	})

	// A manifest with no rows (a tenant manifest that only names externals decodes) is zero
	// discovery: a failure, never a panic or a pass (review r1).
	t.Run("no rows", func(t *testing.T) {
		data := manifestOf(t, "tenant-only")
		start := strings.Index(string(data), `"instances": [`)
		if start < 0 {
			t.Fatal("tenant-only fixture has no instances list")
		}
		end := strings.Index(string(data)[start:], "]") + start
		data = []byte(string(data[:start]) + `"instances": []` + string(data[end+1:]))
		if _, err := DecodeManifest(data); err != nil {
			t.Skipf("the decoder refuses a manifest without rows (%v): nothing to plan", err)
		}
		root := planRoot(t, data, false)
		if _, err := PlanStacks(PlanOptions{Root: root, Tofu: pinnedTofu(t), Fixtures: planFixtures}); !errors.Is(err, ErrNoStacks) {
			t.Fatalf("want ErrNoStacks, got %v", err)
		}
	})

	// Only the generated offline test is planned: another test file in the stack, whose run is
	// also called `plan` (here against an empty helper module, so it plans no bucket), cannot
	// stand in for it and hide a collision (review r1).
	t.Run("another test file cannot replace the generated plan", func(t *testing.T) {
		root := planRoot(t, collision, true)
		main := filepath.Join(root, "stages", "runtime", "main.tf")
		writeFile(t, main, edit(t, readFile(t, main), "slot        = var.slot", "slot        = null", 1))
		green := filepath.Join(root, "stacks/tenants/demo/dev/gra11/runtime-green")
		writeFile(t, filepath.Join(green, "tests/empty/main.tf"), []byte("# empty helper module\n"))
		writeFile(t, filepath.Join(green, "tests/zz_other.tftest.hcl"), []byte("run \"plan\" {\n  command = plan\n  module {\n    source = \"./tests/empty\"\n  }\n}\n"))
		_, err := planAll(t, root, planFixtures)
		var ge *GenerateError
		if !errors.As(err, &ge) || ge.Code != CodeNameCollision {
			t.Fatalf("want %s despite the competing test file, got %v", CodeNameCollision, err)
		}
	})

	// The scratch copy follows the supplied stages/, components/ and modules/ links, but no link
	// inside them or inside stacks/: a nested link is refused and its target is not copied
	// (review r1: a link could pull a file from anywhere, or loop).
	t.Run("nested link refused", func(t *testing.T) {
		root := planRoot(t, manifestOf(t, "sandbox"), true)
		secret := filepath.Join(t.TempDir(), "outside.env")
		writeFile(t, secret, []byte("LZ_T038_OUTSIDE=1\n"))
		if err := os.Symlink(secret, filepath.Join(root, "stages", "runtime", "outside.env")); err != nil {
			t.Fatal(err)
		}
		if _, err := PlanStacks(PlanOptions{Root: root, Tofu: pinnedTofu(t), Fixtures: planFixtures}); err == nil ||
			!strings.Contains(err.Error(), "stages/runtime/outside.env") || !strings.Contains(err.Error(), "link") {
			t.Fatalf("want the nested link refused, naming it, got %v", err)
		}
		// stacks/ itself is the candidate's own tree, never a supplied one: a link there is refused
		// too (review r2).
		linked := planRoot(t, manifestOf(t, "sandbox"), false)
		moved := filepath.Join(t.TempDir(), "stacks")
		if err := os.Rename(filepath.Join(linked, "stacks"), moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(moved, filepath.Join(linked, "stacks")); err != nil {
			t.Fatal(err)
		}
		if _, err := PlanStacks(PlanOptions{Root: linked, Tofu: pinnedTofu(t), Fixtures: planFixtures}); err == nil ||
			!strings.Contains(err.Error(), "stacks") || !strings.Contains(err.Error(), "link") {
			t.Fatalf("want a linked stacks/ refused, got %v", err)
		}
		loop := planRoot(t, manifestOf(t, "sandbox"), true)
		if err := os.Symlink("..", filepath.Join(loop, "stages", "runtime", "up")); err != nil {
			t.Fatal(err)
		}
		if _, err := PlanStacks(PlanOptions{Root: loop, Tofu: pinnedTofu(t), Fixtures: planFixtures}); err == nil || !strings.Contains(err.Error(), "link") {
			t.Fatalf("want a looping link refused, got %v", err)
		}
	})

	// A row whose stack was not generated (or has no offline test) is a failure naming it.
	t.Run("ungenerated stack", func(t *testing.T) {
		data := manifestOf(t, "sandbox")
		root := generationRoot(t, data)
		if _, err := Reconcile(ReconcileOptions{Root: root, Terramate: pinnedTerramate(t)}); err != nil {
			t.Fatal(err)
		}
		_, err := PlanStacks(PlanOptions{Root: root, Tofu: pinnedTofu(t), Fixtures: planFixtures})
		if err == nil || !strings.Contains(err.Error(), "stacks/account/bootstrap") || !strings.Contains(err.Error(), "_lz_offline.tftest.hcl") {
			t.Fatalf("want a failure naming the ungenerated stack's offline test, got %v", err)
		}
	})

	// A consumer whose producer has no fixture envelope is blocked: no plan with a placeholder.
	t.Run("missing producer fixture blocks", func(t *testing.T) {
		fixtures := t.TempDir()
		entries, err := os.ReadDir(planFixtures)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.Name() != "project.json" {
				writeFile(t, filepath.Join(fixtures, e.Name()), readFile(t, filepath.Join(planFixtures, e.Name())))
			}
		}
		plans, err := planAll(t, planRoot(t, manifestOf(t, "sandbox"), false), fixtures)
		for _, consumer := range []string{"stacks/tenants/demo/dev/gra11/runtime", "stacks/tenants/demo/dev/gra11/project-network"} {
			if err == nil || !strings.Contains(err.Error(), consumer+": blocked: no fixture envelope for producer stage project") {
				t.Fatalf("want %s blocked on the missing project envelope, got %v", consumer, err)
			}
		}
		for _, p := range plans {
			if p.Stack == "stacks/tenants/demo/dev/gra11/runtime" || p.Stack == "stacks/tenants/demo/dev/gra11/project-network" {
				t.Errorf("%s planned without its producer's fixture", p.Stack)
			}
		}
	})

	// The planning children get none of the caller's environment beyond PATH, the tofu CLI
	// configuration and proxy settings: no credential variable reaches them, and HOME is scratch.
	t.Run("children get no credential", func(t *testing.T) {
		root := planRoot(t, manifestOf(t, "sandbox"), false)
		dir := t.TempDir()
		log := filepath.Join(dir, "env.log")
		fake := filepath.Join(dir, "tofu")
		writeFile(t, fake, []byte("#!/bin/sh\nenv >> '"+log+"'\nexit 3\n"))
		if err := os.Chmod(fake, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"OVH_CLIENT_SECRET", "OVH_CLIENT_ID", "AWS_SECRET_ACCESS_KEY", "TF_VAR_state_passphrase", "LZ_CREDENTIAL"} {
			t.Setenv(name, "lz-t038-tripwire")
		}
		home, _ := os.UserHomeDir()
		if _, err := PlanStacks(PlanOptions{Root: root, Tofu: fake, Fixtures: planFixtures}); err == nil {
			t.Fatal("a tofu that fails every call planned")
		}
		env := string(readFile(t, log))
		if strings.Contains(env, "lz-t038-tripwire") || !strings.Contains(env, "TF_PLUGIN_CACHE_DIR=") || strings.Contains(env, "HOME="+home+"\n") {
			t.Errorf("planning children's environment carries a caller variable, or lacks the scratch HOME and plugin cache:\n%s", env)
		}
	})

	// Every stage that publishes an envelope has a fixture: the inputs come from them.
	t.Run("fixture envelopes", func(t *testing.T) {
		entries, err := os.ReadDir(planFixtures)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, e := range entries {
			got = append(got, strings.TrimSuffix(e.Name(), ".json"))
		}
		slices.Sort(got)
		if want := slices.Sorted(maps.Keys(map[string]bool{"bootstrap": true, "tenant-state": true, "account-governance": true,
			"project": true, "project-network": true, "runtime": true})); !slices.Equal(got, want) {
			t.Fatalf("fixture envelopes %q, want %q", got, want)
		}
	})
}
