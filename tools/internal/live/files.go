package live

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
)

// Credential files under ~/.config/ovh-lz/ (FR-010, data-model *Account binding and local
// files*). This file is the lane's only writer of files and directories (TestFilesOnlyWriter):
// files are written 0600, directories it creates 0700, and a credential file another user could
// read is refused.

// CondFileMode names the refusal of a credential file that is not a private regular file.
const CondFileMode = "file-mode"

var (
	envKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	// accountID is one plain path segment: GET /auth/details account ids look like ab12345-ovh.
	accountID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
)

// ReadEnvFile parses KEY=value lines (live.env, account.env); blank lines and # comments are
// skipped, any other line without '=' is an error.
func ReadEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseEnv(f, path)
}

func parseEnv(r io.Reader, path string) (map[string]string, error) {
	out := map[string]string{}
	s := bufio.NewScanner(r)
	for n := 1; s.Scan(); n++ {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || !envKey.MatchString(k) {
			// The line itself may hold a secret: name only its number.
			return nil, fmt.Errorf("%s:%d: not KEY=value", path, n)
		}
		out[k] = v
	}
	if err := s.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return out, nil
}

// ReadCredentialFile reads a credential file, refusing (exit 3) one that is not a regular file,
// grants any permission to group or others, or belongs to another user. The checks and the read
// use one open descriptor (no swap in between); O_NONBLOCK keeps a FIFO from blocking the open.
func ReadCredentialFile(path string) (map[string]string, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, refuse(CondFileMode, "%s is not a regular file", path)
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		return nil, refuse(CondFileMode, "%s has mode %04o, want 0600", path, perm)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != os.Getuid() {
		return nil, refuse(CondFileMode, "%s is not owned by this user", path)
	}
	return parseEnv(f, path)
}

// AccountDir returns <root>/accounts/<account>, refusing an account id that is not one plain
// path segment (it comes from the API).
func AccountDir(root, account string) (string, error) {
	if !accountID.MatchString(account) {
		return "", fmt.Errorf("account id %q is not a plain path segment", account)
	}
	return filepath.Join(root, "accounts", account), nil
}

// privateDir refuses a directory that grants any permission to group or others (directories
// 0700): it is reported, never silently tightened, so the owner sees what was open.
func privateDir(dir string) error {
	fi, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf("%s has mode %04o, want 0700", dir, perm)
	}
	return nil
}

// credentialDir checks that rel stays below root and creates its missing directories with mode
// 0700, never through a symlink; an existing directory others can use is refused. It returns the
// directory and the file name.
func credentialDir(root, rel string) (string, string, error) {
	clean := filepath.Clean(rel)
	if rel == "" || filepath.IsAbs(rel) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("credential path %q is not below %s", rel, root)
	}
	parts := strings.Split(clean, string(filepath.Separator))
	dir := root
	if err := privateDir(root); err != nil {
		return "", "", err
	}
	for _, p := range parts[:len(parts)-1] {
		dir = filepath.Join(dir, p)
		fi, err := os.Lstat(dir)
		switch {
		case os.IsNotExist(err):
			if err := os.Mkdir(dir, 0o700); err != nil {
				return "", "", err
			}
			// Mkdir applies the umask, which can only remove bits; Chmod pins the mode.
			if err := os.Chmod(dir, 0o700); err != nil {
				return "", "", err
			}
		case err != nil:
			return "", "", err
		case !fi.IsDir():
			return "", "", fmt.Errorf("%s is not a directory (symlinks are not followed)", dir)
		default:
			if err := privateDir(dir); err != nil {
				return "", "", err
			}
		}
	}
	return dir, parts[len(parts)-1], nil
}

// checkCredentialValues is what WriteCredentialFile requires of values: variable-name keys, values
// without a line break, NUL or surrounding whitespace (a read would trim it). A caller writing
// several files checks them all first, so none is replaced when a later one would be refused.
func checkCredentialValues(values map[string]string) error {
	for k, v := range values {
		if !envKey.MatchString(k) {
			return fmt.Errorf("credential key %q is not a variable name", k)
		}
		if strings.ContainsAny(v, "\n\r\x00") {
			return fmt.Errorf("credential %s: value holds a line break or NUL", k)
		}
		if strings.TrimSpace(v) != v {
			return fmt.Errorf("credential %s: value has surrounding whitespace, which a read trims", k)
		}
	}
	return nil
}

