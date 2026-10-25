// lz-configure turns an answers file into a checked deployments manifest (004 FR-005 to FR-008).
//
//	lz-configure [-root <repo>] check <answers>
//	lz-configure [-root <repo>] export <answers>
//
// Usage errors exit 2.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

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
	syncDir: func(string) error { return nil },
}

const usageLine = "usage: lz-configure [-root <repo>] check <answers> | export <answers>"

func run(args []string, out io.Writer, ops stagingOps) int {
	flags := flag.NewFlagSet("lz-configure", flag.ContinueOnError)
	flags.SetOutput(out)
	flags.String("root", ".", "repository root")
	if err := flags.Parse(args); err != nil || flags.NArg() != 2 || (flags.Arg(0) != "check" && flags.Arg(0) != "export") {
		fmt.Fprintln(out, usageLine)
		return 2
	}
	_ = ops
	fmt.Fprintln(out, "configure: not implemented")
	return 1
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, osStaging))
}
