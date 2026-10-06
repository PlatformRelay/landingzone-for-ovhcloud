package live

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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
	old := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(old) })
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
