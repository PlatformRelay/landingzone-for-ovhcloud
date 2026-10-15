package live

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Credential files (FR-010, data-model *Account binding and local files*): one writer, files
// 0600, directories 0700, under the config root only; the lane refuses group/world-readable
// files.

// withUmask runs the test under a permissive umask, so a mode that only looks right because the
// host's umask strips group/other bits still fails.
func withUmask(t *testing.T) {
	t.Helper()
	notParallel(t)
	old := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(old) })
}

// notParallel marks t as changing process-wide state (the umask, os.Stdout, the standard logger):
// t.Setenv panics in a test that called t.Parallel, so such a test never runs beside another (T114).
func notParallel(t testing.TB) {
	t.Helper()
	t.Setenv("LZ_TEST_PROCESS_WIDE", "1")
}

// tempPrivate is a 0700 temporary directory (t.TempDir follows the host umask: 0775 here).
func tempPrivate(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if err := os.Chmod(d, 0o700); err != nil {
		t.Fatal(err)
	}
	return d
}

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode()
}

// TestFilesMoveCredential (T043, R13 migration): a previous account's sandbox.env moves into its
// account directory, created 0700; an existing target is never replaced and a path leaving the
// root is refused, both with the source left in place.
func TestFilesMoveCredential(t *testing.T) {
	withUmask(t)
	root := tempPrivate(t)
	if err := WriteCredentialFile(root, "sandbox.env", map[string]string{"OVH_CLIENT_ID": "a"}); err != nil {
		t.Fatal(err)
	}
	if err := MoveCredentialFile(root, "sandbox.env", filepath.Join("accounts", "xx1-ovh", "sandbox.env")); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"accounts", filepath.Join("accounts", "xx1-ovh")} {
		if m := mode(t, filepath.Join(root, d)); m.Perm() != 0o700 || !m.IsDir() {
			t.Errorf("%s mode %v, want a 0700 directory", d, m)
		}
	}
	if v, err := ReadCredentialFile(filepath.Join(root, "accounts", "xx1-ovh", "sandbox.env")); err != nil || v["OVH_CLIENT_ID"] != "a" {
		t.Errorf("moved file %v, %v", v, err)
	}
	if _, err := os.Lstat(filepath.Join(root, "sandbox.env")); !os.IsNotExist(err) {
		t.Error("source still present after the move")
	}
	if err := WriteCredentialFile(root, "sandbox.env", map[string]string{"OVH_CLIENT_ID": "b"}); err != nil {
		t.Fatal(err)
	}
	for _, to := range []string{filepath.Join("accounts", "xx1-ovh", "sandbox.env"), filepath.Join("..", "escaped.env"), "/abs.env"} {
		if err := MoveCredentialFile(root, "sandbox.env", to); err == nil {
			t.Errorf("move to %s: no error", to)
		}
	}
	if v, err := ReadCredentialFile(filepath.Join(root, "accounts", "xx1-ovh", "sandbox.env")); err != nil || v["OVH_CLIENT_ID"] != "a" {
		t.Errorf("existing target replaced: %v, %v", v, err)
	}
	if v, err := ReadCredentialFile(filepath.Join(root, "sandbox.env")); err != nil || v["OVH_CLIENT_ID"] != "b" {
		t.Errorf("source lost by a refused move: %v, %v", v, err)
	}
}

func TestFilesWriteCredential(t *testing.T) {
	withUmask(t)
	root := tempPrivate(t)
	values := map[string]string{"OVH_CLIENT_ID": "id-1", "OVH_CLIENT_SECRET": "s3cr=et", "TF_VAR_state_passphrase": "p w"}
	rel := filepath.Join("accounts", "ab12345-ovh", "tenants", "t1", "deployer.env")
	if err := WriteCredentialFile(root, rel, values); err != nil {
		t.Fatalf("WriteCredentialFile: %v", err)
	}
	path := filepath.Join(root, rel)
	if m := mode(t, path); !m.IsRegular() || m.Perm() != 0o600 {
		t.Fatalf("file mode %v, want regular 0600", m)
	}
	for _, d := range []string{"accounts", "accounts/ab12345-ovh", "accounts/ab12345-ovh/tenants", "accounts/ab12345-ovh/tenants/t1"} {
		if m := mode(t, filepath.Join(root, d)); !m.IsDir() || m.Perm() != 0o700 {
			t.Errorf("directory %s mode %v, want 0700", d, m)
		}
	}
	got, err := ReadCredentialFile(path)
	if err != nil {
		t.Fatalf("ReadCredentialFile: %v", err)
	}
	if len(got) != len(values) {
		t.Fatalf("read back %d values, want %d", len(got), len(values))
	}
	for k, v := range values {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	// No temporary file is left next to it.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only the credential file", len(entries))
	}
}

