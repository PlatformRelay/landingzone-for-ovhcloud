// lz-configure turns an answers file into a checked deployments manifest (004 FR-005 to FR-008).
//
//	lz-configure [-root <repo>] check <answers>
//	lz-configure [-root <repo>] export <answers>
//
// check (task configure:check) reads the answers file (relative to the root, or absolute; a
// regular file), decodes it strictly, derives the manifest and passes it through the manifest
// decoder, then prints the row diff against the root's stacks/deployments.yaml (every row `+`
// when that file is absent), the consequence notes and `configure: ok`. It writes nothing.
// export (task configure:export, host-side) does the same, then writes only
// .local/configure/deployments.yaml (data-model.md *Staging file*) and prints
// `wrote .local/configure/deployments.yaml` and the next steps. A refusal prints one line,
// `configure: <CODE>: <detail>. <sentence> Next: <action>`, and nothing else, exit 1; an I/O error
// that carries no refusal code prints `configure: error: <error>`, exit 1. Usage errors exit 2.
// Neither command opens a connection, reads the environment or a credential, or starts a process
// (FR-008).
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/configure"
)

// stagingPath is the staging file, relative to the root (FR-007).
const stagingPath = ".local/configure/deployments.yaml"

// stagingOps are the file operations of the staging write that a test replaces to inject a
// failure: the temporary file's write and fsync, the rename onto the target and the directory
// fsync.
type stagingOps struct {
	write   func(f *os.File, data []byte) error
	sync    func(f *os.File) error
	rename  func(oldpath, newpath string) error
	syncDir func(dir string) error
}

var osStaging = stagingOps{
	write:   func(f *os.File, data []byte) error { _, err := f.Write(data); return err },
	sync:    func(f *os.File) error { return f.Sync() },
	rename:  os.Rename,
	syncDir: syncDir,
}

// syncDir fsyncs a directory, so a rename in it survives a crash.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		d.Close()
		return err
	}
	return d.Close()
}

const usageLine = "usage: lz-configure [-root <repo>] check <answers> | export <answers>"

func run(args []string, out io.Writer, ops stagingOps) int {
	flags := flag.NewFlagSet("lz-configure", flag.ContinueOnError)
	flags.SetOutput(out)
	root := flags.String("root", ".", "repository root")
	if err := flags.Parse(args); err != nil || flags.NArg() != 2 || (flags.Arg(0) != "check" && flags.Arg(0) != "export") {
		fmt.Fprintln(out, usageLine)
		return 2
	}
	abs, err := filepath.Abs(*root)
	if err != nil {
		return refuse(out, err)
	}
	lines, manifest, err := prepare(abs, flags.Arg(1))
	if err != nil {
		return refuse(out, err)
	}
	lines = append(lines, "configure: ok")
	if flags.Arg(0) == "export" {
		if err := stage(abs, manifest, ops); err != nil {
			return refuse(out, err)
		}
		lines = append(lines, "wrote "+stagingPath,
			"next: copy "+stagingPath+" to stacks/deployments.yaml",
			"next: task stacks:reconcile",
			"next: task stacks:generate",
			"next: task stacks:check")
	}
	fmt.Fprintln(out, strings.Join(lines, "\n"))
	return 0
}

