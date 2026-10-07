package stacks

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// Reconciler controls of 005 T035 (FR-007; ADR-0004, ADR-0007; research R3, R16).
//
// Every control runs the pinned Terramate (pinnedTerramate) on a scratch tree: a Terramate
// project root made from the P17 seeds of T007 (tests/fixtures/terramate/p17/: root config with
// required_version, so no git is needed, and a generate_hcl config, so a reconciler that also
// generates is seen), a config-only stacks/_lz/ directory, and a manifest fixture of T033 as
// stacks/deployments.yaml. The expected stack metadata is hand-written in
// tests/fixtures/terramate/reconcile/<fixture>.json. Base trees for the refusal controls are made
// by the test calling `terramate create` itself, so they do not depend on the code under test.

const terramateFixtureDir = "../../../tests/fixtures/terramate"

// stackMeta is a stack's metadata as written in its stack block.
type stackMeta struct {
	Path  string   `json:"path"`
	ID    string   `json:"id"`
	Tags  []string `json:"tags"`
	After []string `json:"after"`
}

var (
	terramateOnce sync.Once
	terramatePath string
	terramateErr  error
)

// pinnedTerramate returns the Terramate binary the controls run: LZ_TERRAMATE, else the entry's
// /tcb/terramate, else terramate on PATH; it must report the version mise.toml pins.
func pinnedTerramate(t *testing.T) string {
	t.Helper()
	terramateOnce.Do(func() {
		mise, err := os.ReadFile("../../../mise.toml")
		if err != nil {
			terramateErr = err
			return
		}
		pin := regexp.MustCompile(`(?m)^"aqua:terramate-io/terramate"\s*=\s*"([^"]+)"`).FindSubmatch(mise)
		if pin == nil {
			terramateErr = errors.New("mise.toml pins no terramate version")
			return
		}
		path := os.Getenv("LZ_TERRAMATE")
		if path == "" {
			if _, err := os.Stat("/tcb/terramate"); err == nil {
				path = "/tcb/terramate"
			} else if path, err = exec.LookPath("terramate"); err != nil {
				terramateErr = fmt.Errorf("no terramate binary (set LZ_TERRAMATE): %v", err)
				return
			}
		}
		out, err := exec.Command(path, "version").Output()
		if got := strings.TrimSpace(string(out)); err != nil || got != string(pin[1]) {
			terramateErr = fmt.Errorf("%s reports version %q (%v), want the pinned %s; set LZ_TERRAMATE", path, got, err, pin[1])
			return
		}
		terramatePath = path
	})
	if terramateErr != nil {
		t.Fatal(terramateErr)
	}
	return terramatePath
}

// terramate runs the pinned Terramate in root and returns its stdout.
func terramate(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command(pinnedTerramate(t), append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "TM_DISABLE_CHECKPOINT=true")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("terramate %s: %v: %s", strings.Join(args, " "), err, stderr.String())
	}
	return string(out)
}

// scratchRoot makes a Terramate project root holding manifest as stacks/deployments.yaml.
func scratchRoot(t *testing.T, manifest []byte) string {
	t.Helper()
	root := t.TempDir()
	for src, dst := range map[string]string{"p17/root.tm.hcl.seed": "terramate.tm.hcl", "p17/generate.tm.hcl.seed": "lz_generate.tm.hcl"} {
		writeFile(t, filepath.Join(root, dst), readFile(t, filepath.Join(terramateFixtureDir, src)))
	}
	writeFile(t, filepath.Join(root, "stacks/_lz/lz_globals.tm.hcl"), []byte("globals {\n  lz_marker = \"config only, not a stack\"\n}\n"))
	writeFile(t, filepath.Join(root, "stacks/deployments.yaml"), manifest)
	return root
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// expectedStacks reads the hand-written stack metadata of a manifest fixture.
func expectedStacks(t *testing.T, fixture string) []stackMeta {
	t.Helper()
	var doc struct {
		Stacks []stackMeta `json:"stacks"`
	}
	if err := json.Unmarshal(readFile(t, filepath.Join(terramateFixtureDir, "reconcile", fixture+".json")), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Stacks) == 0 {
		t.Fatalf("%s: no expected stacks", fixture)
	}
	return doc.Stacks
}

// createStacks makes stacks with the pinned Terramate directly, independent of Reconcile.
func createStacks(t *testing.T, root string, stacks []stackMeta) {
	t.Helper()
	for _, s := range stacks {
		args := []string{"create", "--id", s.ID, "--name", s.ID, "--tags", strings.Join(s.Tags, ","), "--no-generate"}
		for _, a := range s.After {
			args = append(args, "--after", a)
		}
		terramate(t, root, append(args, s.Path)...)
	}
}

// snapshot records every directory and file under root with its content.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			tree[filepath.ToSlash(rel)+"/"] = ""
			return nil
		}
		data, err := os.ReadFile(path)
		tree[filepath.ToSlash(rel)] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// sameTree fails with the first difference between two snapshots.
