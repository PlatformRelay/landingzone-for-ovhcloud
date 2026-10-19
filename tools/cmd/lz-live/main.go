// lz-live runs the live lane on the maintainer host (spec 005 FR-010, FR-011, research R11).
//
//	lz-live <plan|apply|destroy> --reviewed-sha <sha> <instance|all>
//	lz-live chain --reviewed-sha <sha> <instance|all> [--deadline <dur>]
//	lz-live bootstrap --reviewed-sha <sha> [--fresh-account]
//	lz-live probe --reviewed-sha <sha> <probe-root> [--plan-only] [--deadline <dur>]
//	lz-live probe --reviewed-sha <sha> --cleanup <run-id> [--accept-lost-baseline] [--deadline <dur>]
//
// Every verb loads credentials, so every verb first runs the host guard: not inside lz-offline,
// the working directory is the owner's main checkout named in ~/.config/ovh-lz/live.env, not a
// linked worktree, clean, and HEAD is the reviewed SHA reachable from origin/main. A refusal exits
// 3 naming the failed condition; usage errors exit 2; any other failure exits 1.
//
// `probe` runs one probe root through the run core (probe.go, T055); `bootstrap` runs the
// bootstrap phases guard, identify, passphrase, admin, state, publish, verify and revoke
// (bootstrap.go; T043, T057, T089); `plan`, `apply` and `destroy` run through live.Apply, `chain`
// through live.Chain (lane.go; T059, T047). After the guard and before any of them, every verb
// resolves the pinned OpenTofu (pinnedTofu, T093). A blocked bootstrap phase or lane stack, and a
// tofu that is not the pinned one, exit 2.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

const usage = `usage: lz-live <plan|apply|destroy> --reviewed-sha <sha> <instance|all>
       lz-live chain --reviewed-sha <sha> <instance|all> [--deadline <dur>]
       lz-live bootstrap --reviewed-sha <sha> [--fresh-account]
       lz-live probe --reviewed-sha <sha> <probe-root> [--plan-only] [--deadline <dur>]
       lz-live probe --reviewed-sha <sha> --cleanup <run-id> [--accept-lost-baseline] [--deadline <dur>]`

// deps are the host facts lz-live reads; main fills them from the process, tests inject them.
type deps struct {
	Getwd         func() (string, error)
	Home          func() (string, error)
	Getenv        func(string) string
	Git           string // git executable
	OfflineMarker string // its existence means "inside lz-offline"
	Stderr        io.Writer
	Stdout        io.Writer                               // the run's terminal (redacted)
	LookPath      func(string) (string, error)            // tofu on PATH, the pinned rule's last candidate (T093)
	API           func(endpoint string) (live.API, error) // the OVHcloud API of an endpoint
	Now           func() time.Time                        // run ids
	// Terminal opens the operator's terminal (bootstrap --fresh-account, probe
	// --accept-lost-baseline); production reads /dev/tty, tests inject a fake.
	Terminal func(ctx context.Context) (live.Terminal, error)
	// Bootstrap runs the bootstrap phases; nil is live.Bootstrap (tests capture the options).
	Bootstrap func(context.Context, live.BootstrapOptions) ([]live.PhaseResult, error)
	// State turns the state phases' options into Bootstrap's Rest; nil is
	// live.NewBootstrapState(o).Rest (tests capture the options the entry built).
	State func(live.StateOptions) func(context.Context, live.BootstrapAccount) error
	// S3HTTP is the state buckets' S3 client; nil is the store's default (tests dial a fake).
	S3HTTP *http.Client
	// Apply runs plan, apply or destroy; nil is live.Apply (tests capture the options).
	Apply func(context.Context, live.ApplyOptions) error
	// Chain runs chain; nil is live.Chain (tests capture the options).
	Chain func(context.Context, live.ChainOptions) error
}

