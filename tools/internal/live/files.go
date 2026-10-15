package live

import (
	"bufio"
	"errors"
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

// credentialLines is values as sorted KEY=value lines.
func credentialLines(values map[string]string) string {
	var b strings.Builder
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, values[k])
	}
	return b.String()
}

// WriteCredentialFile writes values as sorted KEY=value lines to <root>/<rel>, mode 0600,
// creating missing directories below root with mode 0700. rel must stay below root without
// passing through a symlink; a symlink at the target itself is replaced, never followed. The file
// is written to a temporary name and renamed, so a reader never sees it partly written.
func WriteCredentialFile(root, rel string, values map[string]string) error {
	if err := checkCredentialValues(values); err != nil {
		return err
	}
	content := credentialLines(values)

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
	if _, err := tmp.WriteString(content); err != nil {
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

// SetStep names the steps of a credential-set write (FR-005, data-model *Credential set*), in
// write order: the generation's first file, all its files, the generation directory's fsync,
// `current.tmp`, the switch (the rename over `current`), the removal of older generations.
type SetStep int

const (
	SetStepFirstFile SetStep = iota + 1
	SetStepFiles
	SetStepDirSync
	SetStepPointerTmp
	SetStepSwitch
	SetStepCleanup
)

// CredentialSet is one writing instance's set of credential files, written below
// <Root>/accounts/<Account>/sets/<Instance>/ as generation gen-<RunID>. Files maps each file's
// relative path (the one readers use today) to its variables.
type CredentialSet struct {
	Root, Account, Instance, RunID string
	Files                          map[string]map[string]string
}

// ErrInjectedCrash is what WriteCredentialSet returns right after the step a test named, leaving
// the disk as a crash there would.
var ErrInjectedCrash = errors.New("credential set: injected crash")

// genName is a generation directory's name (data-model *Credential set*): gen-<run_id>.
var genName = regexp.MustCompile(`^gen-[0-9A-Za-z-]+$`)

// The files a credential set may hold, at the canonical paths readers use (data-model *Credential
// set*): account-governance's set holds the deployer clients, every other writing instance is a
// tenant-state and holds S3 keys. The binding, the passphrase, sandbox.env, the bootstrap's
// state.env and the other kind's files never match, so step 5 never removes them as flat files.
const setInstanceGovernance = "account-governance"

var (
	setFilesGovernance = regexp.MustCompile(`^(platform-deployer\.env|tenants/[A-Za-z0-9][A-Za-z0-9_-]*/deployer\.env)$`)
	setFilesState      = regexp.MustCompile(`^tenants/[A-Za-z0-9][A-Za-z0-9_-]*/(state|platform-state)\.env$`)
)

// setSync and setRename are the set writer's fsync and its switch. They are variables only so a
// test can see what crash injection cannot: every file and directory of the generation synced
// before the switch, and the switch one rename over an existing `current`.
var (
	setSync   = func(f *os.File) error { return f.Sync() }
	setRename = os.Rename
)

// WriteCredentialSet writes s as a fresh generation and makes it current through one rename of
// the pointer file `current`. failAfter is 0 in production; a non-zero step makes it return
// ErrInjectedCrash right after that step.
//
// Steps (data-model *Credential set*): 1 create gen-<RunID>/, write and fsync its files; 2 fsync
// its directories and the directories that gained an entry on the way; 3 write and fsync
// current.tmp; 4 rename it over `current` (the switch) and fsync the set directory; 5 remove every
// generation except the new and the previous one, a stale current.tmp, and the flat files the set
// replaces. Nothing a reader follows changes before step 4, and step 4 is one rename(2): a crash
// before it leaves the previous set current, after it the new one.
func WriteCredentialSet(s CredentialSet, failAfter SetStep) error {
	acct, err := AccountDir(s.Root, s.Account)
	if err != nil {
		return err
	}
	if !accountID.MatchString(s.Instance) {
		return fmt.Errorf("credential set: instance %q is not a plain path segment", s.Instance)
	}
	gen := "gen-" + s.RunID
	if !genName.MatchString(gen) {
		return fmt.Errorf("credential set: run id %q does not make a generation name", s.RunID)
	}
	if len(s.Files) == 0 {
		return errors.New("credential set: no files")
	}
	// Every file is checked before anything is written: a refused path or value leaves no trace.
	setFile := setFilesState
	if s.Instance == setInstanceGovernance {
		setFile = setFilesGovernance
	}
	rels := make([]string, 0, len(s.Files))
	for rel, values := range s.Files {
		if !setFile.MatchString(rel) {
			return fmt.Errorf("credential set: %q is not a file of %s's set at its canonical path", rel, s.Instance)
		}
		if err := checkCredentialValues(values); err != nil {
			return err
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	setRel := filepath.Join("accounts", s.Account, "sets", s.Instance)
	setDir := filepath.Join(s.Root, setRel)
	// credentialDir creates sets/<instance>/ 0700 and refuses a symlink or an open directory on
	// the way.
	if _, _, err := credentialDir(s.Root, filepath.Join(setRel, "current")); err != nil {
		return err
	}
	prev, err := currentGeneration(setDir)
	if err != nil {
		return err
	}
	if prev == gen {
		return fmt.Errorf("credential set: %s is current; a run's generation is never rewritten", gen)
	}

	// Step 1. A directory of this name is a crashed earlier write of the same run, never current.
	genDir := filepath.Join(setDir, gen)
	if err := os.RemoveAll(genDir); err != nil {
		return err
	}
	if err := os.Mkdir(genDir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(genDir, 0o700); err != nil {
		return err
	}
	dirs := map[string]bool{genDir: true}
	for i, rel := range rels {
		dir, name, err := credentialDir(s.Root, filepath.Join(setRel, gen, rel))
		if err != nil {
			return err
		}
		for d := dir; strings.HasPrefix(d, genDir+string(filepath.Separator)); d = filepath.Dir(d) {
			dirs[d] = true
		}
		if err := writeSynced(filepath.Join(dir, name), credentialLines(s.Files[rel])); err != nil {
			return err
		}
		if i == 0 && failAfter == SetStepFirstFile {
			return ErrInjectedCrash
		}
	}
	if failAfter == SetStepFiles {
		return ErrInjectedCrash
	}

	// Step 2: the generation's directories deepest first, then the set directory and its parents
	// up to the root, any of which may have gained an entry.
	order := make([]string, 0, len(dirs)+3)
	for d := range dirs {
		order = append(order, d)
	}
	sort.Slice(order, func(i, j int) bool { return len(order[i]) > len(order[j]) })
	order = append(order, setDir, filepath.Dir(setDir), acct, filepath.Dir(acct), s.Root)
	for _, d := range order {
		if err := syncDir(d); err != nil {
			return err
		}
	}
	if failAfter == SetStepDirSync {
		return ErrInjectedCrash
	}

	// Step 3.
	tmp, cur := filepath.Join(setDir, "current.tmp"), filepath.Join(setDir, "current")
	if err := removeFiles(tmp); err != nil {
		return err
	}
	if err := writeSynced(tmp, gen); err != nil {
		return err
	}
	if failAfter == SetStepPointerTmp {
		return ErrInjectedCrash
	}

	// Step 4, the switch: one rename over `current`, never an unlink first (that would leave a
	// moment with no pointer, when readers fall back to the flat files). The set directory is
	// synced before step 5 removes anything: on a first write, a removal of the flat files that
	// outlived a lost rename would leave readers with no set at all.
	if err := setRename(tmp, cur); err != nil {
		return err
	}
	if err := syncDir(setDir); err != nil {
		return err
	}
	if failAfter == SetStepSwitch {
		return ErrInjectedCrash
	}

	// Step 5. Its leftovers are removed by the next writer.
	des, err := os.ReadDir(setDir)
	if err != nil {
		return err
	}
	for _, de := range des {
		n := de.Name()
		if n == "current" || n == gen || n == prev || n != "current.tmp" && !genName.MatchString(n) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(setDir, n)); err != nil {
			return err
		}
	}
	for _, rel := range rels {
		if err := removeFlat(acct, rel); err != nil {
			return err
		}
	}
	if err := syncDir(setDir); err != nil {
		return err
	}
	if failAfter == SetStepCleanup {
		return ErrInjectedCrash
	}
	return nil
}

// currentGeneration is the generation `current` names in setDir, "" while `current` does not
// exist. A pointer that is not a regular file whose whole content names a sibling gen- directory
// is refused: the writer could not tell which generation is the previous one to keep.
func currentGeneration(setDir string) (string, error) {
	cur := filepath.Join(setDir, "current")
	fi, err := os.Lstat(cur)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("CREDENTIAL_POINTER_INVALID: %s is not a regular file", cur)
	}
	b, err := os.ReadFile(cur)
	if err != nil {
		return "", err
	}
	gen := string(b)
	if !genName.MatchString(gen) {
		return "", fmt.Errorf("CREDENTIAL_POINTER_INVALID: %s does not hold a generation name", cur)
	}
	if gi, err := os.Lstat(filepath.Join(setDir, gen)); err != nil || !gi.IsDir() {
		return "", fmt.Errorf("CREDENTIAL_POINTER_INVALID: %s names no generation directory beside it", cur)
	}
	return gen, nil
}

// writeSynced creates path (which must not exist), mode 0600, writes content and fsyncs it.
func writeSynced(path, content string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return err
	}
	if err := setSync(f); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// syncDir fsyncs a directory, making the entries created, renamed or removed in it durable.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := setSync(d); err != nil {
		d.Close()
		return err
	}
	return d.Close()
}

// removeFlat removes the flat (005) file acct/rel that a set replaces, after the switch. A missing
// file or directory is not an error; a directory on the way that is a symlink is refused, never
// followed, and a directory at the path itself is left alone.
func removeFlat(acct, rel string) error {
	dir := acct
	if parent := filepath.Dir(rel); parent != "." {
		for _, p := range strings.Split(parent, string(filepath.Separator)) {
			dir = filepath.Join(dir, p)
			fi, err := os.Lstat(dir)
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if !fi.IsDir() {
				return fmt.Errorf("%s is not a directory (symlinks are not followed)", dir)
			}
		}
	}
	p := filepath.Join(dir, filepath.Base(rel))
	fi, err := os.Lstat(p)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return nil
	}
	return removeFiles(p)
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