func sameTree(t *testing.T, what string, want, got map[string]string) {
	t.Helper()
	if reflect.DeepEqual(want, got) {
		return
	}
	keys := map[string]bool{}
	for k := range want {
		keys[k] = true
	}
	for k := range got {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	slices.Sort(sorted)
	for _, k := range sorted {
		w, inWant := want[k]
		g, inGot := got[k]
		if inWant != inGot || w != g {
			t.Fatalf("%s: tree changed at %s (before present=%v, after present=%v):\nbefore: %q\nafter:  %q", what, k, inWant, inGot, w, g)
		}
	}
}

// stackDirs lists the directories under root/stacks whose Terramate config holds a stack block,
// as slash-separated paths relative to root, sorted.
func stackDirs(t *testing.T, root string) []string {
	t.Helper()
	var dirs []string
	err := filepath.WalkDir(filepath.Join(root, "stacks"), func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		if _, ok := readStack(t, path); ok {
			rel, _ := filepath.Rel(root, path)
			dirs = append(dirs, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(dirs)
	return dirs
}

// readStack parses the stack block of a directory's *.tm.hcl files.
func readStack(t *testing.T, dir string) (stackMeta, bool) {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "*.tm.hcl"))
	for _, f := range files {
		file, diags := hclsyntax.ParseConfig(readFile(t, f), f, hcl.Pos{Line: 1, Column: 1})
		if diags.HasErrors() {
			t.Fatalf("%s: %s", f, diags.Error())
		}
		for _, b := range file.Body.(*hclsyntax.Body).Blocks {
			if b.Type != "stack" {
				continue
			}
			m := stackMeta{}
			for name, attr := range b.Body.Attributes {
				v, diags := attr.Expr.Value(nil)
				if diags.HasErrors() {
					t.Fatalf("%s: %s: %s", f, name, diags.Error())
				}
				switch name {
				case "id":
					m.ID = v.AsString()
				case "tags":
					m.Tags = ctyStrings(t, v)
				case "after":
					m.After = ctyStrings(t, v)
				}
			}
			return m, true
		}
	}
	return stackMeta{}, false
}

func ctyStrings(t *testing.T, v cty.Value) []string {
	t.Helper()
	if !v.CanIterateElements() {
		t.Fatalf("not a list: %#v", v)
	}
	var out []string
	for it := v.ElementIterator(); it.Next(); {
		_, e := it.Element()
		out = append(out, e.AsString())
	}
	return out
}

// checkStack compares a stack's metadata with the expectation: id exactly, tags and after as
// sets without duplicates.
func checkStack(t *testing.T, root string, want stackMeta) {
	t.Helper()
	got, ok := readStack(t, filepath.Join(root, want.Path))
	if !ok {
		t.Errorf("%s: no stack block", want.Path)
		return
	}
	if got.ID != want.ID {
		t.Errorf("%s: id %q, want %q", want.Path, got.ID, want.ID)
	}
	for _, f := range []struct {
		name      string
		got, want []string
	}{{"tags", got.Tags, want.Tags}, {"after", got.After, want.After}} {
		if len(slices.Compact(sortedCopy(f.got))) != len(f.got) {
			t.Errorf("%s: %s %v has duplicates", want.Path, f.name, f.got)
		}
		if !slices.Equal(slices.Compact(sortedCopy(f.got)), sortedCopy(f.want)) {
			t.Errorf("%s: %s %v, want (as a set) %v", want.Path, f.name, f.got, f.want)
		}
	}
}

func paths(stacks []stackMeta) []string {
	var out []string
	for _, s := range stacks {
		out = append(out, s.Path)
	}
	return out
}

func runReconcile(t *testing.T, root string, check bool) (ReconcileReport, error) {
	t.Helper()
	return Reconcile(ReconcileOptions{Root: root, Terramate: pinnedTerramate(t), Check: check})
}

// recordedReconcile runs Reconcile with a wrapper that logs each invocation and then executes the
// pinned Terramate, and returns the logged invocations whose arguments include `create`.
func recordedReconcile(t *testing.T, root string) (ReconcileReport, []string, error) {
	t.Helper()
	dir := t.TempDir()
	log, wrapper := filepath.Join(dir, "invocations.log"), filepath.Join(dir, "terramate")
	writeFile(t, wrapper, []byte(fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %q\nexec %q \"$@\"\n", log, pinnedTerramate(t))))
	if err := os.Chmod(wrapper, 0o755); err != nil {
		t.Fatal(err)
	}
	report, err := Reconcile(ReconcileOptions{Root: root, Terramate: wrapper})
	data, _ := os.ReadFile(log)
	var creates []string
	for _, line := range strings.Split(string(data), "\n") {
		if slices.Contains(strings.Fields(line), "create") {
			creates = append(creates, line)
		}
	}
	return report, creates, err
}

// wantCreates asserts one logged `terramate create` per expected stack, naming its path and id.
func wantCreates(t *testing.T, creates []string, want []stackMeta) {
	t.Helper()
	if len(creates) != len(want) {
		t.Fatalf("%d terramate create invocations %q, want one per created stack (%d)", len(creates), creates, len(want))
	}
	for _, s := range want {
		n := 0
		for _, c := range creates {
			fields := strings.Fields(c)
			if slices.ContainsFunc(fields, func(f string) bool { return strings.HasSuffix(f, s.Path) }) &&
				slices.ContainsFunc(fields, func(f string) bool { return f == s.ID || f == "--id="+s.ID }) {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s: %d terramate create invocations name its path and id %q, want 1: %q", s.Path, n, s.ID, creates)
		}
	}
}

// edit replaces old with new in data exactly n times, so a drifted base fixture fails here.
func edit(t *testing.T, data []byte, old, new string, n int) []byte {
	t.Helper()
	if c := strings.Count(string(data), old); c != n {
		t.Fatalf("edit %q: %d matches, want %d", old, c, n)
	}
	return []byte(strings.ReplaceAll(string(data), old, new))
}

// wantUnsupported asserts an UNSUPPORTED_CHANGE refusal naming path (any path when empty).
func wantUnsupported(t *testing.T, err error, path string) {
	t.Helper()
	var re *ReconcileError
	if !errors.As(err, &re) || re.Code != CodeUnsupportedChange {
		t.Fatalf("want refusal %s, got %v", CodeUnsupportedChange, err)
	}
	if re.Detail == "" || (path != "" && re.Path != path) || re.Path == "" {
		t.Fatalf("refusal %+v, want path %q and a detail", re, path)
	}
}

func manifestOf(t *testing.T, fixture string) []byte {
	return readManifestFixture(t, filepath.Join(fixture, "deployments.yaml"))
}

// A missing stack is created through the pinned terramate create with the derived id, tags and
// after, and nothing else is written (generation is stacks:generate's step).
func TestReconcileCreatesMissingStacks(t *testing.T) {
	for _, fixture := range []string{"sandbox", "tenant-only"} {
		t.Run(fixture, func(t *testing.T) {
			want := expectedStacks(t, fixture)
			root := scratchRoot(t, manifestOf(t, fixture))
			report, creates, err := recordedReconcile(t, root)
			if err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			if !slices.Equal(report.Created, paths(want)) {
				t.Errorf("report created %v, want %v (manifest row order)", report.Created, paths(want))
			}
			wantCreates(t, creates, want)
			if got := stackDirs(t, root); !slices.Equal(got, sortedCopy(paths(want))) {
				t.Fatalf("stacks %v, want %v", got, sortedCopy(paths(want)))
			}
			for _, s := range want {
				checkStack(t, root, s)
				entries, _ := os.ReadDir(filepath.Join(root, s.Path))
				if len(entries) != 1 || entries[0].Name() != "stack.tm.hcl" {
					t.Errorf("%s: holds %v, want only stack.tm.hcl (no generation)", s.Path, entries)
				}
			}
		})
	}
	t.Run("one missing", func(t *testing.T) {
		want := expectedStacks(t, "sandbox")
		root := scratchRoot(t, manifestOf(t, "sandbox"))
		createStacks(t, root, want[:5])
		before := snapshot(t, root)
		report, creates, err := recordedReconcile(t, root)
		if err != nil {
			t.Fatalf("Reconcile: %v", err)
		}
		if !slices.Equal(report.Created, []string{want[5].Path}) {
			t.Fatalf("report created %v, want [%s]", report.Created, want[5].Path)
		}
		wantCreates(t, creates, want[5:])
		after := snapshot(t, root)
		for k, v := range before {
			if after[k] != v {
				t.Errorf("existing %s changed", k)
			}
		}
		checkStack(t, root, want[5])
	})
}

// The after filters select exactly their producer in the pinned Terramate, and its run order
// puts every producer before its consumer (growth: 2 tenants x 2 environments x 2 regions).
func TestReconcileAfterSelectsProducer(t *testing.T) {
	m := decodeFixture(t, "growth")
	root := scratchRoot(t, manifestOf(t, "growth"))
	if _, err := runReconcile(t, root, false); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	pathOf, tagsOf := map[string]string{}, map[string][]string{}
	for _, in := range m.Instances {
		got, ok := readStack(t, filepath.Join(root, in.Path))
		if !ok {
			t.Fatalf("%s: not created", in.Path)
		}
		pathOf[in.ID], tagsOf[in.Path] = in.Path, got.Tags
	}
	order := strings.Fields(terramate(t, root, "list", "--run-order"))
	index := map[string]int{}
	for i, p := range order {
		index[p] = i
	}
	for _, in := range m.Instances {
		got, _ := readStack(t, filepath.Join(root, in.Path))
		var producers []string
		for _, e := range in.Edges {
			producers = append(producers, pathOf[e.Producer])
			if index[pathOf[e.Producer]] >= index[in.Path] {
				t.Errorf("run order puts %s (producer) at %d, not before %s at %d", pathOf[e.Producer], index[pathOf[e.Producer]], in.Path, index[in.Path])
			}
		}
		var selected []string
		for _, filter := range got.After {
			var matches []string
			for p, tags := range tagsOf {
				all := strings.HasPrefix(filter, "tag:")
				for _, tag := range strings.Split(strings.TrimPrefix(filter, "tag:"), ":") {
					all = all && slices.Contains(tags, tag)
				}
				if all {
					matches = append(matches, p)
				}
			}
			if len(matches) != 1 {
				t.Errorf("%s: after %q selects %v, want exactly one producer", in.Path, filter, matches)
			}
			// The same filter as a Terramate tag filter (`:` is AND there too) selects the same stack.
			if filter, ok := strings.CutPrefix(filter, "tag:"); ok {
				if listed := strings.Fields(terramate(t, root, "list", "--tags", filter)); !slices.Equal(listed, sortedCopy(matches)) {
					t.Errorf("%s: terramate list --tags %s gives %v, the AND reading %v", in.Path, filter, listed, matches)
				}
			}
			selected = append(selected, matches...)
		}
		if !slices.Equal(sortedCopy(selected), sortedCopy(producers)) {
			t.Errorf("%s: after %v selects %v, want its producers %v", in.Path, got.After, selected, producers)
		}
	}
}

// A repeat run is a no-op: nothing reported, the tree byte-identical; the config-only
// stacks/_lz/ directory is not taken for a stack.
func TestReconcileRepeatIsNoOp(t *testing.T) {
	root := scratchRoot(t, manifestOf(t, "sandbox"))
	if _, err := runReconcile(t, root, false); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if got := stackDirs(t, root); len(got) != 6 {
		t.Fatalf("first run made stacks %v, want 6", got)
	}
	before := snapshot(t, root)
	for _, check := range []bool{false, true} {
		report, err := runReconcile(t, root, check)
		if err != nil || len(report.Created) != 0 {
			t.Fatalf("repeat run (check=%v): created %v, err %v; want nothing", check, report.Created, err)
		}
		sameTree(t, "repeat run", before, snapshot(t, root))
	}
}

// Every mismatch other than a missing stack is UNSUPPORTED_CHANGE, found before anything is
// written: each case also lacks a stack the manifest names (runtime, or runtime-blue), which a
// refused run must not create.
func TestReconcileRefusesUnsupportedChange(t *testing.T) {
	want := expectedStacks(t, "sandbox")
	base, runtime := want[:5], want[5]
	sandbox := manifestOf(t, "sandbox")
	project, network := want[3], want[4]
	cases := []struct {
		name     string
		manifest []byte
		prepare  func(t *testing.T, root string)
		path     string // the refused path: the existing stack directory that no longer matches
	}{
		{"directory without a row", sandbox, func(t *testing.T, root string) {
			createStacks(t, root, []stackMeta{{Path: "stacks/tenants/demo/dev/gra11/legacy", ID: "demo-dev-gra11-legacy",
				Tags: []string{"lz-stage-runtime", "lz-scope-region", "lz-tenant-demo", "lz-env-dev", "lz-region-gra11"}}})
		}, "stacks/tenants/demo/dev/gra11/legacy"},
		{"account directory without a row", sandbox, func(t *testing.T, root string) {
			createStacks(t, root, []stackMeta{{Path: "stacks/account/old-governance", ID: "account-old-governance",
				Tags: []string{"lz-stage-account-governance", "lz-scope-account"}}})
		}, "stacks/account/old-governance"},
		{"changed id", edit(t, sandbox, `"id": "demo-dev-project"`, `"id": "demo-dev-proj"`, 1), nil, project.Path},
		{"changed dimension (slot)", edit(t, sandbox,
			`"stage": "runtime", "tenant": "demo", "environment": "dev", "region": "GRA11"}`,
			`"stage": "runtime", "tenant": "demo", "environment": "dev", "region": "GRA11", "slot": "blue"}`, 1),
			func(t *testing.T, root string) { createStacks(t, root, []stackMeta{runtime}) }, runtime.Path},
		{"stack moved to another path", sandbox, func(t *testing.T, root string) {
			moved := project
			moved.Path = "stacks/tenants/demo/dev/project-old"
			if err := os.Rename(filepath.Join(root, project.Path), filepath.Join(root, moved.Path)); err != nil {
				t.Fatal(err)
			}
		}, "stacks/tenants/demo/dev/project-old"},
		{"drifted tags", sandbox, func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, network.Path, "stack.tm.hcl"), []byte(fmt.Sprintf(
				"stack {\n  name = %q\n  tags = [\"lz-stage-project-network\", \"lz-scope-region\", \"lz-tenant-demo\", \"lz-region-gra11\"]\n  after = [%s]\n  id = %q\n}\n",
				network.ID, quoted(network.After), network.ID)))
		}, network.Path},
		{"drifted after", sandbox, func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, network.Path, "stack.tm.hcl"), []byte(fmt.Sprintf(
				"stack {\n  name = %q\n  tags = [%s]\n  after = [\"tag:lz-stage-account-governance\"]\n  id = %q\n}\n",
				network.ID, quoted(network.Tags), network.ID)))
		}, network.Path},
	}
	for _, c := range cases {
		for _, check := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/check=%v", c.name, check), func(t *testing.T) {
				root := scratchRoot(t, c.manifest)
				createStacks(t, root, base)
				if c.prepare != nil {
					c.prepare(t, root)
				}
				before := snapshot(t, root)
				_, err := runReconcile(t, root, check)
				wantUnsupported(t, err, c.path)
				sameTree(t, "refused run", before, snapshot(t, root))
			})
		}
	}
}