// prepare reads, decodes, derives and gates the answers, and returns the diff against the root's
// stacks/deployments.yaml and the notes, and the manifest. It writes nothing.
func prepare(root, answers string) ([]string, []byte, error) {
	if !filepath.IsAbs(answers) {
		answers = filepath.Join(root, answers)
	}
	data, err := configure.ReadAnswers(answers)
	if err != nil {
		return nil, nil, err
	}
	a, err := configure.DecodeAnswers(data)
	if err != nil {
		return nil, nil, err
	}
	manifest, err := configure.Export(a)
	if err != nil {
		return nil, nil, err
	}
	// An absent current manifest is nil to Diff (every row `+`); one that does not decode is
	// CURRENT_MANIFEST_INVALID, which Diff returns under the configure source.
	current, err := os.ReadFile(filepath.Join(root, "stacks", "deployments.yaml"))
	if errors.Is(err, fs.ErrNotExist) {
		current, err = nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	diff, err := configure.Diff(current, manifest)
	if err != nil {
		return nil, nil, err
	}
	return append(diff, configure.Notes(a)...), manifest, nil
}

// refuse prints the one refusal line of err and returns exit status 1. An error without a
// catalogue source (an I/O failure other than those the reader codes) is printed as an error,
// one line, never as a refusal of the answers.
func refuse(out io.Writer, err error) int {
	line, xerr := configure.ExplainError(err)
	if xerr != nil {
		msg := err.Error()
		if strings.ContainsFunc(msg, unicode.IsControl) {
			msg = strconv.Quote(msg)
		}
		line = "configure: error: " + msg
	}
	fmt.Fprintln(out, line)
	return 1
}

func stagingErr(code, detail string, err error) error {
	if err != nil {
		detail += ": " + err.Error()
	}
	return &configure.AnswersError{Code: code, Detail: detail}
}

// stage writes the staging file (data-model.md *Staging file*): refuse a symlinked .local,
// .local/configure or target; create the directories (0700) and tighten a looser
// .local/configure to 0700; write a temporary file in it, fsync, rename onto the target; fsync the
// directory. The rename is the publication point: a failure before it removes the temporary file
// and leaves the previous staging file as it was (STAGING_WRITE); a failed directory fsync after it
// is STAGING_NOT_DURABLE.
func stage(root string, manifest []byte, ops stagingOps) error {
	local := filepath.Join(root, ".local")
	dir := filepath.Join(local, "configure")
	target := filepath.Join(dir, "deployments.yaml")
	if err := refuseSymlinks(root, local, dir, target); err != nil {
		return err
	}
	for _, p := range []string{local, dir} {
		if err := ensureDir(root, p); err != nil {
			return err
		}
	}
	tmp, err := publish(dir, target, manifest, ops)
	if err != nil {
		if tmp != "" {
			os.Remove(tmp)
		}
		return stagingErr(configure.CodeStagingWrite, stagingPath, err)
	}
	if err := ops.syncDir(dir); err != nil {
		return stagingErr(configure.CodeStagingNotDurable, rel(root, dir), err)
	}
	return nil
}

// refuseSymlinks is STAGING_SYMLINK when any of paths is a symlink, before anything is written;
// an absent path is fine.
func refuseSymlinks(root string, paths ...string) error {
	for _, p := range paths {
		info, err := os.Lstat(p)
		if err == nil && info.Mode()&fs.ModeSymlink != 0 {
			return stagingErr(configure.CodeStagingSymlink, rel(root, p), nil)
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return stagingErr(configure.CodeStagingWrite, rel(root, p), err)
		}
	}
	return nil
}

// ensureDir creates p (0700) when absent and checks that it is a directory, not a symlink;
// .local/configure with any group or other bit is tightened to 0700.
func ensureDir(root, p string) error {
	if err := os.Mkdir(p, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return stagingErr(configure.CodeStagingWrite, rel(root, p), err)
	}
	info, err := os.Lstat(p)
	switch {
	case err != nil:
		return stagingErr(configure.CodeStagingWrite, rel(root, p), err)
	case info.Mode()&fs.ModeSymlink != 0:
		return stagingErr(configure.CodeStagingSymlink, rel(root, p), nil)
	case !info.IsDir():
		return stagingErr(configure.CodeStagingWrite, rel(root, p), errors.New("not a directory"))
	}
	if filepath.Base(p) == "configure" && info.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(p, 0o700); err != nil {
			return stagingErr(configure.CodeStagingWrite, rel(root, p), err)
		}
	}
	return nil
}

// publish writes data to a temporary file (0600) in dir, fsyncs and closes it and renames it onto
// target. It returns the temporary file's path while that file still exists, for the caller to
// remove on an error.
func publish(dir, target string, data []byte, ops stagingOps) (string, error) {
	f, err := os.CreateTemp(dir, ".deployments.yaml.tmp-*")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	if err := ops.write(f, data); err != nil {
		f.Close()
		return tmp, err
	}
	if err := ops.sync(f); err != nil {
		f.Close()
		return tmp, err
	}
	if err := f.Close(); err != nil {
		return tmp, err
	}
	if err := ops.rename(tmp, target); err != nil {
		return tmp, err
	}
	return "", nil
}

// rel is p relative to the root, for a refusal's detail.
func rel(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil {
		return filepath.ToSlash(r)
	}
	return p
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, osStaging))
}
