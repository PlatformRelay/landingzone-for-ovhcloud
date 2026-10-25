package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/configure"
)

// lz-configure controls of 004 T020 (V004, V005; G31): each case runs in a temporary root holding
// a copy of stacks/deployments.yaml, with an inventory of every path under the root before and
// after the run. The answers fixtures are read from the repository, outside the root, unless a
// case copies one in.

const repoRoot = "../../.."

// The fixtures are absolute: the command resolves a relative answers path against -root.
var (
	sandboxAnswers    = cfgAbs("tests/fixtures/answers/sandbox.yaml")
	twoTenantAnswers  = cfgAbs("tests/fixtures/answers/sandbox-two-tenants.yaml")
	twoTenantManifest = cfgAbs("tests/fixtures/manifests/sandbox-two-tenants/deployments.yaml")
	stagingRel        = ".local/configure/deployments.yaml"
)

func cfgAbs(p string) string {
	abs, err := filepath.Abs(filepath.Join(repoRoot, p))
	if err != nil {
		panic(err)
	}
	return abs
}

// cfgRead reads a file the test needs, failing the test when it cannot.
func cfgRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func cfgWrite(t *testing.T, path string, data []byte, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		t.Fatal(err)
	}
}

// cfgRoot is a temporary root holding a copy of stacks/deployments.yaml (the sandbox manifest).
func cfgRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cfgWrite(t, filepath.Join(root, "stacks/deployments.yaml"), cfgRead(t, filepath.Join(repoRoot, "stacks/deployments.yaml")), 0o644)
	return root
}

// cfgEntry is what the inventory keeps of one path: its type and permission bits, and the bytes
// of a regular file or the target of a symlink.
type cfgEntry struct {
	mode    fs.FileMode
	content string
}

// cfgInventory lists every path under root (symlinks not followed), relative to it.
func cfgInventory(t *testing.T, root string) map[string]cfgEntry {
	t.Helper()
	inv := map[string]cfgEntry{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if rel == "." {
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		e := cfgEntry{mode: info.Mode() & (fs.ModeType | fs.ModePerm)}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			e.content, err = os.Readlink(path)
		case info.Mode().IsRegular():
			var data []byte
			data, err = os.ReadFile(path)
			e.content = string(data)
		}
		inv[filepath.ToSlash(rel)] = e
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return inv
}

// cfgChanged is the paths added, removed or changed between two inventories, sorted.
func cfgChanged(before, after map[string]cfgEntry) []string {
	var changed []string
	for p, e := range after {
		if b, ok := before[p]; !ok || b != e {
			changed = append(changed, p)
		}
	}
	for p := range before {
		if _, ok := after[p]; !ok {
			changed = append(changed, p)
		}
	}
	slices.Sort(changed)
	return changed
}

// cfgRun runs the command with the given operations and returns its exit status and output lines.
func cfgRun(t *testing.T, ops stagingOps, args ...string) (int, []string) {
	t.Helper()
	var out bytes.Buffer
	code := run(args, &out, ops)
	return code, strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
}

// cfgExpected is what a successful check prints for the answers against the current manifest:
// T019's diff, then the notes, then `configure: ok`.
func cfgExpected(t *testing.T, answers string, current []byte) []string {
	t.Helper()
	a, err := configure.DecodeAnswers(cfgRead(t, answers))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := configure.Export(a)
	if err != nil {
		t.Fatal(err)
	}
	diff, err := configure.Diff(current, manifest)
	if err != nil {
		t.Fatal(err)
	}
	return append(append(diff, configure.Notes(a)...), "configure: ok")
}

// cfgValue is a manifest's JSON value, whole-line comments removed.
func cfgValue(t *testing.T, data []byte) any {
	t.Helper()
	var lines []string
	for _, l := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "#") {
			lines = append(lines, l)
		}
	}
	var v any
	if err := json.Unmarshal([]byte(strings.Join(lines, "\n")), &v); err != nil {
		t.Fatalf("not one JSON-compatible document: %v", err)
	}
	return v
}

// cfgNextSteps is the end of a successful export's output (FR-007).
var cfgNextSteps = []string{
	"wrote .local/configure/deployments.yaml",
	"next: copy .local/configure/deployments.yaml to stacks/deployments.yaml",
	"next: task stacks:reconcile",
	"next: task stacks:generate",
	"next: task stacks:check",
}

