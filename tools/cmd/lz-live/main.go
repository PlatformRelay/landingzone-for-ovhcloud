// lz-live runs the live lane on the maintainer host (spec 005 FR-010, FR-011, research R11).
//
//	lz-live <bootstrap|probe|plan|apply|destroy|chain> --reviewed-sha <sha> [args...]
//
// Every verb loads credentials, so every verb first runs the host guard: not inside lz-offline,
// the working directory is the owner's main checkout named in ~/.config/ovh-lz/live.env, not a
// linked worktree, clean, and HEAD is the reviewed SHA reachable from origin/main. A refusal exits
// 3 naming the failed condition; usage errors exit 2; any other failure exits 1.
//
// The verb bodies arrive with the run core (probe, T055), the bootstrap phases (T043, T057),
// plan and apply (T059) and destroy and chain (T047); until then an admitted run stops with
// exit 1 before reading any credential.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

const usage = "usage: lz-live <bootstrap|probe|plan|apply|destroy|chain> --reviewed-sha <sha> [args...]"

// deps are the host facts lz-live reads; main fills them from the process, tests inject them.
type deps struct {
	Getwd         func() (string, error)
	Home          func() (string, error)
	Getenv        func(string) string
	Git           string // git executable
	OfflineMarker string // its existence means "inside lz-offline"
	Stderr        io.Writer
}

func run(args []string, d deps) int {
	if len(args) == 0 || !live.Guarded(args[0]) {
		fmt.Fprintln(d.Stderr, usage)
		return 2
	}
	verb := args[0]
	flags := flag.NewFlagSet("lz-live "+verb, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	reviewed := flags.String("reviewed-sha", "", "the reviewed commit the owner's checkout must be at")
	if err := flags.Parse(args[1:]); err != nil || *reviewed == "" {
		fmt.Fprintln(d.Stderr, usage)
		return 2
	}
	err := func() error {
		dir, err := d.Getwd()
		if err != nil {
			return err
		}
		home, err := d.Home()
		if err != nil {
			return err
		}
		h := live.Host{
			Dir:           dir,
			ReviewedSHA:   *reviewed,
			LiveEnv:       filepath.Join(home, ".config", "ovh-lz", "live.env"),
			OfflineMarker: d.OfflineMarker,
			Getenv:        d.Getenv,
			Git:           d.Git,
		}
		return live.Run(verb, h, func() error {
			return errors.New("lz-live " + verb + ": not implemented yet")
		})
	}()
	if err != nil {
		fmt.Fprintln(d.Stderr, "lz-live:", err)
	}
	return live.ExitCode(err)
}

func main() {
	git, err := exec.LookPath("git")
	if err != nil {
		git = "git" // the guard's first git call fails and refuses
	}
	os.Exit(run(os.Args[1:], deps{
		Getwd:         os.Getwd,
		Home:          os.UserHomeDir,
		Getenv:        os.Getenv,
		Git:           git,
		OfflineMarker: "/tcb",
		Stderr:        os.Stderr,
	}))
}