func TestFilesWriteReplacesOpenFile(t *testing.T) {
	withUmask(t)
	root := tempPrivate(t)
	path := filepath.Join(root, "state.env")
	if err := os.WriteFile(path, []byte("OLD=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteCredentialFile(root, "state.env", map[string]string{"NEW": "2"}); err != nil {
		t.Fatalf("WriteCredentialFile: %v", err)
	}
	if m := mode(t, path); m.Perm() != 0o600 {
		t.Fatalf("mode %v after rewrite, want 0600", m)
	}
	got, err := ReadCredentialFile(path)
	if err != nil || got["NEW"] != "2" || got["OLD"] != "" {
		t.Fatalf("read back %v, %v; want only NEW=2", got, err)
	}
}

func TestFilesWriteStaysUnderRoot(t *testing.T) {
	withUmask(t)
	// Private base: an escape must be refused by the path check, not by the base's mode.
	base := tempPrivate(t)
	root := filepath.Join(base, "ovh-lz")
	outside := filepath.Join(base, "outside")
	for _, d := range []string{root, outside} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	victim := filepath.Join(outside, "victim.env")
	writeFile(t, victim, "KEEP=1\n")
	if err := os.Symlink(outside, filepath.Join(root, "accounts")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(root, "linked.env")); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"", ".", "../outside/x.env", "a/../../outside/x.env", victim, // escapes or no file name
		"accounts/victim.env", // through a symlinked directory
	} {
		if err := WriteCredentialFile(root, rel, map[string]string{"K": "v"}); err == nil {
			t.Errorf("WriteCredentialFile(%q) succeeded, want refused", rel)
		}
	}
	// A symlink at the target is replaced, never followed.
	if err := WriteCredentialFile(root, "linked.env", map[string]string{"K": "v"}); err != nil {
		t.Fatalf("WriteCredentialFile(linked.env): %v", err)
	}
	if m := mode(t, filepath.Join(root, "linked.env")); !m.IsRegular() || m.Perm() != 0o600 {
		t.Errorf("linked.env mode %v, want a regular 0600 file", m)
	}
	if raw, _ := os.ReadFile(victim); string(raw) != "KEEP=1\n" {
		t.Errorf("file outside the root changed: %q", raw)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 1 {
		t.Errorf("outside directory holds %d entries, want 1", len(entries))
	}
}

func TestFilesWriteRefusesMalformedValues(t *testing.T) {
	t.Parallel()
	root := tempPrivate(t)
	for name, values := range map[string]map[string]string{
		"newline in value": {"K": "a\nINJECTED=1"},
		"cr in value":      {"K": "a\rb"},
		"nul in value":     {"K": "a\x00b"},
		"= in key":         {"A=B": "v"},
		"space in key":     {"A B": "v"},
		"empty key":        {"": "v"},
		"digit first":      {"1A": "v"},
		// ReadEnvFile trims each line, so these would not read back as written.
		"leading space":  {"K": " v"},
		"trailing space": {"K": "v "},
		"trailing tab":   {"K": "v\t"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := WriteCredentialFile(root, "x.env", values); err == nil {
				t.Fatal("written, want refused")
			}
			if _, err := os.Lstat(filepath.Join(root, "x.env")); !os.IsNotExist(err) {
				t.Fatalf("x.env exists after a refused write (%v)", err)
			}
		})
	}
}