// cfgRefusal asserts one refusal line with the code, exit 1.
func cfgRefusal(t *testing.T, code int, lines []string, want string) {
	t.Helper()
	if code != 1 || len(lines) != 1 || !strings.HasPrefix(lines[0], "configure: "+want+": ") {
		t.Errorf("want exit 1 and one %s line, got exit %d, %q", want, code, lines)
	}
}

// TestCheckTwoTenants: check prints the diff, the notes and `configure: ok`, exit 0, and changes
// nothing under the root (exposes a write during check, G31). The answers path is relative to the
// root.
func TestCheckTwoTenants(t *testing.T) {
	root := cfgRoot(t)
	cfgWrite(t, filepath.Join(root, "answers.yaml"), cfgRead(t, twoTenantAnswers), 0o600)
	before := cfgInventory(t, root)
	code, lines := cfgRun(t, osStaging, "-root", root, "check", "answers.yaml")
	want := cfgExpected(t, twoTenantAnswers, cfgRead(t, filepath.Join(root, "stacks/deployments.yaml")))
	if code != 0 || !reflect.DeepEqual(lines, want) {
		t.Errorf("check: exit %d, got\n%s\nwant\n%s", code, strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	// literal anchors, so the expectation is not only what the package prints
	if !slices.Contains(lines, "diff: 4 added, 0 removed") || !slices.ContainsFunc(lines, func(l string) bool {
		return strings.HasPrefix(l, "note: account.env needs LZ_PROJECT_ID_STATE, LZ_PROJECT_ID_DEMO_DEV, LZ_PROJECT_ID_LAB_DEV")
	}) {
		t.Errorf("check: want the four added rows and the account.env names, got %q", lines)
	}
	if changed := cfgChanged(before, cfgInventory(t, root)); len(changed) > 0 {
		t.Errorf("check changed %v; want nothing", changed)
	}
}

// TestCheckWithoutCurrentManifest: without stacks/deployments.yaml every row is `+` (T019: nil to
// Diff, not a refusal); control: the same answers against the copy give `diff: none`.
func TestCheckWithoutCurrentManifest(t *testing.T) {
	root := cfgRoot(t)
	code, lines := cfgRun(t, osStaging, "-root", root, "check", sandboxAnswers)
	if code != 0 || !slices.Contains(lines, "diff: none") {
		t.Errorf("control: want exit 0 and diff: none, got exit %d, %q", code, lines)
	}
	if err := os.Remove(filepath.Join(root, "stacks/deployments.yaml")); err != nil {
		t.Fatal(err)
	}
	code, lines = cfgRun(t, osStaging, "-root", root, "check", sandboxAnswers)
	if want := cfgExpected(t, sandboxAnswers, nil); code != 0 || !reflect.DeepEqual(lines, want) {
		t.Errorf("no manifest: exit %d, got %q want %q", code, lines, want)
	}
}

// TestExportTwoTenants: with no .local, export creates only .local, .local/configure (0700) and
// the staging file (0600), value-equal to the two-tenant fixture, and ends with the copy step and
// the three stack commands (exposes a write elsewhere or missing next steps).
func TestExportTwoTenants(t *testing.T) {
	root := cfgRoot(t)
	before := cfgInventory(t, root)
	code, lines := cfgRun(t, osStaging, "-root", root, "export", twoTenantAnswers)
	after := cfgInventory(t, root)
	if got := cfgChanged(before, after); !reflect.DeepEqual(got, []string{".local", ".local/configure", stagingRel}) {
		t.Errorf("export changed %v; want only .local, .local/configure and %s", got, stagingRel)
	}
	if m := after[".local/configure"].mode; m != fs.ModeDir|0o700 {
		t.Errorf(".local/configure mode %v; want drwx------", m)
	}
	if m := after[stagingRel].mode; m != 0o600 {
		t.Errorf("staging file mode %v; want -rw-------", m)
	}
	if code != 0 {
		t.Fatalf("export: exit %d, %q", code, lines)
	}
	if !reflect.DeepEqual(cfgValue(t, []byte(after[stagingRel].content)), cfgValue(t, cfgRead(t, twoTenantManifest))) {
		t.Errorf("staging file is not value-equal to %s", twoTenantManifest)
	}
	check := cfgExpected(t, twoTenantAnswers, cfgRead(t, filepath.Join(root, "stacks/deployments.yaml")))
	if want := append(check, cfgNextSteps...); !reflect.DeepEqual(lines, want) {
		t.Errorf("export output\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

// TestExportTightensLooseDirectory: an existing .local/configure with a group or other bit ends
// 0700 and the file is written; control: an existing 0700 directory stays 0700 (G31: exposes a
// looser directory kept).
func TestExportTightensLooseDirectory(t *testing.T) {
	for _, perm := range []fs.FileMode{0o755, 0o750, 0o701, 0o700} {
		t.Run(perm.String(), func(t *testing.T) { cfgTightenRow(t, perm) })
	}
}

func cfgTightenRow(t *testing.T, perm fs.FileMode) {
	root := cfgRoot(t)
	dir := filepath.Join(root, ".local/configure")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, perm); err != nil {
		t.Fatal(err)
	}
	code, lines := cfgRun(t, osStaging, "-root", root, "export", sandboxAnswers)
	info, err := os.Lstat(dir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("directory at %v: ends %v, %v; want 0700", perm, info.Mode(), err)
	}
	if _, err := os.Lstat(filepath.Join(root, stagingRel)); code != 0 || err != nil {
		t.Errorf("want the file written, exit 0; got exit %d, %v, %q", code, err, lines)
	}
}

// TestExportRefusesSymlinks: a symlinked .local, .local/configure or target is STAGING_SYMLINK
// with nothing written, under the root or where the link points; control: the same tree with a
// real directory or no target exports (G31: exposes a followed symlink).
func TestExportRefusesSymlinks(t *testing.T) {
	for _, link := range []string{".local", ".local/configure", stagingRel} {
		t.Run(link, func(t *testing.T) { cfgSymlinkRow(t, link) })
	}
}

func cfgSymlinkRow(t *testing.T, link string) {
	root := cfgRoot(t)
	elsewhere := cfgPlantLink(t, root, link)
	before, beforeDest := cfgInventory(t, root), cfgInventory(t, elsewhere)
	code, lines := cfgRun(t, osStaging, "-root", root, "export", sandboxAnswers)
	cfgRefusal(t, code, lines, configure.CodeStagingSymlink)
	if changed := cfgChanged(before, cfgInventory(t, root)); len(changed) > 0 {
		t.Errorf("changed under the root: %v", changed)
	}
	if changed := cfgChanged(beforeDest, cfgInventory(t, elsewhere)); len(changed) > 0 {
		t.Errorf("changed where the link points: %v", changed)
	}
	// control: the link removed (a real directory in its place, or no target)
	if err := os.Remove(filepath.Join(root, link)); err != nil {
		t.Fatal(err)
	}
	if link != stagingRel {
		if err := os.Mkdir(filepath.Join(root, link), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if code, lines := cfgRun(t, osStaging, "-root", root, "export", sandboxAnswers); code != 0 {
		t.Errorf("control without the link: exit %d, %q", code, lines)
	}
}

// cfgPlantLink makes link under root a symlink to a directory (for the target, a file) in a
// temporary directory outside the root, and returns that directory.
func cfgPlantLink(t *testing.T, root, link string) string {
	t.Helper()
	elsewhere := t.TempDir()
	dest := filepath.Join(elsewhere, "dest")
	if link == stagingRel {
		cfgWrite(t, dest, []byte("elsewhere\n"), 0o600)
	} else if err := os.Mkdir(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, link)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dest, filepath.Join(root, link)); err != nil {
		t.Fatal(err)
	}
	return elsewhere
}

var errInjected = errors.New("injected failure")

// TestExportFailureBeforeRename: a failure of the temporary write, its fsync or the rename is
// STAGING_WRITE, exit 1, with the previous staging file byte-identical and no temporary file left
// (G31: exposes an in-place write). Control: the same operations without the failure replace it.
func TestExportFailureBeforeRename(t *testing.T) {
	cases := map[string]func(*stagingOps){
		"write":  func(o *stagingOps) { o.write = func(*os.File, []byte) error { return errInjected } },
		"fsync":  func(o *stagingOps) { o.sync = func(*os.File) error { return errInjected } },
		"rename": func(o *stagingOps) { o.rename = func(*os.Root, string, string) error { return errInjected } },
		// a write that reaches the file before it fails: an in-place write would leave it partial
		"partial write": func(o *stagingOps) {
			o.write = func(f *os.File, data []byte) error {
				if _, err := f.Write(data[:len(data)/2]); err != nil {
					return err
				}
				return errInjected
			}
		},
	}
	for name, inject := range cases {
		t.Run(name, func(t *testing.T) {
			root := cfgRoot(t)
			old := []byte("previous staging file\n")
			cfgWrite(t, filepath.Join(root, stagingRel), old, 0o600)
			if err := os.Chmod(filepath.Join(root, ".local/configure"), 0o700); err != nil {
				t.Fatal(err)
			}
			ops := osStaging
			inject(&ops)
			code, lines := cfgRun(t, ops, "-root", root, "export", sandboxAnswers)
			cfgRefusal(t, code, lines, configure.CodeStagingWrite)
			if got := cfgRead(t, filepath.Join(root, stagingRel)); !bytes.Equal(got, old) {
				t.Errorf("previous staging file changed: %q", got)
			}
			cfgOnlyStaging(t, root)
			if code, lines := cfgRun(t, osStaging, "-root", root, "export", sandboxAnswers); code != 0 || bytes.Equal(cfgRead(t, filepath.Join(root, stagingRel)), old) {
				t.Errorf("control: want the file replaced, exit 0; got exit %d, %q", code, lines)
			}
		})
	}
}

// cfgOnlyStaging asserts that .local/configure holds the staging file and nothing else.
func cfgOnlyStaging(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, ".local/configure"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !reflect.DeepEqual(names, []string{"deployments.yaml"}) {
		t.Errorf(".local/configure holds %v; want deployments.yaml only (no temporary file)", names)
	}
}

// TestExportDirectorySyncFailure: a directory fsync failure after the rename is
// STAGING_NOT_DURABLE, exit 1, with the new file in place, no temporary file and no
// `configure: ok` (G31: exposes a published file reported as unchanged, or a durability failure
// reported as success). Control: the operations run in data-model's order, write, file fsync,
// rename onto deployments.yaml, then one fsync of .local/configure itself.
func TestExportDirectorySyncFailure(t *testing.T) {
	root := cfgRoot(t)
	cfgWrite(t, filepath.Join(root, stagingRel), []byte("previous staging file\n"), 0o600)
	calls, ops := cfgRecorded(t, root)
	code, lines := cfgRun(t, ops, "-root", root, "export", twoTenantAnswers)
	cfgRefusal(t, code, lines, configure.CodeStagingNotDurable)
	if !reflect.DeepEqual(cfgValue(t, cfgRead(t, filepath.Join(root, stagingRel))), cfgValue(t, cfgRead(t, twoTenantManifest))) {
		t.Errorf("the new staging file is not in place")
	}
	cfgOnlyStaging(t, root)
	if want := []string{"write", "fsync", "rename deployments.yaml", "sync .local/configure"}; !reflect.DeepEqual(*calls, want) {
		t.Errorf("calls %q; want %q", *calls, want)
	}
}

// cfgRecorded is the real staging operations, each recording its call, with a directory fsync that
// records whether it got .local/configure under root and then fails.
func cfgRecorded(t *testing.T, root string) (*[]string, stagingOps) {
	var calls []string
	ops := osStaging
	ops.write = func(f *os.File, data []byte) error {
		calls = append(calls, "write")
		return osStaging.write(f, data)
	}
	ops.sync = func(f *os.File) error {
		calls = append(calls, "fsync")
		return osStaging.sync(f)
	}
	ops.rename = func(dir *os.Root, oldname, newname string) error {
		calls = append(calls, "rename "+newname)
		return osStaging.rename(dir, oldname, newname)
	}
	ops.syncDir = func(dir *os.File) error {
		got, err := dir.Stat()
		want, lerr := os.Lstat(filepath.Join(root, ".local/configure"))
		if err != nil || lerr != nil || !os.SameFile(got, want) {
			calls = append(calls, "sync another directory")
		} else {
			calls = append(calls, "sync .local/configure")
		}
		return errInjected
	}
	return &calls, ops
}

// TestCheckRemovedRows: a current manifest with rows the answers drop prints the removed rows,
// then the warning line, then the notes and `configure: ok`, exit 0 (FR-005: a predicted refusal
// is a warning, not a refusal).
func TestCheckRemovedRows(t *testing.T) {
	root := cfgRoot(t)
	cfgWrite(t, filepath.Join(root, "stacks/deployments.yaml"), cfgRead(t, twoTenantManifest), 0o644)
	code, lines := cfgRun(t, osStaging, "-root", root, "check", sandboxAnswers)
	want := cfgExpected(t, sandboxAnswers, cfgRead(t, twoTenantManifest))
	warning := slices.Index(lines, "configure: warning: stacks:reconcile is expected to refuse this export (4 removed rows)")
	if code != 0 || !reflect.DeepEqual(lines, want) || warning < 0 || !strings.HasPrefix(lines[warning+1], "note: ") || lines[len(lines)-1] != "configure: ok" {
		t.Errorf("check: exit %d, got\n%s\nwant the four removed rows, the warning, the notes and configure: ok", code, strings.Join(lines, "\n"))
	}
}

// TestOpenDirRefusesLink: openDir refuses a name that is a symlink (to a directory inside the
// parent, which an os.Root would follow) as STAGING_SYMLINK and a regular file as STAGING_WRITE,
// without the command's earlier check (exposes a link swapped in after it); control: a real
// directory opens. Through the command, a regular file at .local is STAGING_WRITE with nothing
// changed.
func TestOpenDirRefusesLink(t *testing.T) {
	base := t.TempDir()
	cfgWrite(t, filepath.Join(base, "real/keep"), []byte("x"), 0o600)
	cfgWrite(t, filepath.Join(base, "file"), []byte("x"), 0o600)
	if err := os.Symlink("real", filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	parent, err := os.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	for name, code := range map[string]string{"link": configure.CodeStagingSymlink, "file": configure.CodeStagingWrite} {
		r, err := openDir(parent, name, name)
		if e, ok := err.(*configure.AnswersError); !ok || e.Code != code || r != nil {
			t.Errorf("%s: got %v, %v; want %s", name, r, err, code)
		}
	}
	if r, err := openDir(parent, "real", "real"); err != nil {
		t.Errorf("control: real directory: %v", err)
	} else {
		r.Close()
	}
	root := cfgRoot(t)
	cfgWrite(t, filepath.Join(root, ".local"), []byte("a file\n"), 0o600)
	before := cfgInventory(t, root)
	code, lines := cfgRun(t, osStaging, "-root", root, "export", sandboxAnswers)
	cfgRefusal(t, code, lines, configure.CodeStagingWrite)
	if changed := cfgChanged(before, cfgInventory(t, root)); len(changed) > 0 {
		t.Errorf(".local as a file: changed %v", changed)
	}
}

// TestRefusalsWriteNothing: an invalid answers file, a refused generated manifest, a missing
// answers path, a current manifest that does not decode, and an answers path the reader fails on
// without a code each print one refusal line, exit 1, for check and for export, with nothing
// changed under the root (exposes output after a refusal). Control: the valid answers export.
func TestRefusalsWriteNothing(t *testing.T) {
	invalid := cfgAbs("tests/fixtures/answers/invalid/unknown-field.yaml")
	dupName := strings.Replace(string(cfgRead(t, twoTenantAnswers)), `"name": "lab"`, `"name": "demo"`, 1)
	cases := []struct {
		name, answers, code string
		setup               func(t *testing.T, root string)
	}{
		{"invalid answers", invalid, configure.CodeUnknownField, nil},
		{"generated refusal", "dup.yaml", "DUPLICATE_NAME", func(t *testing.T, root string) {
			cfgWrite(t, filepath.Join(root, "dup.yaml"), []byte(dupName), 0o600)
		}},
		{"missing answers", "absent.yaml", configure.CodeAnswersNotRegular, nil},
		{"current manifest invalid", sandboxAnswers, configure.CodeCurrentManifestInvalid, func(t *testing.T, root string) {
			cfgWrite(t, filepath.Join(root, "stacks/deployments.yaml"), []byte("{\n"), 0o644)
		}},
		// a NUL in the path fails the open with EINVAL: an I/O error without a catalogue code
		{"uncoded read error", "bad\x00name.yaml", "error", nil},
	}
	for _, c := range cases {
		for _, verb := range []string{"check", "export"} {
			t.Run(c.name+"/"+verb, func(t *testing.T) {
				root := cfgRoot(t)
				if c.setup != nil {
					c.setup(t, root)
				}
				before := cfgInventory(t, root)
				code, lines := cfgRun(t, osStaging, "-root", root, verb, c.answers)
				cfgRefusal(t, code, lines, c.code)
				if changed := cfgChanged(before, cfgInventory(t, root)); len(changed) > 0 {
					t.Errorf("changed %v after a refusal", changed)
				}
			})
		}
	}
	root := cfgRoot(t)
	if code, lines := cfgRun(t, osStaging, "-root", root, "export", twoTenantAnswers); code != 0 {
		t.Errorf("control: exit %d, %q", code, lines)
	}
}

// TestUsage (pin): a missing argument, an unknown verb, an extra argument or an unknown flag
// prints the usage line, and only it, and exits 2.
func TestUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"check"}, {"export"}, {"apply", "a.yaml"}, {"check", "a.yaml", "b.yaml"}, {"-x", "check", "a.yaml"}} {
		code, lines := cfgRun(t, osStaging, args...)
		if code != 2 || !reflect.DeepEqual(lines, []string{usageLine}) {
			t.Errorf("%q: exit %d, %q; want 2 and the usage line", args, code, lines)
		}
	}
}

// cfgForbidden are the imports and os functions that would give the command a path to the
// network, a process or the environment (FR-008).
var (
	cfgForbiddenImports = []string{"net", "os/exec", "os/user", "syscall", "golang.org/x/sys/unix"}
	cfgForbiddenCalls   = []string{"Getenv", "LookupEnv", "Environ", "UserHomeDir", "ExpandEnv",
		"Setenv", "Unsetenv", "Clearenv", "StartProcess"}
)

// cfgForbiddenUses parses the non-test Go files of dir and reports each forbidden import (or one
// under it) and each use of a forbidden os function, under any import name.
func cfgForbiddenUses(dir string) (files int, uses []string, err error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return 0, nil, err
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return files, uses, err
		}
		files++
		osNames, importUses := cfgImportUses(path, f)
		uses = append(uses, importUses...)
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && osNames[id.Name] && slices.Contains(cfgForbiddenCalls, sel.Sel.Name) {
					uses = append(uses, path+": uses os."+sel.Sel.Name)
				}
			}
			return true
		})
	}
	return files, uses, nil
}