// WriteCredentialFile writes values as sorted KEY=value lines to <root>/<rel>, mode 0600,
// creating missing directories below root with mode 0700. rel must stay below root without
// passing through a symlink; a symlink at the target itself is replaced, never followed. The file
// is written to a temporary name and renamed, so a reader never sees it partly written.
func WriteCredentialFile(root, rel string, values map[string]string) error {
	if err := checkCredentialValues(values); err != nil {
		return err
	}
	var b strings.Builder
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, values[k])
	}

	dir, name, err := credentialDir(root, rel)
	if err != nil {
		return err
	}
	target := filepath.Join(dir, name)
	if fi, err := os.Lstat(target); err == nil && fi.IsDir() {
		return fmt.Errorf("%s is a directory", target)
	}

	tmp, err := os.CreateTemp(dir, ".write-*")
	if err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			os.Remove(tmp.Name())
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(b.String()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return err
	}
	done = true
	return nil
}

// Run record (.local/live/<run-id>/) and scratch files of the run core: directories 0700, files
// 0600, like the credential files above.

// ensureDir creates dir and missing parents with mode 0700.
func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}

// appendLine appends line and a newline to path (created 0600) and syncs it before returning.
func appendLine(path string, line []byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// writeRecord writes a run-record file (0600), replacing it.
func writeRecord(path string, data []byte) error {
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// scratchHome creates a private per-run HOME for the children, under the resolved TMPDIR (the
// path ScratchOutside checked, never a relative one).
func scratchHome() (string, error) {
	base, err := scratchBase()
	if err != nil {
		return "", err
	}
	d, err := os.MkdirTemp(base, "lz-live-home-")
	if err != nil {
		return "", err
	}
	return d, os.Chmod(d, 0o700)
}

// CondScratch names the refusal of a TMPDIR inside the checkout: the children's scratch HOME and
// their tofu data directories (<HOME>/tofu-data/<stack>) go under it (T077/T078).
const CondScratch = "scratch"

// ScratchOutside refuses (CondScratch) a TMPDIR that resolves to or inside one of dirs, symlinks
// resolved on both sides. Call it before any credential is read, file written or child started.
func ScratchOutside(dirs ...string) error {
	tmp := os.TempDir()
	real, err := scratchBase()
	if err != nil {
		return refuse(CondScratch, "TMPDIR %s does not resolve: %v", tmp, err)
	}
	for _, d := range dirs {
		rel, err := filepath.Rel(resolveExisting(d), real)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return refuse(CondScratch, "TMPDIR %s lies inside %s; point TMPDIR outside the checkout", tmp, d)
		}
	}
	return nil
}

// scratchBase is TMPDIR as the kernel resolves it: made absolute against the working directory
// without cleaning it first, then resolved component by component, so ".." after a symlink climbs
// from the link's target (review r1).
func scratchBase() (string, error) {
	tmp, err := absUncleaned(os.TempDir())
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(tmp)
}

// absUncleaned makes p absolute against the working directory without cleaning it: filepath.Abs
// would drop "x/.." before x's symlink is followed.
func absUncleaned(p string) (string, error) {
	if filepath.IsAbs(p) {
		return p, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return wd + string(filepath.Separator) + p, nil
}

// resolveExisting is p as the kernel resolves it, for a root that may not exist yet (the run
// records' directory): its longest existing prefix resolved component by component (symlinks,
// then ".."; review r2), the missing rest appended.
func resolveExisting(p string) string {
	if abs, err := absUncleaned(p); err == nil {
		p = abs
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	trimmed := strings.TrimRight(p, string(filepath.Separator))
	i := strings.LastIndex(trimmed, string(filepath.Separator))
	if i <= 0 {
		return filepath.Clean(p)
	}
	return filepath.Join(resolveExisting(trimmed[:i]), trimmed[i+1:])
}

// removeTree removes a scratch directory.
func removeTree(dir string) error { return os.RemoveAll(dir) }

// removeEmptyDir removes dir if it is empty; a missing directory is not an error.
func removeEmptyDir(dir string) error {
	if err := os.Remove(dir); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// removeFiles removes the named files; a missing file is not an error.
func removeFiles(paths ...string) error {
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// MoveCredentialFile moves the credential file <root>/<from> to <root>/<to> (a previous
// account's sandbox.env into its account directory, research R13), creating the target's missing
// directories 0700. An existing target is never replaced: it may be that account's own file.
func MoveCredentialFile(root, from, to string) error {
	if _, _, err := credentialDir(root, from); err != nil {
		return err
	}
	dir, name, err := credentialDir(root, to)
	if err != nil {
		return err
	}
	dst := filepath.Join(dir, name)
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		return fmt.Errorf("%s exists or cannot be examined; not replaced", dst)
	}
	return os.Rename(filepath.Join(root, from), dst)
}
