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

// WriteCredentialFile writes values as sorted KEY=value lines to <root>/<rel>, mode 0600,
// creating missing directories below root with mode 0700. rel must stay below root without
// passing through a symlink; a symlink at the target itself is replaced, never followed. The file
// is written to a temporary name and renamed, so a reader never sees it partly written.
func WriteCredentialFile(root, rel string, values map[string]string) error {
	var b strings.Builder
	keys := make([]string, 0, len(values))
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
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, values[k])
	}

	clean := filepath.Clean(rel)
	if rel == "" || filepath.IsAbs(rel) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("credential path %q is not below %s", rel, root)
	}
	parts := strings.Split(clean, string(filepath.Separator))
	dir := root
	if err := privateDir(root); err != nil {
		return err
	}
	for _, p := range parts[:len(parts)-1] {
		dir = filepath.Join(dir, p)
		fi, err := os.Lstat(dir)
		switch {
		case os.IsNotExist(err):
			if err := os.Mkdir(dir, 0o700); err != nil {
				return err
			}
			// Mkdir applies the umask, which can only remove bits; Chmod pins the mode.
			if err := os.Chmod(dir, 0o700); err != nil {
				return err
			}
		case err != nil:
			return err
		case !fi.IsDir():
			return fmt.Errorf("%s is not a directory (symlinks are not followed)", dir)
		default:
			if err := privateDir(dir); err != nil {
				return err
			}
		}
	}
	target := filepath.Join(dir, parts[len(parts)-1])
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

// scratchHome creates a private per-run HOME for the children.
func scratchHome() (string, error) {
	d, err := os.MkdirTemp("", "lz-live-home-")
	if err != nil {
		return "", err
	}
	return d, os.Chmod(d, 0o700)
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
