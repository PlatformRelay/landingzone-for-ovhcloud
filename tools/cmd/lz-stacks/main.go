// lz-stacks reconciles, generates, checks and plans the stack tree of stacks/deployments.yaml
// (005 FR-007, FR-009).
//
//	lz-stacks [-root <repo>] reconcile [-check]
//	lz-stacks [-root <repo>] generate
//	lz-stacks [-root <repo>] check
//	lz-stacks [-root <repo>] plans
//	lz-stacks [-root <repo>] order [-records <dir>] <instance|all>
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
// NAME_COLLISION, or `pass`. order (task stacks:order) prints the run order grouped by level
// (`order a → {b, c} → d`), then one `selected <id> <reason>[,<reason>…] [blocked-on=<p>[,…]]`
// line per selected stack in run order (for <instance>, only that stack, or `unselected <id>`),
// then `note:` lines. It compares each stack's code digest with its record in -records (relative
// to the root; it must exist) or, by default, .local/live/records (missing: a first run, every
// stack `no-record`). Published artefacts and resolved references are not readable offline: a
// producer counts as published at the digest its consumers recorded (at an unknown one when they
// disagree or none did and it has a record), and a recorded resolved digest as current; the live
// lane (T059) compares both. Without a manifest every command prints `fail: no manifest`.
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
		fmt.Fprintln(out, "usage: lz-stacks [-root <repo>] reconcile [-check] | generate | check | plans | order [-records <dir>] <instance|all>")
		return 2
	}
	if err := flags.Parse(args); err != nil || flags.NArg() == 0 {
		return usage()
	}
	var check bool
	var records, target string
	switch cmd := flags.Arg(0); {
	case (cmd == "check" || cmd == "generate" || cmd == "plans") && flags.NArg() == 1:
	case cmd == "reconcile":
		sub := flag.NewFlagSet("reconcile", flag.ContinueOnError)
		sub.SetOutput(out)
		sub.BoolVar(&check, "check", false, "report missing stacks without writing")
		if err := sub.Parse(flags.Args()[1:]); err != nil || sub.NArg() != 0 {
			return usage()
		}
	case cmd == "order":
		sub := flag.NewFlagSet("order", flag.ContinueOnError)
		sub.SetOutput(out)
		sub.StringVar(&records, "records", "", "records directory (default <root>/.local/live/records)")
		if err := sub.Parse(flags.Args()[1:]); err != nil || sub.NArg() != 1 {
			return usage()
		}
		target = sub.Arg(0)
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
	if flags.Arg(0) == "order" {
		// The default records directory may be missing (a first run); one the caller names must
		// exist. A relative one is the root's: under task the process runs in tools/.
		if records == "" {
			records = filepath.Join(abs, ".local", "live", "records")
		} else {
			if !filepath.IsAbs(records) {
				records = filepath.Join(abs, records)
			}
			if fi, err := os.Stat(records); err != nil || !fi.IsDir() {
				return fail(out, fmt.Errorf("records directory %s: not a directory", records))
			}
		}
		return order(out, abs, records, target)
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

// order is `lz-stacks order`: the run order, then the selected set with reasons (research R21).
func order(out io.Writer, root, records, target string) int {
	data, err := os.ReadFile(filepath.Join(root, stacks.ManifestPath))
	if err != nil {
		return fail(out, err)
	}
	m, err := stacks.DecodeManifest(data)
	if err != nil {
		return fail(out, err)
	}
	if target != "all" {
		if _, err := m.Row(target); err != nil {
			return fail(out, err)
		}
	}
	levels, err := stacks.Levels(m)
	if err != nil {
		return fail(out, err)
	}
	var parts []string
	for _, l := range levels {
		if len(l) == 1 {
			parts = append(parts, l[0])
		} else {
			parts = append(parts, "{"+strings.Join(l, ", ")+"}")
		}
	}
	code := map[string]string{}
	for _, in := range m.Instances {
		if code[in.ID], err = stacks.CodeDigest(root, in); err != nil {
			return fail(out, err)
		}
	}
	recs, err := stacks.ReadRecords(records, m)
	if err != nil {
		return fail(out, err)
	}
	// Offline, a producer counts as published at the digest its consumers recorded (unknown when
	// they disagree), and a recorded resolved digest as current.
	artifacts, resolved, conflict := map[string]string{}, map[string]string{}, map[string]bool{}
	for id, r := range recs {
		resolved[id] = r.Resolved
		for p, d := range r.Consumed {
			if prev, ok := artifacts[p]; ok && prev != d {
				conflict[p] = true
			}
			artifacts[p] = d
		}
	}
	// A producer whose consumers disagree, or which has a record of its own but no recorded
	// digest, has published at a digest unknown here: it counts as published at a digest no
	// consumer recorded (they select on input, not blocked).
	for p := range conflict {
		artifacts[p] = "recorded-producer-digest-unknown-offline"
	}
	for id := range recs {
		if _, ok := artifacts[id]; !ok {
			artifacts[id] = "recorded-producer-digest-unknown-offline"
		}
	}
	sel, err := stacks.Select(stacks.SelectOptions{Manifest: m, Code: code, Resolved: resolved, Artifacts: artifacts, Records: recs})
	if err != nil {
		return fail(out, err)
	}
	fmt.Fprintln(out, "order "+strings.Join(parts, " → "))
	found := false
	for _, s := range sel {
		if target != "all" && s.ID != target {
			continue
		}
		found = true
		var rs []string
		for _, r := range s.Reasons {
			rs = append(rs, r.String())
		}
		line := "selected " + s.ID + " " + strings.Join(rs, ",")
		if len(s.Blocked) > 0 {
			line += " blocked-on=" + strings.Join(s.Blocked, ",")
		}
		fmt.Fprintln(out, line)
	}
	if target != "all" && !found {
		fmt.Fprintln(out, "unselected "+target)
	}
	if len(recs) == 0 {
		fmt.Fprintln(out, "note: no records: every stack is selected as on a first run")
	}
	fmt.Fprintln(out, "note: published artefacts and resolved references are compared by the live lane, not here")
	return 0
}

func fail(out io.Writer, err error) int {
	fmt.Fprintln(out, "fail: "+err.Error())
	return 1
}

func main() { os.Exit(run(os.Args[1:], os.Stdout)) }