func TestFilesReadRefusesOpenModes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, m := range []os.FileMode{0o640, 0o604, 0o620, 0o602, 0o660, 0o644} {
		path := filepath.Join(dir, "open.env")
		writeFile(t, path, "K=v\n")
		if err := os.Chmod(path, m); err != nil {
			t.Fatal(err)
		}
		_, err := ReadCredentialFile(path)
		assertRefused(t, err, []string{CondFileMode})
	}
	for _, m := range []os.FileMode{0o600, 0o400} {
		path := filepath.Join(dir, "closed.env")
		writeFile(t, path, "K=v\n")
		if err := os.Chmod(path, m); err != nil {
			t.Fatal(err)
		}
		if got, err := ReadCredentialFile(path); err != nil || got["K"] != "v" {
			t.Errorf("mode %v: %v, %v; want K=v", m, got, err)
		}
		os.Chmod(path, 0o600)
	}
	// A private directory: only the regular-file clause can refuse it.
	sub := filepath.Join(dir, "private-dir")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := ReadCredentialFile(sub)
	assertRefused(t, err, []string{CondFileMode})
	// A FIFO is refused, not waited on (the open must not block).
	fifo := filepath.Join(dir, "fifo.env")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := ReadCredentialFile(fifo); done <- err }()
	select {
	case err := <-done:
		assertRefused(t, err, []string{CondFileMode})
	case <-time.After(5 * time.Second):
		t.Fatal("ReadCredentialFile blocked on a FIFO")
	}
	if _, err := ReadCredentialFile(filepath.Join(dir, "missing.env")); err == nil {
		t.Error("a missing credential file read without error")
	}
	bad := filepath.Join(dir, "bad.env")
	writeFile(t, bad, "no equals sign\n")
	if _, err := ReadCredentialFile(bad); err == nil {
		t.Error("a malformed line read without error")
	}
}

func TestFilesAccountDir(t *testing.T) {
	t.Parallel()
	root := tempPrivate(t)
	got, err := AccountDir(root, "ab12345-ovh")
	if err != nil || got != filepath.Join(root, "accounts", "ab12345-ovh") {
		t.Fatalf("AccountDir = %q, %v", got, err)
	}
	for _, id := range []string{"", ".", "..", "a/b", "../x", "/abs", "a\\b", "a\nb", ".hidden"} {
		if got, err := AccountDir(root, id); err == nil {
			t.Errorf("AccountDir(%q) = %q, want refused", id, got)
		}
	}
}

// writers are the os functions that create, replace or re-permission a file or directory.
var writers = map[string]bool{
	"WriteFile": true, "Create": true, "CreateTemp": true, "OpenFile": true, "Mkdir": true,
	"MkdirAll": true, "MkdirTemp": true, "Rename": true, "Chmod": true, "Symlink": true, "Link": true,
	"Truncate": true, "Remove": true, "RemoveAll": true, "Chown": true, "Lchown": true, "Chtimes": true,
}

// TestFilesOnlyWriter: outside files.go, no non-test file of the live lane calls a writer
// (os.X, ioutil.WriteFile, or a method of the same name on an *os.File is not a writer).
func TestFilesOnlyWriter(t *testing.T) {
	t.Parallel()
	var offenders []string
	for _, dir := range []string{".", filepath.Join("..", "..", "cmd", "lz-live")} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		if len(files) == 0 {
			t.Fatalf("no Go files in %s", dir)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") || filepath.Base(f) == "files.go" && dir == "." {
				continue
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, f, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			pkgs := map[string]bool{}
			for _, imp := range file.Imports {
				p := strings.Trim(imp.Path.Value, `"`)
				if p != "os" && p != "io/ioutil" {
					continue
				}
				name := filepath.Base(p)
				if imp.Name != nil {
					name = imp.Name.Name
				}
				pkgs[name] = true
			}
			ast.Inspect(file, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if id, ok := sel.X.(*ast.Ident); ok && pkgs[id.Name] && writers[sel.Sel.Name] {
					offenders = append(offenders, fset.Position(sel.Pos()).String()+" "+id.Name+"."+sel.Sel.Name)
				}
				return true
			})
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("file writers outside files.go:\n%s", strings.Join(offenders, "\n"))
	}
}

// Credential set (FR-005, data-model *Credential set*; T007): one set per writing instance under
// accounts/<account>/sets/<instance>/, written as a fresh generation and switched by one rename
// of `current`; a crash before the switch leaves the previous set whole, after it the new one.

const (
	csAccount  = "xx1-ovh"
	csInstance = "account-governance"
	csRunID    = "20261015T101500Z-1a2b"
	csNextID   = "20261015T111500Z-3c4d"
)

// csRels are the account-governance set's files at the relative paths readers use today.
var csRels = []string{"platform-deployer.env", filepath.Join("tenants", "alpha", "deployer.env")}