// parse reads the verb's flags and positional arguments in any order. Only probe, chain and
// bootstrap take more than --reviewed-sha.
func parse(verb string, args []string) (reviewed string, p probeArgs, fresh bool, positional []string, err error) {
	flags := flag.NewFlagSet("lz-live "+verb, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&reviewed, "reviewed-sha", "", "the reviewed commit the owner's checkout must be at")
	if verb == "probe" {
		flags.BoolVar(&p.planOnly, "plan-only", false, "plan and judge the probe, apply nothing")
		flags.StringVar(&p.cleanup, "cleanup", "", "resume the cleanup of a probe run")
		flags.BoolVar(&p.acceptLost, "accept-lost-baseline", false, "owner recovery of a run without a trustworthy baseline (terminal only)")
	}
	if verb == "probe" || verb == "chain" {
		flags.DurationVar(&p.deadline, "deadline", 0, "run deadline (default 45m)")
	}
	if verb == "bootstrap" {
		flags.BoolVar(&fresh, "fresh-account", false, "root keys at the prompt: create the admin, then revoke them")
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
	reviewed, p, fresh, positional, err := parse(verb, args[1:])
	if err == nil && verb == "probe" {
		p, err = p.check(positional)
	}
	if err == nil && verb == "bootstrap" && len(positional) > 0 {
		err = errors.New("bootstrap takes no positional argument")
	}
	if err == nil && (verb == "plan" || verb == "apply" || verb == "destroy" || verb == "chain") && len(positional) != 1 {
		err = errors.New(verb + " takes one target: an instance id or all")
	}
	if err == nil && verb == "chain" && p.deadlineGiven && p.deadline <= 0 {
		err = errors.New("--deadline must be positive")
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
			// Before any credential, API call or tofu run (T093). pinnedTofu checks TMPDIR first
			// (its probe's scratch HOME goes under it, T056), then the version, so both come before
			// the verbs' own credential-free refusals (probe root).
			tofu, err := pinnedTofu(d, h, dir)
			if err != nil {
				return err
			}
			if verb == "probe" {
				return probe(context.Background(), d, dir, filepath.Join(home, ".config", "ovh-lz"), tofu, p)
			}
			if verb == "bootstrap" {
				// SIGINT/SIGTERM cancel the run instead of killing it, so the root credential of
				// --fresh-account is still revoked (research R13).
				ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
				defer stop()
				return bootstrap(ctx, d, dir, filepath.Join(home, ".config", "ovh-lz"), reviewed, tofu, fresh)
			}
			if verb == "chain" {
				// live.Chain handles SIGINT, SIGTERM and SIGHUP itself: they start the destroy-on-exit,
				// which then runs to its own deadline.
				return lane(context.Background(), d, dir, filepath.Join(home, ".config", "ovh-lz"), reviewed, tofu, verb, positional[0], p.deadline)
			}
			// plan, apply, destroy: SIGINT/SIGTERM cancel the run: the running tofu gets SIGINT (its
			// own process group) and the run locks and saved plan are released on the way out. These
			// verbs take no --deadline (p.deadline stays 0): live.Apply bounds them by
			// live.DefaultDeadline (spec 006 FR-001).
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return lane(ctx, d, dir, filepath.Join(home, ".config", "ovh-lz"), reviewed, tofu, verb, positional[0], p.deadline)
		})
	}()
	if err != nil {
		fmt.Fprintln(d.Stderr, "lz-live:", err)
	}
	return live.ExitCode(err)
}

// pinnedTofu resolves the OpenTofu every child of the run uses (T093, ADR-0011): stacks'
// pinned rule against the checkout's mise.toml, with LZ_TOFU from the environment, else from
// live.env (the live command line is fixed, so the owner sets it there), else the entry's tofu,
// else tofu on PATH. A TMPDIR inside the checkout is refused first (exit 3, scratch): the version
// probe's private scratch HOME goes under it (FR-007, T056). Any other failure is blocked (exit 2)
// and names LZ_TOFU: another version is never
// skipped for the next candidate, and a relative LZ_TOFU would name another file in the
// children's working directories.
func pinnedTofu(d deps, h live.Host, checkout string) (string, error) {
	lz, err := h.Setting("LZ_TOFU")
	if err != nil {
		return "", &live.Blocked{Phase: "tofu", Detail: fmt.Sprintf("reading LZ_TOFU: %v", err)}
	}
	if lz != "" && !filepath.IsAbs(lz) {
		pin, err := stacks.TofuPin(checkout)
		if err != nil {
			pin = fmt.Sprintf("version (%v)", err)
		}
		return "", &live.Blocked{Phase: "tofu", Detail: fmt.Sprintf("LZ_TOFU %q is not an absolute path; name the pinned OpenTofu %s by its absolute path", lz, pin)}
	}
	// The probe's HOME is a fresh private scratch directory (FR-007, T056), under a TMPDIR outside
	// the checkout (scratchHome's precondition), removed once the version is read.
	if err := live.ScratchOutside(checkout); err != nil {
		return "", err
	}
	home, remove, err := live.ScratchHome()
	if err != nil {
		return "", &live.Blocked{Phase: "tofu", Detail: fmt.Sprintf("version probe scratch home: %v", err)}
	}
	defer remove()
	tofu, err := stacks.ResolveTofu(checkout, lz, filepath.Join(d.OfflineMarker, "tofu"), home, d.LookPath)
	if err != nil {
		return "", &live.Blocked{Phase: "tofu", Detail: fmt.Sprintf("%v (LZ_TOFU in %s or in the environment names the pinned binary)", err, h.LiveEnv)}
	}
	return tofu, nil
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
		Terminal:      openTTY,
	}))
}
