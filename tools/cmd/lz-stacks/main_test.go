package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

// lz-stacks controls of 005 T036: `reconcile` (task stacks:reconcile, host) and `check`
// (task stacks:check, offline, scratch copy). They run the pinned Terramate on scratch candidates
// made from the P17 root seed, a generate config and the sandbox manifest fixture.

const repoRoot = "../../.."

// pinned sets LZ_TERRAMATE to the pinned Terramate for the run under test.
func pinned(t *testing.T) string {
	t.Helper()
	path, err := stacks.PinnedTerramate(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("LZ_TERRAMATE", path)
	return path
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dst, data)
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

// candidate makes a checkout-like root: mise.toml, a Terramate root config, a generate config
// and, when manifest is true, the sandbox manifest as stacks/deployments.yaml.
func candidate(t *testing.T, manifest bool) string {
	t.Helper()
	root := t.TempDir()
	copyFile(t, filepath.Join(repoRoot, "mise.toml"), filepath.Join(root, "mise.toml"))
	copyFile(t, filepath.Join(repoRoot, "tests/fixtures/terramate/p17/root.tm.hcl.seed"), filepath.Join(root, "terramate.tm.hcl"))
	copyFile(t, filepath.Join(repoRoot, "tests/fixtures/terramate/p17/generate.tm.hcl.seed"), filepath.Join(root, "lz_generate.tm.hcl"))
	writeFile(t, filepath.Join(root, "stacks/_lz/lz_globals.tm.hcl"), []byte("globals {\n  lz_marker = \"config only\"\n}\n"))
	if manifest {
		copyFile(t, filepath.Join(repoRoot, "tests/fixtures/manifests/sandbox/deployments.yaml"), filepath.Join(root, "stacks/deployments.yaml"))
	}
	return root
}

// sandboxPaths are the stack paths of the sandbox fixture in manifest row order.
func sandboxPaths(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, "tests/fixtures/terramate/reconcile/sandbox.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Stacks []struct {
			Path string `json:"path"`
		} `json:"stacks"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, s := range doc.Stacks {
		out = append(out, s.Path)
	}
	if len(out) != 6 {
		t.Fatalf("sandbox expectation lists %d stacks, want 6", len(out))
	}
	return out
}

func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			tree[rel+"/"] = ""
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			tree[rel] = "symlink to " + target
			return err
		}
		data, err := os.ReadFile(path)
		tree[rel] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func unchanged(t *testing.T, what string, before map[string]string, root string) {
	t.Helper()
	if after := snapshot(t, root); !reflect.DeepEqual(before, after) {
		t.Fatalf("%s wrote to the candidate", what)
	}
}

func lzStacks(t *testing.T, root string, args ...string) (int, []string) {
	t.Helper()
	var out strings.Builder
	code := run(append([]string{"-root", root}, args...), &out)
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return code, lines
}

func terramate(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command(os.Getenv("LZ_TERRAMATE"), append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "TM_DISABLE_CHECKPOINT=true")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("terramate %v: %v: %s", args, err, out)
	}
}

func prefixed(prefix string, paths []string) []string {
	var out []string
	for _, p := range paths {
		out = append(out, prefix+p)
	}
	return out
}

// Until the repository has a manifest (T039), stacks:check reports `fail: no manifest`.
func TestStacksCheckNoManifest(t *testing.T) {
	pinned(t)
	root := candidate(t, false)
	before := snapshot(t, root)
	code, lines := lzStacks(t, root, "check")
	if code != 1 || !slices.Equal(lines, []string{"fail: no manifest"}) {
		t.Fatalf("check without a manifest: exit %d, %q; want 1, [fail: no manifest]", code, lines)
	}
	unchanged(t, "check", before, root)
}

// stacks:check works on a scratch copy, fails on every missing stack and every stale or missing
// generated file, naming them, and passes on a reconciled and generated candidate.
func TestStacksCheck(t *testing.T) {
	pinned(t)
	stackPaths := sandboxPaths(t)
	generated := func(paths []string) []string {
		var out []string
		for _, p := range paths {
			out = append(out, p+"/_lz_main.tf")
		}
		slices.Sort(out)
		return out
	}
	reconciled := func(t *testing.T, root string) {
		if code, lines := lzStacks(t, root, "reconcile"); code != 0 {
			t.Fatalf("reconcile: exit %d, %q", code, lines)
		}
	}
	cases := []struct {
		name    string
		prepare func(t *testing.T, root string)
		code    int
		want    []string
	}{
		{"missing stacks", nil, 1, prefixed("fail: missing stack ", stackPaths)},
		{"reconciled, not generated", reconciled, 1, prefixed("fail: stale generated file ", generated(stackPaths))},
		{"reconciled and generated", func(t *testing.T, root string) {
			reconciled(t, root)
			terramate(t, root, "generate")
		}, 0, []string{"pass"}},
		{"generated file edited", func(t *testing.T, root string) {
			reconciled(t, root)
			terramate(t, root, "generate")
			path := filepath.Join(root, stackPaths[3], "_lz_main.tf")
			data, _ := os.ReadFile(path)
			writeFile(t, path, append(data, []byte("# hand edit\n")...))
		}, 1, []string{"fail: stale generated file " + stackPaths[3] + "/_lz_main.tf"}},
		{"generated file deleted", func(t *testing.T, root string) {
			reconciled(t, root)
			terramate(t, root, "generate")
			if err := os.Remove(filepath.Join(root, stackPaths[5], "_lz_main.tf")); err != nil {
				t.Fatal(err)
			}
		}, 1, []string{"fail: stale generated file " + stackPaths[5] + "/_lz_main.tf"}},
		{"orphaned generated file", func(t *testing.T, root string) {
			reconciled(t, root)
			terramate(t, root, "generate")
			copyFile(t, filepath.Join(root, stackPaths[2], "_lz_main.tf"), filepath.Join(root, stackPaths[2], "_lz_old.tf"))
		}, 1, []string{"fail: stale generated file " + stackPaths[2] + "/_lz_old.tf"}},
		{"OpenTofu working files ignored", func(t *testing.T, root string) {
			reconciled(t, root)
			terramate(t, root, "generate")
			if err := os.Symlink(t.TempDir(), filepath.Join(root, stackPaths[3], ".terraform")); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(root, stackPaths[4], ".terraform/providers/p"), []byte("provider\n"))
			writeFile(t, filepath.Join(root, stackPaths[4], "terraform.tfstate"), []byte("{}\n"))
			writeFile(t, filepath.Join(root, stackPaths[4], "terraform.tfstate.backup"), []byte("{}\n"))
			writeFile(t, filepath.Join(root, stackPaths[4], "plan.tfplan"), []byte("plan\n"))
		}, 0, []string{"pass"}},
		{"one stack missing, the others generated", func(t *testing.T, root string) {
			reconciled(t, root)
			terramate(t, root, "generate")
			if err := os.RemoveAll(filepath.Join(root, stackPaths[5])); err != nil {
				t.Fatal(err)
			}
		}, 1, []string{"fail: missing stack " + stackPaths[5]}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := candidate(t, true)
			if c.prepare != nil {
				c.prepare(t, root)
			}
			before := snapshot(t, root)
			code, lines := lzStacks(t, root, "check")
			if code != c.code || !slices.Equal(lines, c.want) {
				t.Errorf("check: exit %d, %q; want %d, %q", code, lines, c.code, c.want)
			}
			unchanged(t, "check", before, root)
		})
	}
}

// A reconciler refusal is a check failure naming the refused stack, with nothing written.
func TestStacksCheckRefusal(t *testing.T) {
	pinned(t)
	root := candidate(t, true)
	terramate(t, root, "create", "--id", "demo-dev-legacy", "--tags", "lz-stage-project", "--no-generate", "stacks/tenants/demo/dev/legacy")
	before := snapshot(t, root)
	for _, args := range [][]string{{"check"}, {"reconcile"}, {"reconcile", "-check"}} {
		code, lines := lzStacks(t, root, args...)
		if code != 1 || len(lines) != 1 || !strings.HasPrefix(lines[0], "fail: UNSUPPORTED_CHANGE: stacks/tenants/demo/dev/legacy: ") {
			t.Errorf("%v: exit %d, %q; want 1 and one UNSUPPORTED_CHANGE line naming the stack", args, code, lines)
		}
		unchanged(t, strings.Join(args, " "), before, root)
	}
}

// The scratch copy follows no symlink: a linked Terramate file at the root or under stacks/ is
// refused, so the comparison never reads outside the candidate.
func TestStacksCheckRefusesSymlink(t *testing.T) {
	pinned(t)
	outside := filepath.Join(t.TempDir(), "outside.tm.hcl")
	writeFile(t, outside, []byte("globals {\n  x = 1\n}\n"))
	for _, link := range []string{"linked.tm.hcl", "stacks/_lz/linked.tm.hcl"} {
		root := candidate(t, true)
		if err := os.Symlink(outside, filepath.Join(root, link)); err != nil {
			t.Fatal(err)
		}
		before := snapshot(t, root)
		code, lines := lzStacks(t, root, "check")
		if code != 1 || !slices.Equal(lines, []string{"fail: " + link + ": not a regular file"}) {
			t.Errorf("%s: exit %d, %q; want 1, [fail: %s: not a regular file]", link, code, lines, link)
		}
		unchanged(t, "check", before, root)
	}
}

// stacks:reconcile creates the missing stacks in the checkout and prints them; a repeat run
// prints nothing; -check prints what is missing, fails, and writes nothing.
func TestStacksReconcile(t *testing.T) {
	pinned(t)
	stackPaths := sandboxPaths(t)
	root := candidate(t, true)
	before := snapshot(t, root)
	code, lines := lzStacks(t, root, "reconcile", "-check")
	if code != 1 || !slices.Equal(lines, prefixed("fail: missing stack ", stackPaths)) {
		t.Fatalf("reconcile -check: exit %d, %q", code, lines)
	}
	unchanged(t, "reconcile -check", before, root)
	code, lines = lzStacks(t, root, "reconcile")
	if code != 0 || !slices.Equal(lines, prefixed("created ", stackPaths)) {
		t.Fatalf("reconcile: exit %d, %q; want 0 and one created line per stack in manifest order", code, lines)
	}
	for _, p := range stackPaths {
		if _, err := os.Stat(filepath.Join(root, p, "stack.tm.hcl")); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	for _, args := range [][]string{{"reconcile"}, {"reconcile", "-check"}} {
		if code, lines := lzStacks(t, root, args...); code != 0 || len(lines) != 0 {
			t.Errorf("repeat %v: exit %d, %q; want 0 and no output", args, code, lines)
		}
	}
}

// A Terramate other than the pinned one is refused before anything is read from it or written.
func TestStacksPinnedTerramate(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "terramate")
	writeFile(t, fake, []byte("#!/bin/sh\nif [ \"$1\" = version ]; then echo 0.17.1; exit 0; fi\necho called >&2; exit 3\n"))
	if err := os.Chmod(fake, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LZ_TERRAMATE", fake)
	root := candidate(t, true)
	before := snapshot(t, root)
	for _, args := range [][]string{{"reconcile"}, {"reconcile", "-check"}, {"check"}} {
		code, lines := lzStacks(t, root, args...)
		if code != 1 || len(lines) != 1 || !strings.HasPrefix(lines[0], "fail: ") || !strings.Contains(lines[0], `"0.17.1"`) || !strings.Contains(lines[0], "0.17.3") {
			t.Errorf("%v with terramate 0.17.1: exit %d, %q; want 1 and one fail line naming both versions", args, code, lines)
		}
	}
	unchanged(t, "refused runs", before, root)
}

// stacks:generate (005 T038, host, credential-free, unguarded): generates the reconciled stacks
// in the checkout and prints one `generated <path>` line per file it wrote, sorted; a repeat run
// prints nothing. Without a manifest it reports `fail: no manifest`; a `git` stage source is
// refused with nothing written.
func TestStacksGenerate(t *testing.T) {
	pinned(t)
	root := candidate(t, true)
	if code, lines := lzStacks(t, root, "reconcile"); code != 0 || len(lines) != 6 {
		t.Fatalf("reconcile: exit %d, %q", code, lines)
	}
	var want []string
	for _, p := range sandboxPaths(t) {
		want = append(want, "generated "+p+"/_lz_main.tf")
	}
	slices.Sort(want)
	code, lines := lzStacks(t, root, "generate")
	if code != 0 || !slices.Equal(lines, want) {
		t.Fatalf("generate: exit %d, %q; want 0 and %q", code, lines, want)
	}
	for _, p := range sandboxPaths(t) {
		if _, err := os.Stat(filepath.Join(root, p, "_lz_main.tf")); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	if code, lines := lzStacks(t, root, "generate"); code != 0 || len(lines) != 0 {
		t.Errorf("repeat generate: exit %d, %q; want 0 and no output", code, lines)
	}

	empty := candidate(t, false)
	before := snapshot(t, empty)
	if code, lines := lzStacks(t, empty, "generate"); code != 1 || !slices.Equal(lines, []string{"fail: no manifest"}) {
		t.Errorf("generate without a manifest: exit %d, %q", code, lines)
	}
	unchanged(t, "generate without a manifest", before, empty)

	git := candidate(t, true)
	manifest := filepath.Join(git, "stacks/deployments.yaml")
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, manifest, []byte(strings.Replace(string(data), `"stage_source": {"kind": "local"}`, `"stage_source": {"kind": "git"}`, 1)))
	if code, lines := lzStacks(t, git, "reconcile"); code != 0 || len(lines) != 6 {
		t.Fatalf("reconcile (git source): exit %d, %q", code, lines)
	}
	before = snapshot(t, git)
	if code, lines := lzStacks(t, git, "generate"); code != 1 || len(lines) != 1 || !strings.HasPrefix(lines[0], "fail: "+stacks.CodeStageSourceNotImplemented) {
		t.Errorf("generate with a git stage source: exit %d, %q", code, lines)
	}
	unchanged(t, "generate with a git stage source", before, git)
}

// test:stack-plans reports `fail: no manifest` until the repository has one (T039): zero
// discovery is never a pass.
func TestStacksPlansNoManifest(t *testing.T) {
	root := candidate(t, false)
	before := snapshot(t, root)
	if code, lines := lzStacks(t, root, "plans"); code != 1 || !slices.Equal(lines, []string{"fail: no manifest"}) {
		t.Fatalf("plans without a manifest: exit %d, %q; want 1, [fail: no manifest]", code, lines)
	}
	unchanged(t, "plans", before, root)
}

// fakeTofu installs a tofu that reports version and answers everything else with exit 3, as
// LZ_TOFU.
func fakeTofu(t *testing.T, version string) {
	t.Helper()
	fake := filepath.Join(t.TempDir(), "tofu")
	writeFile(t, fake, []byte("#!/bin/sh\nif [ \"$1\" = version ]; then echo '{\"terraform_version\":\""+version+"\"}'; exit 0; fi\necho \"fake tofu $*\" >&2; exit 3\n"))
	if err := os.Chmod(fake, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LZ_TOFU", fake)
}

// plans fails, naming each stack, when the stacks are not generated; a tofu other than the
// pinned one is refused before anything is planned; neither writes to the candidate.
func TestStacksPlansFail(t *testing.T) {
	pinned(t)
	root := candidate(t, true)
	if code, lines := lzStacks(t, root, "reconcile"); code != 0 || len(lines) != 6 {
		t.Fatalf("reconcile: exit %d, %q", code, lines)
	}
	before := snapshot(t, root)
	fakeTofu(t, "1.13.0")
	code, lines := lzStacks(t, root, "plans")
	var want []string
	for _, p := range sandboxPaths(t) {
		want = append(want, "fail: "+p+": not generated: no tests/_lz_offline.tftest.hcl (run stacks:generate)")
	}
	if code != 1 || !slices.Equal(lines, want) {
		t.Errorf("plans of ungenerated stacks: exit %d, %q; want 1 and %q", code, lines, want)
	}
	fakeTofu(t, "1.10.3")
	if code, lines := lzStacks(t, root, "plans"); code != 1 || len(lines) != 1 || !strings.Contains(lines[0], `"1.10.3"`) || !strings.Contains(lines[0], "1.13.0") {
		t.Errorf("plans with tofu 1.10.3: exit %d, %q; want 1 and one fail line naming both versions", code, lines)
	}
	unchanged(t, "plans", before, root)
}

func TestStacksUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"order"}, {"check", "extra"}, {"reconcile", "extra"}, {"-bogus", "check"}, {"generate", "extra"}, {"plans", "extra"}} {
		var out strings.Builder
		if code := run(args, &out); code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
	}
}

// `lz-stacks order` (task stacks:order, 005 T041) on this repository's stacks: the sandbox run
// order grouped by level and a reason per selected stack; records select by code digest, a
// selected producer selects its data consumers, an instance argument narrows the report.
func TestStacksOrder(t *testing.T) {
	const wantOrder = "order account-bootstrap → {account-governance, demo-state} → demo-dev-project → {demo-dev-gra11-network, demo-dev-gra11-runtime}"
	const offlineNote = "note: published artefacts and resolved references are compared by the live lane, not here"
	records := filepath.Join(t.TempDir(), "records")
	if err := os.Mkdir(records, 0o700); err != nil {
		t.Fatal(err)
	}
	code, lines := lzStacks(t, repoRoot, "order", "-records", records, "all")
	want := []string{wantOrder,
		"selected account-bootstrap no-record",
		"selected account-governance no-record",
		"selected demo-state no-record",
		"selected demo-dev-project no-record",
		"selected demo-dev-gra11-network no-record,consumer:demo-dev-project blocked-on=demo-dev-project",
		"selected demo-dev-gra11-runtime no-record,consumer:demo-dev-project blocked-on=demo-dev-project",
		"note: no records: every stack is selected as on a first run",
		offlineNote}
	if code != 0 || !slices.Equal(lines, want) {
		t.Fatalf("order all (first run): exit %d\n%s\nwant\n%s", code, strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	// Without -records the default directory is read; a checkout without one is a first run.
	if _, err := os.Stat(filepath.Join(repoRoot, ".local/live/records")); os.IsNotExist(err) {
		if code, lines := lzStacks(t, repoRoot, "order", "all"); code != 0 || !slices.Equal(lines, want) {
			t.Errorf("order all (default records, none): exit %d, %q; want the first-run report", code, lines)
		}
	}

	// Records of an apply of this tree: nothing is selected.
	data, err := os.ReadFile(filepath.Join(repoRoot, stacks.ManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	m, err := stacks.DecodeManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	write := func(id string, r stacks.Record) {
		t.Helper()
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(records, id+".json"), b)
	}
	recs := map[string]stacks.Record{}
	for _, in := range m.Instances {
		d, err := stacks.CodeDigest(repoRoot, in)
		if err != nil {
			t.Fatal(err)
		}
		r := stacks.Record{CodeDigest: d, Consumed: map[string]string{}}
		for _, e := range in.Edges {
			if e.Kind == stacks.EdgeData {
				r.Consumed[e.Producer] = "sha-of-" + e.Producer
			}
		}
		recs[in.ID] = r
		write(in.ID, r)
	}
	if code, lines := lzStacks(t, repoRoot, "order", "-records", records, "all"); code != 0 || !slices.Equal(lines, []string{wantOrder, offlineNote}) {
		t.Errorf("order all (applied, unchanged): exit %d, %q; want only the order and the note", code, lines)
	}

	// A code change recorded for the project selects it and its data consumers, not its producers.
	r := recs["demo-dev-project"]
	r.CodeDigest = "older"
	write("demo-dev-project", r)
	want = []string{wantOrder,
		"selected demo-dev-project code",
		"selected demo-dev-gra11-network consumer:demo-dev-project",
		"selected demo-dev-gra11-runtime consumer:demo-dev-project",
		offlineNote}
	if code, lines := lzStacks(t, repoRoot, "order", "-records", records, "all"); code != 0 || !slices.Equal(lines, want) {
		t.Errorf("order all (project code changed): exit %d\n%s\nwant\n%s", code, strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	for target, line := range map[string]string{
		"demo-dev-gra11-runtime": "selected demo-dev-gra11-runtime consumer:demo-dev-project",
		"account-bootstrap":      "unselected account-bootstrap",
	} {
		if code, lines := lzStacks(t, repoRoot, "order", "-records", records, target); code != 0 || !slices.Equal(lines, []string{wantOrder, line, offlineNote}) {
			t.Errorf("order %s: exit %d, %q; want the order, %q and the note", target, code, lines, line)
		}
	}

	// Consumers that recorded different digests of one producer: its artefact is unknown, so they
	// are selected on their input.
	n := recs["demo-dev-gra11-network"]
	n.Consumed = map[string]string{"demo-dev-project": "another-sha"}
	write("demo-dev-gra11-network", n)
	write("demo-dev-project", recs["demo-dev-project"])
	// The project has a record, so it is not reported as unpublished (review r1).
	want = []string{wantOrder,
		"selected demo-dev-gra11-network input:demo-dev-project",
		"selected demo-dev-gra11-runtime input:demo-dev-project",
		offlineNote}
	if code, lines := lzStacks(t, repoRoot, "order", "-records", records, "all"); code != 0 || !slices.Equal(lines, want) {
		t.Errorf("order all (disagreeing consumers): exit %d\n%s\nwant\n%s", code, strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}

	// The same without the project's own record: the consumers recorded consuming it, so it
	// published; they are selected (input, consumer), not blocked on it (review r2).
	if err := os.Remove(filepath.Join(records, "demo-dev-project.json")); err != nil {
		t.Fatal(err)
	}
	want = []string{wantOrder,
		"selected demo-dev-project no-record",
		"selected demo-dev-gra11-network input:demo-dev-project,consumer:demo-dev-project",
		"selected demo-dev-gra11-runtime input:demo-dev-project,consumer:demo-dev-project",
		offlineNote}
	if code, lines := lzStacks(t, repoRoot, "order", "-records", records, "all"); code != 0 || !slices.Equal(lines, want) {
		t.Errorf("order all (disagreeing consumers, producer without record): exit %d\n%s\nwant\n%s", code, strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	write("demo-dev-project", recs["demo-dev-project"])

	// A consumer added after its producer was applied (no record of its own, nobody else recorded
	// the producer's digest) is selected, not blocked on the applied producer.
	if err := os.Remove(filepath.Join(records, "demo-dev-gra11-network.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(records, "demo-dev-gra11-runtime.json")); err != nil {
		t.Fatal(err)
	}
	want = []string{wantOrder,
		"selected demo-dev-gra11-network no-record",
		"selected demo-dev-gra11-runtime no-record",
		offlineNote}
	if code, lines := lzStacks(t, repoRoot, "order", "-records", records, "all"); code != 0 || !slices.Equal(lines, want) {
		t.Errorf("order all (new consumers of an applied project): exit %d\n%s\nwant\n%s", code, strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}

	// A relative -records is the root's, not the process directory's (tools/ under task); a
	// -records that does not exist is refused, never read as a first run (review r1).
	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(absRoot, records)
	if err != nil {
		t.Fatal(err)
	}
	if code, lines := lzStacks(t, repoRoot, "order", "-records", rel, "all"); code != 0 || !slices.Equal(lines, want) {
		t.Errorf("order -records %s (relative to the root): exit %d, %q; want %q", rel, code, lines, want)
	}
	if code, lines := lzStacks(t, repoRoot, "order", "-records", filepath.Join(records, "absent"), "all"); code != 1 || len(lines) != 1 || !strings.HasPrefix(lines[0], "fail: ") {
		t.Errorf("order -records <missing>: exit %d, %q; want 1 and one fail line", code, lines)
	}

	// Refusals: an id that is not a row, a malformed record.
	if code, lines := lzStacks(t, repoRoot, "order", "-records", records, "unknown"); code != 1 || len(lines) != 1 || !strings.HasPrefix(lines[0], "fail: ") {
		t.Errorf("order unknown: exit %d, %q; want 1 and one fail line", code, lines)
	}
	writeFile(t, filepath.Join(records, "demo-state.json"), []byte(`{"code_digest": `))
	if code, lines := lzStacks(t, repoRoot, "order", "-records", records, "all"); code != 1 || len(lines) != 1 || !strings.Contains(lines[0], "demo-state") {
		t.Errorf("order with a malformed record: exit %d, %q; want 1 and a fail line naming it", code, lines)
	}
}
