// lz-live runs the live lane on the maintainer host (spec 005 FR-010, FR-011, research R11).
//
//	lz-live <bootstrap|plan|apply|destroy|chain> --reviewed-sha <sha> [args...]
//	lz-live probe --reviewed-sha <sha> <probe-root> [--plan-only] [--deadline <dur>]
//	lz-live probe --reviewed-sha <sha> --cleanup <run-id> [--deadline <dur>]
//
// Every verb loads credentials, so every verb first runs the host guard: not inside lz-offline,
// the working directory is the owner's main checkout named in ~/.config/ovh-lz/live.env, not a
// linked worktree, clean, and HEAD is the reviewed SHA reachable from origin/main. A refusal exits
// 3 naming the failed condition; usage errors exit 2; any other failure exits 1.
//
// `probe` runs one probe root through the run core (probe.go, T055). The other verb bodies arrive
// with the bootstrap phases (T043, T057), plan and apply (T059) and destroy and chain (T047);
// until then an admitted run stops with exit 1 before reading any credential.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

const usage = `usage: lz-live <bootstrap|plan|apply|destroy|chain> --reviewed-sha <sha> [args...]
       lz-live probe --reviewed-sha <sha> <probe-root> [--plan-only] [--deadline <dur>]
       lz-live probe --reviewed-sha <sha> --cleanup <run-id> [--deadline <dur>]`

// deps are the host facts lz-live reads; main fills them from the process, tests inject them.
type deps struct {
	Getwd         func() (string, error)
	Home          func() (string, error)
	Getenv        func(string) string
	Git           string // git executable
	OfflineMarker string // its existence means "inside lz-offline"
	Stderr        io.Writer
	Stdout        io.Writer                               // the run's terminal (redacted)
	LookPath      func(string) (string, error)            // tofu
	API           func(endpoint string) (live.API, error) // the OVHcloud API of an endpoint
	Now           func() time.Time                        // run ids
}

// parse reads the verb's flags and positional arguments in any order. Only probe takes more than
// --reviewed-sha.
func parse(verb string, args []string) (reviewed string, p probeArgs, positional []string, err error) {
	flags := flag.NewFlagSet("lz-live "+verb, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&reviewed, "reviewed-sha", "", "the reviewed commit the owner's checkout must be at")
	if verb == "probe" {
		flags.BoolVar(&p.planOnly, "plan-only", false, "plan and judge the probe, apply nothing")
		flags.DurationVar(&p.deadline, "deadline", 0, "run deadline (default 45m)")
		flags.StringVar(&p.cleanup, "cleanup", "", "resume the cleanup of a probe run")
	}
	for {
		if err = flags.Parse(args); err != nil {
			return
		}
		if flags.NArg() == 0 {
			break
		}
		positional = append(positional, flags.Arg(0))
		args = flags.Args()[1:]
	}
	flags.Visit(func(f *flag.Flag) { p.deadlineGiven = p.deadlineGiven || f.Name == "deadline" })
	if reviewed == "" {
		err = errors.New("--reviewed-sha is required")
	}
	return
}

func run(args []string, d deps) int {
	if len(args) == 0 || !live.Guarded(args[0]) {
		fmt.Fprintln(d.Stderr, usage)
		return 2
	}
	verb := args[0]
	reviewed, p, positional, err := parse(verb, args[1:])
	if err == nil && verb == "probe" {
		p, err = p.check(positional)
	}
	if err != nil {
		fmt.Fprintln(d.Stderr, usage)
		return 2
	}
	err = func() error {
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
			ReviewedSHA:   reviewed,
			LiveEnv:       filepath.Join(home, ".config", "ovh-lz", "live.env"),
			OfflineMarker: d.OfflineMarker,
			Getenv:        d.Getenv,
			Git:           d.Git,
		}
		return live.Run(verb, h, func() error {
			if verb == "probe" {
				return probe(context.Background(), d, dir, filepath.Join(home, ".config", "ovh-lz"), p)
			}
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
		Stdout:        os.Stdout,
		LookPath:      exec.LookPath,
		API:           endpointAPI,
		Now:           time.Now,
	}))
}
