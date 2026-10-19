// lz-release renders the ADR-0022 release-train manifest (006 FR-016).
//
//	lz-release manifest [--repo <dir>] --commit <full commit id> --version <YYYY.MM.N>
//
// manifest prints the manifest of the commit in the git repository at <dir> (default: the one
// the working directory is in) to standard output, and nothing else: it reads git objects only,
// creates no tag and writes no file. A refusal (release.Manifest's rule ids) exits 1, a usage
// error 2; neither prints anything on standard output. A read-only host step: the offline
// entry's snapshot carries no .git.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/release"
)

const usage = "usage: lz-release manifest [--repo <dir>] --commit <full commit id> --version <YYYY.MM.N>"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	repo, commit, version, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "lz-release: %v\n%s\n", err, usage)
		return 2
	}
	out, err := release.Manifest(repo, commit, version)
	if err != nil {
		fmt.Fprintf(stderr, "lz-release: %v\n", err)
		return 1
	}
	if _, err := stdout.Write(out); err != nil {
		fmt.Fprintf(stderr, "lz-release: %v\n", err)
		return 1
	}
	return 0
}

func parseArgs(args []string) (repo, commit, version string, err error) {
	if len(args) == 0 || args[0] != "manifest" {
		return "", "", "", errors.New("the only subcommand is manifest")
	}
	flags := flag.NewFlagSet("lz-release manifest", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&repo, "repo", ".", "the git repository, or a directory inside it")
	flags.StringVar(&commit, "commit", "", "the full commit id to render")
	flags.StringVar(&version, "version", "", "the train version, YYYY.MM.N")
	if err := flags.Parse(args[1:]); err != nil {
		return "", "", "", err
	}
	switch {
	case flags.NArg() != 0:
		return "", "", "", fmt.Errorf("unexpected argument %q", flags.Arg(0))
	case commit == "" || version == "":
		return "", "", "", errors.New("--commit and --version are required")
	}
	return repo, commit, version, nil
}
