// lz-stacks reconciles, generates, checks and plans the stack tree of stacks/deployments.yaml
// (005 FR-007, FR-009).
//
//	lz-stacks [-root <repo>] reconcile [-check]
//	lz-stacks [-root <repo>] generate
//	lz-stacks [-root <repo>] check
//	lz-stacks [-root <repo>] plans
//
// reconcile creates every missing stack with the pinned `terramate create` and prints
// `created <path>` per stack in manifest row order; any other mismatch is refused
// (UNSUPPORTED_CHANGE) with nothing written. With -check it writes nothing and prints
// `fail: missing stack <path>` per stack it would create. generate (task stacks:generate) runs
// the pinned `terramate generate` after the strict manifest decode and prints `generated <path>`
// per file under stacks/ it created, changed or removed, sorted; a `git` stage source is refused
// (STAGE_SOURCE_NOT_IMPLEMENTED) with nothing written. check (task stacks:check) copies the root
// Terramate files and stacks/ to scratch, reconciles there under -check, runs `terramate generate`
// and prints `fail: …` per missing stack and per stale generated file, or `pass`; the candidate is
// never written. plans (task test:stack-plans) plans every stack offline under its generated
// mocked test with the fixture envelopes of tests/fixtures/outputs/envelopes, on a scratch copy,
// prints `planned <path>` per stack that planned, then `fail: …` per stack that did not or for a
// NAME_COLLISION, or `pass`. Without a manifest every command prints `fail: no manifest`.
//
// The pinned Terramate is LZ_TERRAMATE (an absolute path), else the entry's /tcb/terramate, else
// terramate on PATH; the pinned OpenTofu (plans) is LZ_TOFU, else /tcb/tofu, else tofu on PATH;
// each must report the version mise.toml pins. reconcile -check resolves Terramate too, so a check
// never passes on a host whose reconcile would refuse. No command reads a credential or calls the
// host guard. Findings and refusals exit 1, usage errors 2.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

func run(args []string, out io.Writer) int {
	flags := flag.NewFlagSet("lz-stacks", flag.ContinueOnError)
	flags.SetOutput(out)
	root := flags.String("root", ".", "repository root (Terramate project root)")
	usage := func() int {
		fmt.Fprintln(out, "usage: lz-stacks [-root <repo>] reconcile [-check] | generate | check | plans")
		return 2
	}
	if err := flags.Parse(args); err != nil || flags.NArg() == 0 {
		return usage()
	}
	var check bool
	switch cmd := flags.Arg(0); {
	case (cmd == "check" || cmd == "generate" || cmd == "plans") && flags.NArg() == 1:
	case cmd == "reconcile":
		sub := flag.NewFlagSet("reconcile", flag.ContinueOnError)
		sub.SetOutput(out)
		sub.BoolVar(&check, "check", false, "report missing stacks without writing")
		if err := sub.Parse(flags.Args()[1:]); err != nil || sub.NArg() != 0 {
			return usage()
		}
	default:
		return usage()
	}
	abs, err := filepath.Abs(*root)
	if err != nil {
		return fail(out, err)
	}
	if _, err := os.Stat(filepath.Join(abs, stacks.ManifestPath)); errors.Is(err, os.ErrNotExist) {
		return fail(out, stacks.ErrNoManifest)
	}
	if flags.Arg(0) == "plans" {
		return plans(out, abs)
	}
	terramate, err := stacks.PinnedTerramate(abs)
	if err != nil {
		return fail(out, err)
	}
	switch flags.Arg(0) {
	case "check":
		findings, err := stacks.CheckStacks(abs, terramate)
		if err != nil {
			return fail(out, err)
		}
		if len(findings) == 0 {
			fmt.Fprintln(out, "pass")
			return 0
		}
		for _, f := range findings {
			fmt.Fprintln(out, "fail: "+f)
		}
		return 1
	case "generate":
		changed, err := stacks.GenerateReport(stacks.GenerateOptions{Root: abs, Terramate: terramate})
		if err != nil {
			return fail(out, err)
		}
		for _, p := range changed {
			fmt.Fprintln(out, "generated "+p)
		}
		return 0
	}
	report, err := stacks.Reconcile(stacks.ReconcileOptions{Root: abs, Terramate: terramate, Check: check})
	for _, p := range report.Created {
		if check {
			fmt.Fprintln(out, "fail: missing stack "+p)
		} else {
			fmt.Fprintln(out, "created "+p)
		}
	}
	if err != nil {
		return fail(out, err)
	}
	if check && len(report.Created) > 0 {
		return 1
	}
	return 0
}

// plans is `lz-stacks plans`: every stack planned offline, one line per stack, then the verdict.
func plans(out io.Writer, root string) int {
	tofu, err := stacks.PinnedTofu(root)
	if err != nil {
		return fail(out, err)
	}
	planned, err := stacks.PlanStacks(stacks.PlanOptions{Root: root, Tofu: tofu,
		Fixtures: filepath.Join(root, "tests", "fixtures", "outputs", "envelopes")})
	for _, p := range planned {
		fmt.Fprintln(out, "planned "+p.Stack)
	}
	if err != nil {
		for _, line := range strings.Split(err.Error(), "\n") {
			fmt.Fprintln(out, "fail: "+line)
		}
		return 1
	}
	fmt.Fprintln(out, "pass")
	return 0
}

func fail(out io.Writer, err error) int {
	fmt.Fprintln(out, "fail: "+err.Error())
	return 1
}

func main() { os.Exit(run(os.Args[1:], os.Stdout)) }
