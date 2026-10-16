package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/bundle"
)

const prepareUsage = "usage: lz-pack prepare -root <private dir> -toolchain <qualified toolchain root> -crane <file> -archive <pin name>=<file> ..."

// archiveFlags collects each -archive <pin name>=<absolute file>, every pin
// name once.
type archiveFlags map[string]string

func (a archiveFlags) String() string { return "" }

func (a archiveFlags) Set(value string) error {
	name, file, ok := strings.Cut(value, "=")
	if !ok || name == "" || !filepath.IsAbs(file) {
		return fmt.Errorf("-archive <pin name>=<absolute file> required: %q", value)
	}
	if _, seen := a[name]; seen {
		return fmt.Errorf("archive %s given twice", name)
	}
	a[name] = file
	return nil
}

// prepareOptions reads prepare's command line.
func prepareOptions(args []string) (bundle.PrepareOptions, error) {
	flags := flag.NewFlagSet("prepare", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	o := bundle.PrepareOptions{Archives: archiveFlags{}}
	flags.StringVar(&o.Root, "root", "", "private preparation root holding proof/go-files.sha256 (absolute)")
	flags.StringVar(&o.Toolchain, "toolchain", "", "qualified toolchain root (absolute)")
	flags.StringVar(&o.Crane, "crane", "", "crane binary that exports the pinned image (absolute)")
	flags.Var(archiveFlags(o.Archives), "archive", "fetched artefact by its pin-table name, as <name>=<absolute file>")
	if err := flags.Parse(args); err != nil {
		return o, fmt.Errorf("PREPARE_ARGS: %w", err)
	}
	if o.Root == "" || o.Toolchain == "" || o.Crane == "" || flags.NArg() != 0 {
		return o, errors.New("PREPARE_ARGS: -root, -toolchain and -crane required, and no other argument")
	}
	return o, nil
}

// prepareMain runs `lz-pack prepare` and returns its exit code: 2 for a
// malformed command line, 1 for a refused preparation.
func prepareMain(args []string, stdout, stderr io.Writer) int {
	o, err := prepareOptions(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		fmt.Fprintln(stderr, prepareUsage)
		return 2
	}
	if err := bundle.Prepare(o); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "bundle %s\nreceipt %s\n", filepath.Join(o.Root, "bundle"), filepath.Join(o.Root, "prepared.json"))
	return 0
}