// cfgImportUses reports the forbidden imports of one file and the names it imports os under.
func cfgImportUses(path string, f *ast.File) (osNames map[string]bool, uses []string) {
	osNames = map[string]bool{}
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if slices.ContainsFunc(cfgForbiddenImports, func(x string) bool { return p == x || strings.HasPrefix(p, x+"/") }) {
			uses = append(uses, path+": imports "+p)
		}
		if p != "os" {
			continue
		}
		name := "os"
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if name == "." {
			uses = append(uses, path+": dot-imports os")
		}
		osNames[name] = true
	}
	return osNames, uses
}

// TestSourceHasNoAuthority (pin): the command imports nothing under net, no os/exec, os/user or
// syscall, and uses no os function that reads or changes the environment or starts a process
// (FR-008, SC-003); controls: a planted net/http import and a planted os.Getenv under another
// import name in a scratch copy are each found.
func TestSourceHasNoAuthority(t *testing.T) {
	files, uses, err := cfgForbiddenUses(".")
	if err != nil || files == 0 || len(uses) > 0 {
		t.Errorf("command source: %d files read, %v, uses %v; want files read and no use", files, err, uses)
	}
	scratch := t.TempDir()
	for name, src := range map[string]string{
		"net.go": "package main\n\nimport _ \"net/http\"\n",
		"env.go": "package main\n\nimport sys \"os\"\n\nfunc home() string { return sys.Getenv(\"HOME\") }\n",
	} {
		cfgWrite(t, filepath.Join(scratch, name), []byte(src), 0o600)
	}
	if _, uses, err := cfgForbiddenUses(scratch); err != nil || len(uses) != 2 {
		t.Errorf("control: planted net/http import and os.Getenv: %v, uses %v; want both found", err, uses)
	}
}