func quoted(list []string) string {
	q := make([]string, len(list))
	for i, s := range list {
		q[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(q, ", ")
}

// Reserved stages are the decoder's STAGE_NOT_IMPLEMENTED (T034 decision 1), which the reconciler
// reports without creating anything.
func TestReconcileStageNotImplemented(t *testing.T) {
	for _, name := range []string{"stage-account-admin.yaml", "stage-account-fabric.yaml", "stage-observability.yaml"} {
		t.Run(name, func(t *testing.T) {
			root := scratchRoot(t, readManifestFixture(t, filepath.Join("invalid", name)))
			before := snapshot(t, root)
			_, err := runReconcile(t, root, false)
			var me *ManifestError
			if !errors.As(err, &me) || me.Code != CodeStageNotImplemented {
				t.Fatalf("want the decoder's %s, got %v", CodeStageNotImplemented, err)
			}
			sameTree(t, "refused run", before, snapshot(t, root))
		})
	}
}

// --check reports what would be created and writes nothing.
func TestReconcileCheckDoesNotWrite(t *testing.T) {
	want := expectedStacks(t, "sandbox")
	for _, c := range []struct {
		name    string
		present []stackMeta
		created []string
	}{
		{"empty tree", nil, paths(want)},
		{"one missing", want[:5], []string{want[5].Path}},
		{"complete tree", want, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := scratchRoot(t, manifestOf(t, "sandbox"))
			createStacks(t, root, c.present)
			before := snapshot(t, root)
			report, err := runReconcile(t, root, true)
			if err != nil {
				t.Fatalf("Reconcile --check: %v", err)
			}
			if !slices.Equal(report.Created, c.created) {
				t.Errorf("check reported %v, want %v", report.Created, c.created)
			}
			sameTree(t, "check run", before, snapshot(t, root))
		})
	}
}

// The same manifest gives a byte-identical tree and report on every run, and the same tree with
// several mismatches the same refusal. Terramate 0.17.3 sorts tags and after when it writes them
// (verified 2026-10-07), so the order an implementation passes them in cannot show here; the
// order in which it walks the stacks it finds can.
func TestReconcileDeterministic(t *testing.T) {
	t.Run("refusal", func(t *testing.T) {
		root := scratchRoot(t, manifestOf(t, "sandbox"))
		createStacks(t, root, expectedStacks(t, "sandbox"))
		for _, name := range []string{"legacy-a", "legacy-b", "legacy-c"} {
			createStacks(t, root, []stackMeta{{Path: "stacks/tenants/demo/dev/" + name, ID: "demo-dev-" + name,
				Tags: []string{"lz-stage-project", "lz-scope-environment", "lz-tenant-demo", "lz-env-dev"}}})
		}
		var first string
		for i := range 16 {
			_, err := runReconcile(t, root, true)
			wantUnsupported(t, err, "")
			if i == 0 {
				first = err.Error()
			} else if err.Error() != first {
				t.Fatalf("run %d refused with %q, run 0 with %q", i, err, first)
			}
		}
	})
	const runs = 3
	var first map[string]string
	var firstReport ReconcileReport
	for i := range runs {
		root := scratchRoot(t, manifestOf(t, "growth"))
		report, err := runReconcile(t, root, false)
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		tree := snapshot(t, root)
		if i == 0 {
			if n := len(stackDirs(t, root)); n != 25 {
				t.Fatalf("run 0 made %d stacks, want 25", n)
			}
			first, firstReport = tree, report
			continue
		}
		sameTree(t, fmt.Sprintf("run %d against run 0", i), first, tree)
		if !reflect.DeepEqual(report, firstReport) {
			t.Fatalf("run %d report %v, run 0 %v", i, report, firstReport)
		}
	}
}