// csName is the generation name data-model.md lets a reader accept.
var csName = regexp.MustCompile(`^gen-[0-9A-Za-z-]+$`)

// csFiles is a set whose every value carries tag, so a set read from two generations shows.
func csFiles(tag string) map[string]map[string]string {
	return map[string]map[string]string{
		csRels[0]: {"OVH_CLIENT_ID": tag + "-platform-id", "OVH_CLIENT_SECRET": tag + "-platform-secret"},
		csRels[1]: {"OVH_CLIENT_ID": tag + "-alpha-id", "OVH_CLIENT_SECRET": tag + "-alpha-secret"},
	}
}

// csSet is the account-governance set of run id under root.
func csSet(root, runID, tag string) CredentialSet {
	return CredentialSet{Root: root, Account: csAccount, Instance: csInstance, RunID: runID, Files: csFiles(tag)}
}

// csRoot is a private config root holding an empty account directory. It returns the root, the
// account directory and the instance's set directory (not created).
func csRoot(t *testing.T) (string, string, string) {
	t.Helper()
	root := tempPrivate(t)
	acct := filepath.Join(root, "accounts", csAccount)
	if err := os.MkdirAll(acct, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{filepath.Join(root, "accounts"), acct} {
		if err := os.Chmod(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return root, acct, filepath.Join(acct, "sets", csInstance)
}

// csSeed lays down generation gen by hand (files 0600 below 0700 directories, through
// WriteCredentialFile), not through the writer under test.
func csSeed(t *testing.T, root, gen, tag string) {
	t.Helper()
	for rel, values := range csFiles(tag) {
		if err := WriteCredentialFile(root, filepath.Join("accounts", csAccount, "sets", csInstance, gen, rel), values); err != nil {
			t.Fatal(err)
		}
	}
}

// csPoint makes `current` name gen, mode 0600, as a completed write leaves it.
func csPoint(t *testing.T, setDir, gen string) {
	t.Helper()
	cur := filepath.Join(setDir, "current")
	if err := os.WriteFile(cur, []byte(gen), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cur, 0o600); err != nil {
		t.Fatal(err)
	}
}

// csLoad is a fresh reader by data-model.md's rule: it resolves `current` once (a regular file
// whose whole content is a generation name naming a sibling directory) and reads every file of the
// set from that generation through ReadCredentialFile. It is the test's reader, not T014's.
func csLoad(setDir string) (string, map[string]map[string]string, error) {
	cur := filepath.Join(setDir, "current")
	fi, err := os.Lstat(cur)
	if err != nil {
		return "", nil, err
	}
	if !fi.Mode().IsRegular() {
		return "", nil, fmt.Errorf("current is %v, not a regular file", fi.Mode())
	}
	b, err := os.ReadFile(cur)
	if err != nil {
		return "", nil, err
	}
	gen := string(b)
	if !csName.MatchString(gen) {
		return gen, nil, fmt.Errorf("current holds %q, not a generation name", gen)
	}
	if fi, err := os.Lstat(filepath.Join(setDir, gen)); err != nil || !fi.IsDir() {
		return gen, nil, fmt.Errorf("current names %s, which is not a directory: %v", gen, err)
	}
	set, err := csRead(filepath.Join(setDir, gen))
	return gen, set, err
}

// csHolds reports whether the file at path holds exactly s.
func csHolds(path, s string) bool {
	b, err := os.ReadFile(path)
	return err == nil && string(b) == s
}

// csRead reads every file of the set from one generation directory.
func csRead(genDir string) (map[string]map[string]string, error) {
	set := map[string]map[string]string{}
	for _, rel := range csRels {
		v, err := ReadCredentialFile(filepath.Join(genDir, rel))
		if err != nil {
			return nil, err
		}
		set[rel] = v
	}
	return set, nil
}

// csKept fails t unless the kept generation gen still holds the set tagged tag whole: cleanup
// that empties or rewrites the previous generation breaks the manual rollback.
func csKept(t *testing.T, setDir, gen, tag, when string) {
	t.Helper()
	if set, err := csRead(filepath.Join(setDir, gen)); err != nil || !reflect.DeepEqual(set, csFiles(tag)) {
		t.Errorf("%s: the kept generation %s does not hold the %q set whole (%v)", when, gen, tag, err)
	}
}

// csWant fails t unless a fresh reader loads exactly gen with the set tagged tag.
func csWant(t *testing.T, setDir, gen, tag, when string) {
	t.Helper()
	got, set, err := csLoad(setDir)
	switch {
	case err != nil:
		t.Errorf("%s: a fresh reader cannot load a set: %v", when, err)
	case got != gen:
		t.Errorf("%s: current names %s, want %s", when, got, gen)
	case !reflect.DeepEqual(set, csFiles(tag)):
		t.Errorf("%s: the set read from %s is not the %q set whole", when, got, tag)
	}
}

// csEntries are the names in the set directory, sorted.
func csEntries(t *testing.T, setDir string) []string {
	t.Helper()
	des, err := os.ReadDir(setDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, de := range des {
		names = append(names, de.Name())
	}
	sort.Strings(names)
	return names
}

// csOnlySets fails t when anything in the account directory lies outside sets/: a set file
// written at its live (flat) path.
func csOnlySets(t *testing.T, acct, when string) {
	t.Helper()
	des, err := os.ReadDir(acct)
	if err != nil {
		t.Fatal(err)
	}
	for _, de := range des {
		if de.Name() != "sets" {
			t.Errorf("%s: account directory holds %s outside sets/", when, de.Name())
		}
	}
}

// TestFilesCredentialSetWriter (T007, FR-005): the writer's generation, pointer, crash windows and
// retention. The previous set of the crash cases is laid down by hand, so the cases judge the
// writer alone. A previous generation removed before the switch fails every crash case (the
// reader cannot load the previous set).
func TestFilesCredentialSetWriter(t *testing.T) {
	withUmask(t)

	// Given an empty account directory, when the account-governance set is written, then
	// sets/<instance>/gen-<run_id>/ holds every file at mode 600 and `current` names it.
	// Exposes a writer that skips the generation or the pointer.
	t.Run("fresh-write", func(t *testing.T) {
		root, acct, setDir := csRoot(t)
		if err := WriteCredentialSet(csSet(root, csRunID, "new"), 0); err != nil {
			t.Fatalf("WriteCredentialSet on an empty account directory: %v", err)
		}
		gen := "gen-" + csRunID
		csWant(t, setDir, gen, "new", "after the write")
		if got, want := csEntries(t, setDir), []string{"current", gen}; !reflect.DeepEqual(got, want) {
			t.Errorf("set directory holds %v, want %v", got, want)
		}
		for _, p := range []string{filepath.Join(acct, "sets"), setDir, filepath.Join(setDir, gen), filepath.Join(setDir, gen, "tenants"), filepath.Join(setDir, gen, "tenants", "alpha")} {
			if m := mode(t, p); !m.IsDir() || m.Perm() != 0o700 {
				t.Errorf("%s mode %v, want a 0700 directory", p, m)
			}
		}
		for _, p := range []string{filepath.Join(setDir, "current"), filepath.Join(setDir, gen, csRels[0]), filepath.Join(setDir, gen, csRels[1])} {
			if m := mode(t, p); !m.IsRegular() || m.Perm() != 0o600 {
				t.Errorf("%s mode %v, want a regular 0600 file", p, m)
			}
		}
		csOnlySets(t, acct, "after the write")
	})

	// Given a previous set, when the writer stops after the step, then a fresh reader loads the
	// previous set whole and the next writer removes the partial generation (and a stale
	// current.tmp). Exposes files written into live paths (first file), a switch before the
	// generation is durable (files, dir sync) and a pointer replaced in place (pointer tmp).
	const prev = "gen-20261014T090000Z-0aa0"
	for _, c := range []struct {
		name string
		step SetStep
	}{
		{"crash-after-first-file", SetStepFirstFile},
		{"crash-after-files", SetStepFiles},
		{"crash-after-dir-sync", SetStepDirSync},
		{"crash-after-pointer-tmp", SetStepPointerTmp},
	} {
		t.Run(c.name, func(t *testing.T) {
			root, acct, setDir := csRoot(t)
			csSeed(t, root, prev, "prev")
			csPoint(t, setDir, prev)
			if err := WriteCredentialSet(csSet(root, csRunID, "new"), c.step); !errors.Is(err, ErrInjectedCrash) {
				t.Errorf("WriteCredentialSet stopping after step %d returned %v, want ErrInjectedCrash", c.step, err)
			}
			csWant(t, setDir, prev, "prev", c.name)
			csOnlySets(t, acct, c.name)
			if c.step == SetStepPointerTmp {
				// data-model step 3: the new name waits in current.tmp, not in current.
				tmp := filepath.Join(setDir, "current.tmp")
				if b, err := os.ReadFile(tmp); err != nil || string(b) != "gen-"+csRunID {
					t.Errorf("%s: current.tmp holds %q (%v), want gen-%s", c.name, b, err, csRunID)
				} else if m := mode(t, tmp); !m.IsRegular() || m.Perm() != 0o600 {
					t.Errorf("%s: current.tmp mode %v, want a regular 0600 file", c.name, m)
				}
			}
			if err := WriteCredentialSet(csSet(root, csNextID, "next"), 0); err != nil {
				t.Fatalf("the next write: %v", err)
			}
			csWant(t, setDir, "gen-"+csNextID, "next", "after the next write")
			if got, want := csEntries(t, setDir), []string{"current", prev, "gen-" + csNextID}; !reflect.DeepEqual(got, want) {
				t.Errorf("after the next write the set directory holds %v, want %v (partial generation or current.tmp left)", got, want)
			}
			csKept(t, setDir, prev, "prev", "after the next write")
		})
	}

	// Given a previous set, when the writer stops after the switch, then a fresh reader gets the
	// new set whole. The previous generation's name sorts after the new one, so a reader that
	// picks by name rather than through `current` would get the old set. Exposes a reader that
	// still prefers the old set. The switch is a rename: `current` is a new file, not the old
	// one rewritten in place (a reader mid-read of an in-place rewrite can see a torn name).
	t.Run("crash-after-switch", func(t *testing.T) {
		root, acct, setDir := csRoot(t)
		const later = "gen-20261016T090000Z-0bb0"
		csSeed(t, root, later, "prev")
		csPoint(t, setDir, later)
		before, err := os.Lstat(filepath.Join(setDir, "current"))
		if err != nil {
			t.Fatal(err)
		}
		if err := WriteCredentialSet(csSet(root, csRunID, "new"), SetStepSwitch); !errors.Is(err, ErrInjectedCrash) {
			t.Errorf("WriteCredentialSet stopping after the switch returned %v, want ErrInjectedCrash", err)
		}
		csWant(t, setDir, "gen-"+csRunID, "new", "after a crash at the switch")
		cur := filepath.Join(setDir, "current")
		if after, err := os.Lstat(cur); err == nil && os.SameFile(before, after) && !csHolds(cur, later) {
			t.Error("after a crash at the switch: current is the old file rewritten in place, not replaced by a rename")
		}
		csKept(t, setDir, later, "prev", "after a crash at the switch")
		csOnlySets(t, acct, "after a crash at the switch")
	})

	// Given three earlier generations, when a write completes, then only the current and the
	// previous remain. The previous (the one `current` named, after a manual rollback) is the
	// oldest by name and by modification time, so a writer that keeps the newest instead fails.
	// Exposes unbounded growth or removal of the previous generation.
	t.Run("keeps-current-and-previous", func(t *testing.T) {
		root, _, setDir := csRoot(t)
		gens := []string{"gen-20261012T090000Z-0001", "gen-20261013T090000Z-0002", "gen-20261014T090000Z-0003"}
		tags := []string{"rollback", "old-2", "old-3"}
		base := time.Now().Add(-72 * time.Hour)
		for i, g := range gens {
			csSeed(t, root, g, tags[i])
			at := base.Add(time.Duration(i) * time.Hour)
			if err := os.Chtimes(filepath.Join(setDir, g), at, at); err != nil {
				t.Fatal(err)
			}
		}
		csPoint(t, setDir, gens[0])
		if err := WriteCredentialSet(csSet(root, csRunID, "new"), 0); err != nil {
			t.Fatalf("WriteCredentialSet: %v", err)
		}
		csWant(t, setDir, "gen-"+csRunID, "new", "after the write")
		if got, want := csEntries(t, setDir), []string{"current", gens[0], "gen-" + csRunID}; !reflect.DeepEqual(got, want) {
			t.Errorf("set directory holds %v, want %v", got, want)
		}
		csKept(t, setDir, gens[0], "rollback", "after the write")
	})
}
