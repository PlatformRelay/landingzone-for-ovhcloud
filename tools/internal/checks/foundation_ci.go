package checks

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// FoundationSource is pipelines/github/foundation-source.json: the approved
// inputs of the foundation workflow. The workflow itself is rendered from it
// by RenderFoundationWorkflow, so a reviewed change to this file and to the
// renderer is the only way the workflow changes. Jobs run in parallel, each
// its targets in order, and TimeoutMinutes bounds each of them (D126 part A).
type FoundationSource struct {
	Runner         string            `json:"runner"`
	TimeoutMinutes int               `json:"timeout_minutes"`
	Manifest       string            `json:"manifest"`
	Layer          string            `json:"layer"`
	EntrySHA256    string            `json:"entry_sha256"`
	BwrapSHA256    string            `json:"bwrap_sha256"`
	Jobs           []FoundationJob   `json:"jobs"`
	Deferred       map[string]string `json:"deferred"`
}

// FoundationJob is one job of the workflow: its name and the targets it
// runs, in order, through the offline entry.
type FoundationJob struct {
	Name    string   `json:"name"`
	Targets []string `json:"targets"`
}

// Targets is every target of the source, job by job, in order.
func (s FoundationSource) Targets() []string {
	var targets []string
	for _, j := range s.Jobs {
		targets = append(targets, j.Targets...)
	}
	return targets
}

const (
	// RuntimeImage is the published runtime image, without registry.
	RuntimeImage = "platformrelay/landingzone-for-ovhcloud/runtime"
	// FoundationWorkflow and FoundationSourcePath are where both live.
	FoundationWorkflow   = ".github/workflows/foundation.yml"
	FoundationSourcePath = "pipelines/github/foundation-source.json"
	// FoundationCheck is the gate job: it needs every job of the source and
	// succeeds only when each of them did. Its name is the check the main
	// ruleset requires, so it stays foundation (D123, D126).
	FoundationCheck = "foundation"
	// foundationCreator is the task that creates the CI and its own checks.
	foundationCreator = "T023"
	// maxTimeoutMinutes bounds each job (D125; D126 part A withdrew the
	// 20-minute bridge once the targets ran in parallel jobs).
	maxTimeoutMinutes = 15
	// maxFoundationJobs bounds the jobs: each costs a runner and a fetch of
	// the runtime.
	maxFoundationJobs = 4
	// gateTimeoutMinutes bounds the gate, which starts once the jobs ended
	// and only compares their results.
	gateTimeoutMinutes = 5
	// FoundationChildDeadline is the offline entry's deadline for one target
	// (lz-offline's childDeadline, which its TestChildDeadline binds to this).
	// Each job keeps room for one target to run into it (T055, D125).
	FoundationChildDeadline = 240 * time.Second
)

// foundationJobName is a job name of the family: foundation-, then lower-case
// letters and digits in dash-separated words, so it can carry no YAML,
// expression or shell syntax and never names the gate.
var foundationJobName = regexp.MustCompile(`^foundation(-[a-z0-9]+)+$`)

// hostedRunners are the GitHub-hosted, ephemeral runner labels the workflow
// may use.
var hostedRunners = []string{"ubuntu-24.04"}

// deferrable are the only checks the source may defer, each with why it
// cannot run inside the workflow it judges.
var deferrable = map[string]string{"ci:foundation": "judges this workflow's run evidence after the run completes"}

// AdmitTarget accepts a plain Task target name: a lower-case letter, then
// letters, digits, ':', '-' or '_', at most 64 bytes. The offline entry
// admits targets with it, and the foundation workflow renders only such
// names, so a name can carry no YAML, expression or shell syntax.
func AdmitTarget(name string) error {
	if len(name) == 0 || len(name) > 64 || name[0] < 'a' || name[0] > 'z' {
		return fmt.Errorf("COMMAND_ADMISSION: plain Task target required")
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == ':' || c == '-' || c == '_') {
			return fmt.Errorf("COMMAND_ADMISSION: plain Task target required")
		}
	}
	return nil
}

var (
	imageDigest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	fileDigest  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// ParseFoundationSource decodes the source strictly: no unknown, repeated,
// missing or null field.
func ParseFoundationSource(data []byte) (FoundationSource, error) {
	var s FoundationSource
	if err := DecodeStrict(data, &s); err != nil {
		return FoundationSource{}, fmt.Errorf("SOURCE_SYNTAX: %w", err)
	}
	return s, nil
}

// gateStep is the gate's one step.
const gateStep = "Require every foundation job to succeed"

// jobResults is LZ_JOB_RESULTS, each job of the source with its result as
// result gives it: the gate's template, or what the gate requires.
func jobResults(s FoundationSource, result func(name string) string) string {
	var results []string
	for _, j := range s.Jobs {
		results = append(results, j.Name+"="+result(j.Name))
	}
	return strings.Join(results, " ")
}

// allSucceeded is the only LZ_JOB_RESULTS the gate passes.
func allSucceeded(s FoundationSource) string {
	return jobResults(s, func(string) string { return "success" })
}

// RenderFoundationWorkflow is the only workflow the check accepts. It grants
// the token no permission, uses no Action and no secret, runs on pushes to
// and pull requests against main, and pins the head and the runtime once for
// every job. Each job of the source, in parallel, fetches the exact head as
// data, fetches the runtime layer by digest, installs the pinned launcher and
// runs its targets through the offline entry. The gate, named foundation,
// needs them all and runs whatever they concluded (always()), so it is never
// skipped; it fails unless each of them succeeded.
func RenderFoundationWorkflow(s FoundationSource) string {
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	script := func(name string, lines ...string) {
		line("      - name: %s", name)
		line("        run: |")
		line("          set -euo pipefail")
		for _, l := range lines {
			line("          %s", l)
		}
	}
	line("# Rendered by `lz-check ci-workflow` from %s; do not edit.", FoundationSourcePath)
	line("# Runtime image: ghcr.io/%s@%s", RuntimeImage, s.Manifest)
	line("name: foundation")
	line("on:")
	line("  pull_request:")
	line("    branches: [main]")
	line("  push:")
	line("    branches: [main]")
	line("permissions: {}")
	line("env:")
	line("  LZ_HEAD: ${{ github.event.pull_request.head.sha || github.sha }}")
	line("  LZ_IMAGE: %s", RuntimeImage)
	line("  LZ_MANIFEST: %s", s.Manifest)
	line("  LZ_LAYER: %s", s.Layer)
	line("  LZ_ENTRY_SHA256: %s", s.EntrySHA256)
	line("  LZ_BWRAP_SHA256: %s", s.BwrapSHA256)
	line("jobs:")
	for _, j := range s.Jobs {
		renderTargetJob(line, script, s, j)
	}
	var needs []string
	for _, j := range s.Jobs {
		needs = append(needs, j.Name)
	}
	line("  %s:", FoundationCheck)
	line("    needs: [%s]", strings.Join(needs, ", "))
	line("    if: ${{ always() }}")
	line("    runs-on: %s", s.Runner)
	line("    timeout-minutes: %d", gateTimeoutMinutes)
	line("    env:")
	line("      LZ_JOB_RESULTS: %s", jobResults(s, func(name string) string { return "${{ needs." + name + ".result }}" }))
	line("    steps:")
	script(gateStep,
		`echo "LZ_JOB_RESULTS $LZ_JOB_RESULTS"`,
		`test "$LZ_JOB_RESULTS" = "`+allSucceeded(s)+`"`)
	return b.String()
}

// renderTargetJob renders one job of the source.
func renderTargetJob(line func(string, ...any), script func(string, ...string), s FoundationSource, j FoundationJob) {
	line("  %s:", j.Name)
	line("    runs-on: %s", s.Runner)
	line("    timeout-minutes: %d", s.TimeoutMinutes)
	line("    env:")
	line("      LZ_TARGETS: %s", strings.Join(j.Targets, " "))
	line("    steps:")
	// The runner context is not available to env.
	script("Locate the work directories",
		`echo "LZ_CANDIDATE=$RUNNER_TEMP/candidate" >> "$GITHUB_ENV"`,
		`echo "LZ_RUNTIME=$RUNNER_TEMP/runtime" >> "$GITHUB_ENV"`)
	script("Fetch the exact head as data",
		`git init -q "$LZ_CANDIDATE"`,
		`git -C "$LZ_CANDIDATE" fetch -q --depth=1 --no-tags "$GITHUB_SERVER_URL/$GITHUB_REPOSITORY" "$LZ_HEAD"`,
		`git -c core.hooksPath=/dev/null -c filter.lfs.process= -c filter.lfs.smudge= -c filter.lfs.required=false -C "$LZ_CANDIDATE" checkout -q --detach FETCH_HEAD`,
		`test "$(git -C "$LZ_CANDIDATE" rev-parse HEAD)" = "$LZ_HEAD"`,
		`echo "LZ_CANDIDATE_HEAD $LZ_HEAD"`)
	script("Fetch the runtime layer by digest",
		`token=$(curl --proto '=https' -fsS "https://ghcr.io/token?scope=repository:$LZ_IMAGE:pull" | jq -r .token)`,
		`curl --proto '=https' --proto-redir '=https' -fsSL -H "Authorization: Bearer $token" -H "Accept: application/vnd.oci.image.manifest.v1+json" -o "$RUNNER_TEMP/manifest.json" "https://ghcr.io/v2/$LZ_IMAGE/manifests/$LZ_MANIFEST"`,
		`echo "${LZ_MANIFEST#sha256:}  $RUNNER_TEMP/manifest.json" | sha256sum -c -`,
		`test "$(jq -r '.layers | length' "$RUNNER_TEMP/manifest.json")" = 1`,
		`test "$(jq -r '.layers[0].digest' "$RUNNER_TEMP/manifest.json")" = "$LZ_LAYER"`,
		`curl --proto '=https' --proto-redir '=https' -fsSL -H "Authorization: Bearer $token" -o "$RUNNER_TEMP/layer.tgz" "https://ghcr.io/v2/$LZ_IMAGE/blobs/$LZ_LAYER"`,
		`echo "${LZ_LAYER#sha256:}  $RUNNER_TEMP/layer.tgz" | sha256sum -c -`,
		`mkdir "$LZ_RUNTIME"`,
		`tar -xzpf "$RUNNER_TEMP/layer.tgz" -C "$LZ_RUNTIME"`,
		`echo "$LZ_ENTRY_SHA256  $LZ_RUNTIME/runtime/lz-offline" | sha256sum -c -`,
		`echo "$LZ_BWRAP_SHA256  $LZ_RUNTIME/host/bwrap" | sha256sum -c -`)
	script("Install the pinned launcher",
		`sudo install -m 0755 "$LZ_RUNTIME/host/bwrap" /usr/bin/bwrap`,
		`if [ -e /proc/sys/kernel/apparmor_restrict_unprivileged_userns ]; then sudo sysctl -q -w kernel.apparmor_restrict_unprivileged_userns=0; fi`)
	script(checksStep,
		`for target in $LZ_TARGETS; do`,
		`  "$LZ_RUNTIME/runtime/lz-offline" --candidate "$LZ_CANDIDATE" -- task "$target"`,
		`  echo "LZ_TARGET_OK $target"`,
		`done`,
		`echo "LZ_TARGETS_DONE $(wc -w <<<"$LZ_TARGETS")"`)
}

// ValidateFoundationSource checks what can be judged from the source alone:
// a hosted runner, a bounded timeout, well-formed digests, one to four jobs
// of distinct plain names, none the gate's, each running a target, and plain
// target names. ci-workflow renders only a source without findings, so no
// name can inject into the workflow. Deferred names are not rendered, and
// only the deferrable ones are accepted.
func ValidateFoundationSource(s FoundationSource) []Finding {
	var findings []Finding
	add := func(rule, subject, format string, args ...any) {
		findings = append(findings, Finding{Rule: rule, Subject: subject, Detail: fmt.Sprintf(format, args...)})
	}
	if !slices.Contains(hostedRunners, s.Runner) {
		add("SOURCE_PIN", "runner", "%q is not a hosted runner of %v", s.Runner, hostedRunners)
	}
	if s.TimeoutMinutes < 1 || s.TimeoutMinutes > maxTimeoutMinutes {
		add("SOURCE_PIN", "timeout_minutes", "%d is outside 1..%d", s.TimeoutMinutes, maxTimeoutMinutes)
	}
	for field, value := range map[string]string{"manifest": s.Manifest, "layer": s.Layer} {
		if !imageDigest.MatchString(value) {
			add("SOURCE_PIN", field, "%q is not a sha256 digest", value)
		}
	}
	for field, value := range map[string]string{"entry_sha256": s.EntrySHA256, "bwrap_sha256": s.BwrapSHA256} {
		if !fileDigest.MatchString(value) {
			add("SOURCE_PIN", field, "%q is not a hex SHA-256", value)
		}
	}
	findings = append(findings, validateJobs(s.Jobs)...)
	for _, id := range s.Targets() {
		if AdmitTarget(id) != nil {
			add("CHECK_NAME", id, "not a plain Task target")
		}
	}
	return findings
}

// validateJobs requires one to maxFoundationJobs jobs of distinct names of
// the family, each running a target.
func validateJobs(jobs []FoundationJob) []Finding {
	var findings []Finding
	add := func(subject, format string, args ...any) {
		findings = append(findings, Finding{Rule: "SOURCE_JOBS", Subject: subject, Detail: fmt.Sprintf(format, args...)})
	}
	if len(jobs) < 1 || len(jobs) > maxFoundationJobs {
		add("jobs", "%d jobs, want 1..%d", len(jobs), maxFoundationJobs)
	}
	names := map[string]bool{}
	for _, j := range jobs {
		switch {
		case !foundationJobName.MatchString(j.Name):
			add(j.Name, "not a job name of the form foundation-<word>")
		case names[j.Name]:
			add(j.Name, "named twice")
		case len(j.Targets) == 0:
			add(j.Name, "runs no target")
		}
		names[j.Name] = true
	}
	return findings
}

// CheckFoundationCI judges the approved source and the committed workflow.
// The source must pin a hosted runner, a bounded timeout and well-formed
// digests, and account for every argument-free check of a closed spec 001
// task or of T023 itself: each runs once, in one job, or is deferred with a
// reason. An
// argument-free check of another spec's closed task (tasks keyed by spec
// number, creator "005/T055") may run too. The workflow must be exactly the
// rendered one.
func CheckFoundationCI(source, workflow []byte, registry Registry, tasks map[string][]Task) []Finding {
	var findings []Finding
	add := func(rule, subject, format string, args ...any) {
		findings = append(findings, Finding{Rule: rule, Subject: subject, Detail: fmt.Sprintf(format, args...)})
	}
	s, err := ParseFoundationSource(source)
	if err != nil {
		add("SOURCE_SYNTAX", FoundationSourcePath, "%v", err)
		return findings
	}
	findings = append(findings, ValidateFoundationSource(s)...)

	known := map[string]bool{}
	runnable := map[string]bool{}
	required := map[string]bool{}
	for _, c := range registry.Checks {
		known[c.ID] = true
		if c.Command != "task "+c.ID {
			continue
		}
		if c.Creator == foundationCreator || creatorDone(c.Creator, tasks) {
			runnable[c.ID] = true
			// Only spec 001's own checks are owed to this workflow; another
			// spec's closed check may join it, but need not.
			if !strings.Contains(c.Creator, "/") {
				required[c.ID] = true
			}
		}
	}
	if len(s.Targets()) == 0 {
		add("NO_TARGETS", "targets", "the workflow would run no check")
	}
	seen := map[string]bool{}
	for _, id := range s.Targets() {
		switch {
		case seen[id]:
			add("CHECK_DUPLICATE", id, "listed twice, in one job or in two")
		case !known[id]:
			add("CHECK_UNKNOWN", id, "not in the check registry")
		case !runnable[id]:
			add("CHECK_NOT_RUNNABLE", id, "its creator is open or in no spec read, or its command takes an argument")
		}
		seen[id] = true
	}
	for id, reason := range s.Deferred {
		switch {
		case !known[id]:
			add("CHECK_UNKNOWN", id, "deferred but not in the check registry")
		case seen[id]:
			add("CHECK_DEFERRED", id, "both run and deferred")
		case deferrable[id] == "":
			add("CHECK_DEFERRED", id, "not deferrable; only %s may be", strings.Join(slices.Sorted(maps.Keys(deferrable)), ", "))
		case strings.TrimSpace(reason) == "":
			add("CHECK_DEFERRED", id, "deferred without a reason")
		}
	}
	for _, id := range sortedSet(required) {
		if _, deferred := s.Deferred[id]; !seen[id] && !deferred {
			add("CHECK_OMITTED", id, "required check neither run nor deferred")
		}
	}

	if string(workflow) != RenderFoundationWorkflow(s) {
		add("WORKFLOW_DRIFT", FoundationWorkflow, "differs from the workflow rendered from %s", FoundationSourcePath)
	}
	return findings
}

// creatorDone reports whether a registry creator names a ticked task of the
// spec whose tasks are keyed by its number: a bare id is spec 001's, a
// spec-scoped "005/T055" is spec 005's T055. An unread spec or an id it lacks
// is not done.
func creatorDone(creator string, tasks map[string][]Task) bool {
	for spec, list := range tasks {
		id, ok := ownKey(spec, creator)
		if !ok {
			continue
		}
		for _, t := range list {
			if t.ID == id {
				return t.Done
			}
		}
	}
	return false
}

const (
	// FoundationRepository is the only repository whose runs are evidence.
	FoundationRepository = "PlatformRelay/landingzone-for-ovhcloud"
	// FoundationRunsPath is the committed record of the runs ci:foundation
	// judges.
	FoundationRunsPath = "pipelines/github/foundation-runs.json"
	// RoleHead is a run that must show the candidate head green; a
	// RoleRedControl run must show a broken candidate red in the checks.
	RoleHead       = "head"
	RoleRedControl = "red-control"
)

// FoundationRuns is the record ci:foundation judges: one observation per
// GitHub run, each derived by ObserveFoundationRun from GitHub's own output.
type FoundationRuns struct {
	Runs []FoundationRun `json:"runs"`
}

// FoundationRun is what one run of the foundation workflow showed. Candidate
// is the commit the run is evidence for; the rest is observed. CheckRunID is
// the check run of the gate, the check named foundation (0 when the run had
// no gate); Jobs are the run's jobs, ordered by name.
type FoundationRun struct {
	Role           string            `json:"role"`
	Candidate      string            `json:"candidate"`
	RunID          int64             `json:"run_id"`
	Attempt        int               `json:"attempt"`
	CheckRunID     int64             `json:"check_run_id"`
	Repository     string            `json:"repository"`
	HeadRepository string            `json:"head_repository"`
	Event          string            `json:"event"`
	HeadSHA        string            `json:"head_sha"`
	WorkflowPath   string            `json:"workflow_path"`
	WorkflowBlob   string            `json:"workflow_blob"`
	SourceBlob     string            `json:"source_blob"`
	Status         string            `json:"status"`
	Conclusion     string            `json:"conclusion"`
	Jobs           []RunJob          `json:"jobs"`
	CheckDigests   map[string]string `json:"check_digests"`
}

// RunJob is what one job of the run showed: its own check run, outcome,
// runner, token, steps, the environment of its main step (the checks step of
// a job of the source, the gate's step of the gate), the head it fetched and
// its targets. OutsideSeconds is the job's time outside its targets
// (timeTargets); a job without targets has TargetsDone -1 and spends its
// whole log outside them.
type RunJob struct {
	Name           string            `json:"name"`
	CheckRunID     int64             `json:"check_run_id"`
	Conclusion     string            `json:"conclusion"`
	RunnerLabels   []string          `json:"runner_labels"`
	RunnerGroup    string            `json:"runner_group"`
	RunnerImage    string            `json:"runner_image"`
	Permissions    []string          `json:"permissions"`
	Steps          []RunStep         `json:"steps"`
	Env            map[string]string `json:"env"`
	FetchedHead    string            `json:"fetched_head"`
	Targets        []RunTarget       `json:"targets"`
	TargetsDone    int               `json:"targets_done"`
	OutsideSeconds int               `json:"outside_seconds"`
}

// job is the run's job of that name, or nil.
func (r *FoundationRun) job(name string) *RunJob {
	for i := range r.Jobs {
		if r.Jobs[i].Name == name {
			return &r.Jobs[i]
		}
	}
	return nil
}

// RunStep is one step of a run's job as GitHub reported it.
type RunStep struct {
	Name       string `json:"name"`
	Conclusion string `json:"conclusion"`
}

// RunTarget is one target of the checks step, in log order: ok when the
// workflow printed LZ_TARGET_OK for it, failed when Task reported it failed.
// Discovered counts its passing package lines and summary lines, Empty its
// packages that ran no test in any of its lines (a target may split one
// package's tests over concurrent `go test` processes, so one half printing
// `[no tests to run]` is no finding when another ran tests there), Failures
// the failed test assertions (`--- FAIL:` lines) of a failed target, and
// Seconds how long it ran: the time from the checks step's first log line, or
// the previous target's end, to its own end line, rounded up to a second.
type RunTarget struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Discovered int    `json:"discovered"`
	Empty      int    `json:"empty"`
	Failures   int    `json:"failures"`
	Seconds    int    `json:"seconds"`
}

// RunCapture is GitHub's own output for one run, captured read-only: the run
// and its jobs from the REST API, the blob SHAs of the workflow and the
// source at the run's head, and the job log; plus Digests, the CheckDigest
// of every source target computed on a checkout of the run's head.
type RunCapture struct {
	Run, Jobs, WorkflowBlob, SourceBlob, Log []byte
	Digests                                  map[string]string
}

// FoundationDigests is the CheckDigest of each target in root: what decides
// the target's result there. A run is evidence for a tree only where these
// are equal.
func FoundationDigests(root string, registry Registry, targets []string) (map[string]string, error) {
	digests := map[string]string{}
	for _, id := range targets {
		i := slices.IndexFunc(registry.Checks, func(c CheckDefinition) bool { return c.ID == id })
		if i < 0 {
			return nil, fmt.Errorf("CHECK_UNKNOWN: %s is not in the check registry", id)
		}
		digest, err := CheckDigest(root, registry.Checks[i])
		if err != nil {
			return nil, fmt.Errorf("DIGEST: %s: %w", id, err)
		}
		digests[id] = digest
	}
	return digests, nil
}

var (
	gitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
	// A summary line a foundation target prints when it judged something:
	// TRACE_OK, DEPENDENCIES_OK, TOOLCHAIN_QUALIFIED, ...
	summaryLine = regexp.MustCompile(`^[A-Z][A-Z_]*_(OK|QUALIFIED|PASS) `)
	failedTask  = regexp.MustCompile(`^task: Failed to run task "([^"]+)"`)
)

// capturedJob is one job of a run as GitHub's jobs API reports it.
type capturedJob struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	RunID       int64    `json:"run_id"`
	RunAttempt  int      `json:"run_attempt"`
	HeadSHA     string   `json:"head_sha"`
	Conclusion  *string  `json:"conclusion"`
	Labels      []string `json:"labels"`
	RunnerGroup string   `json:"runner_group_name"`
	Steps       []struct {
		Name       string  `json:"name"`
		Conclusion *string `json:"conclusion"`
	} `json:"steps"`
}

// ObserveFoundationRun derives the observation of one run from its capture,
// job by job. It refuses a capture that is not one completed-or-not run of
// one or more uniquely named jobs of that run, a log line of a job the run
// does not have, a blob that is not a git SHA, or an empty log; it judges
// nothing.
func ObserveFoundationRun(role, candidate string, c RunCapture) (FoundationRun, error) {
	var run struct {
		ID         int64   `json:"id"`
		Attempt    int     `json:"run_attempt"`
		Event      string  `json:"event"`
		HeadSHA    string  `json:"head_sha"`
		Path       string  `json:"path"`
		Status     string  `json:"status"`
		Conclusion *string `json:"conclusion"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		HeadRepository struct {
			FullName string `json:"full_name"`
		} `json:"head_repository"`
	}
	if err := json.Unmarshal(c.Run, &run); err != nil {
		return FoundationRun{}, fmt.Errorf("CAPTURE_RUN: %w", err)
	}
	jobs, err := captureJobs(c.Jobs)
	if err != nil {
		return FoundationRun{}, err
	}
	blobs := [2]string{strings.TrimSpace(string(c.WorkflowBlob)), strings.TrimSpace(string(c.SourceBlob))}
	for _, blob := range blobs {
		if !gitSHA.MatchString(blob) {
			return FoundationRun{}, fmt.Errorf("CAPTURE_BLOB: %q is not a git blob SHA", blob)
		}
	}
	if len(c.Log) == 0 {
		return FoundationRun{}, fmt.Errorf("CAPTURE_LOG: the job log is empty")
	}
	if len(c.Digests) == 0 {
		return FoundationRun{}, fmt.Errorf("CAPTURE_TREE: no check digests of the run's head")
	}
	logs, err := readLog(string(c.Log), jobs)
	if err != nil {
		return FoundationRun{}, err
	}
	o := FoundationRun{
		Role: role, Candidate: candidate, RunID: run.ID, Attempt: run.Attempt,
		Repository: run.Repository.FullName, HeadRepository: run.HeadRepository.FullName,
		Event: run.Event, HeadSHA: run.HeadSHA, WorkflowPath: run.Path,
		WorkflowBlob: blobs[0], SourceBlob: blobs[1],
		Status: run.Status, Conclusion: deref(run.Conclusion), Jobs: []RunJob{},
		CheckDigests: maps.Clone(c.Digests),
	}
	if err := observeJobs(&o, jobs, logs); err != nil {
		return FoundationRun{}, err
	}
	return o, nil
}

// observeJobs observes each job of the run, refusing one of another run,
// attempt or head, orders them by name and takes the gate's check run as the
// run's.
func observeJobs(o *FoundationRun, jobs []capturedJob, logs map[string][]logLine) error {
	for _, job := range jobs {
		if job.RunID != o.RunID || job.RunAttempt != o.Attempt || job.HeadSHA != o.HeadSHA {
			return fmt.Errorf("CAPTURE_JOBS: job %s of run %d attempt %d at %s, want run %d attempt %d at %s",
				job.Name, job.RunID, job.RunAttempt, job.HeadSHA, o.RunID, o.Attempt, o.HeadSHA)
		}
		j, err := observeJob(job, logs[job.Name])
		if err != nil {
			return err
		}
		if job.Name == FoundationCheck {
			o.CheckRunID = job.ID
		}
		o.Jobs = append(o.Jobs, j)
	}
	slices.SortFunc(o.Jobs, func(a, b RunJob) int { return strings.Compare(a.Name, b.Name) })
	return nil
}

// captureJobs decodes the run's jobs: at least one, as many as GitHub
// counted, each named, no name twice.
func captureJobs(data []byte) ([]capturedJob, error) {
	var jobs struct {
		TotalCount int           `json:"total_count"`
		Jobs       []capturedJob `json:"jobs"`
	}
	if err := json.Unmarshal(data, &jobs); err != nil {
		return nil, fmt.Errorf("CAPTURE_JOBS: %w", err)
	}
	if jobs.TotalCount < 1 || jobs.TotalCount != len(jobs.Jobs) {
		return nil, fmt.Errorf("CAPTURE_JOBS: %d jobs counted, %d listed; want one or more, all listed", jobs.TotalCount, len(jobs.Jobs))
	}
	names := map[string]bool{}
	for _, job := range jobs.Jobs {
		if job.Name == "" || names[job.Name] {
			return nil, fmt.Errorf("CAPTURE_JOBS: job name %q missing or repeated", job.Name)
		}
		names[job.Name] = true
	}
	return jobs.Jobs, nil
}

// observeJob observes one job from its entry in the jobs and its lines of
// the log.
func observeJob(job capturedJob, lines []logLine) (RunJob, error) {
	j := RunJob{
		Name: job.Name, CheckRunID: job.ID, Conclusion: deref(job.Conclusion),
		RunnerLabels: append([]string{}, job.Labels...), RunnerGroup: job.RunnerGroup,
		Permissions: []string{}, Steps: []RunStep{}, Env: map[string]string{}, Targets: []RunTarget{}, TargetsDone: -1,
	}
	for _, s := range job.Steps {
		j.Steps = append(j.Steps, RunStep{Name: s.Name, Conclusion: deref(s.Conclusion)})
	}
	lines, err := splitLog(lines, j.Steps)
	if err != nil {
		return RunJob{}, fmt.Errorf("%w (job %s)", err, job.Name)
	}
	return j, observeLog(&j, lines)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// checksStep is the step that runs the targets; its env block is the one
// the targets ran with.
const checksStep = "Run the foundation checks inside the offline entry"

// unknownStep is the step column `gh run view --log` prints on a line it
// cannot attribute to a step; gh 2.98.0 prints it on every line of a
// successful run.
const unknownStep = "UNKNOWN STEP"

// stepMarker opens the log of each workflow step: `##[group]Run ` and the
// step's first command line.
const stepMarker = "##[group]Run "

// logLine is one line of `gh run view --log` output: its step, its timestamp
// and its text without the timestamp.
type logLine struct{ step, stamp, text string }

// readLog reads `gh run view --log` output: job, step and a timestamped
// line, tab-separated, into each job's lines in order. A line of a job the
// run does not have is refused.
func readLog(log string, jobs []capturedJob) (map[string][]logLine, error) {
	lines := map[string][]logLine{}
	for _, job := range jobs {
		lines[job.Name] = nil
	}
	for _, raw := range strings.Split(log, "\n") {
		fields := strings.SplitN(raw, "\t", 3)
		if len(fields) != 3 {
			continue
		}
		if _, known := lines[fields[0]]; !known {
			return nil, fmt.Errorf("CAPTURE_LOG: a line of job %q, which the run does not have", fields[0])
		}
		text := strings.TrimPrefix(fields[2], "\uFEFF")
		stamp, rest, ok := strings.Cut(text, " ")
		if ok {
			text = rest
		} else {
			text = ""
		}
		lines[fields[0]] = append(lines[fields[0]], logLine{step: fields[1], stamp: stamp, text: strings.TrimRight(text, "\r")})
	}
	return lines, nil
}

// splitLog attributes one job's lines to its steps. When gh names no step,
// it attributes them from the log itself: those before the first step marker
// to "Set up job", and each marker and the lines after it to the next step
// between "Set up job" and "Complete job" of steps, the job's steps as GitHub
// reported them, that was not skipped, since a skipped step prints no marker.
// The lines after the last marker stay with the last workflow step, as no
// marker opens "Complete job". Markers and steps that do not pair one to one,
// or a log that names some steps and not others, are refused; so a step's
// output that prints a marker has its run refused, not misread.
func splitLog(lines []logLine, steps []RunStep) ([]logLine, error) {
	unknown := 0
	for _, l := range lines {
		if l.step == unknownStep {
			unknown++
		}
	}
	switch unknown {
	case 0:
		return lines, nil
	case len(lines):
	default:
		return nil, fmt.Errorf("CAPTURE_LOG: %d of %d lines name no step", unknown, len(lines))
	}
	names := stepNames(steps)
	n := len(names)
	if n < 2 || names[0] != "Set up job" || names[n-1] != "Complete job" {
		return nil, fmt.Errorf("CAPTURE_LOG: steps %q do not run from Set up job to Complete job", names)
	}
	workflow := []string{}
	for _, s := range steps[1 : n-1] {
		if s.Conclusion != "skipped" {
			workflow = append(workflow, s.Name)
		}
	}
	markers := 0
	for _, l := range lines {
		if strings.HasPrefix(l.text, stepMarker) {
			markers++
		}
	}
	if markers != len(workflow) {
		return nil, fmt.Errorf("CAPTURE_LOG: %d step markers for the %d workflow steps %q", markers, len(workflow), workflow)
	}
	step, next := names[0], 0
	for i := range lines {
		if strings.HasPrefix(lines[i].text, stepMarker) {
			step = workflow[next]
			next++
		}
		lines[i].step = step
	}
	return lines, nil
}

// targetScan reads the targets from the checks step's output: each target's
// outcome, discovery and failed assertions, the lines they closed on, and the
// done count.
type targetScan struct {
	o        *RunJob
	current  RunTarget
	failures int
	// tested and idle are the current target's packages that ran tests and
	// that printed `[no tests to run]`; Empty is what is idle and untested.
	tested, idle map[string]bool
	// ends are the lines each target closed on, in order.
	ends []int
}

func (s *targetScan) close(at int, name, status string) {
	s.ends = append(s.ends, at)
	for pkg := range s.idle {
		if !s.tested[pkg] {
			s.current.Empty++
		}
	}
	s.current.Name, s.current.Status = name, status
	s.o.Targets = append(s.o.Targets, s.current)
	s.current, s.tested, s.idle = RunTarget{}, map[string]bool{}, map[string]bool{}
}

func (s *targetScan) line(at int, text string) {
	if strings.HasPrefix(strings.TrimLeft(text, " "), "--- FAIL: ") {
		s.failures++
	}
	if name, ok := strings.CutPrefix(text, "LZ_TARGET_OK "); ok {
		s.close(at, name, "ok")
		s.failures = 0
	} else if m := failedTask.FindStringSubmatch(text); m != nil {
		s.close(at, m[1], "failed")
	} else if count, ok := strings.CutPrefix(text, "LZ_TARGETS_DONE "); ok {
		if n, err := strconv.Atoi(count); err == nil {
			s.o.TargetsDone = n
		}
	} else if pkg, isPackage := goPackage(text); isPackage && strings.Contains(text, "[no tests to run]") {
		s.idle[pkg] = true
	} else if isPackage {
		s.tested[pkg] = true
		s.current.Discovered++
	} else if summaryLine.MatchString(text) {
		s.current.Discovered++
	}
}

// finish gives a failed last target the failed assertions since the last
// passing one: Go prints them around Task's failure line.
func (s *targetScan) finish() {
	if n := len(s.o.Targets); n > 0 && s.o.Targets[n-1].Status == "failed" {
		s.o.Targets[n-1].Failures = s.failures
	}
}

// setUpLine takes the runner image and the token permissions from the job
// set-up.
func setUpLine(o *RunJob, group, text string) {
	switch {
	case group == "Runner Image" && strings.HasPrefix(text, "Image: "):
		o.RunnerImage = strings.TrimPrefix(text, "Image: ")
	case group == "GITHUB_TOKEN Permissions":
		o.Permissions = append(o.Permissions, text)
	}
}

// envLine reads one line of a step's echoed environment, which `env:` opens:
// each `  NAME: value` line under it is one variable. It reports whether the
// environment is open.
func envLine(o *RunJob, inEnv bool, text string) bool {
	if text == "env:" {
		return true
	}
	if name, value, ok := strings.Cut(strings.TrimPrefix(text, "  "), ": "); inEnv && ok && strings.HasPrefix(text, "  ") {
		o.Env[name] = value
	}
	return inEnv
}

// observeLog takes the runner image and token permissions from the job
// set-up, the fetched head, the environment of the checks step or the gate's
// step, and each target's outcome, discovery and time from the job's lines
// (splitLog, timeTargets).
func observeLog(o *RunJob, lines []logLine) error {
	var group string
	inEnv := false
	scan := &targetScan{o: o, tested: map[string]bool{}, idle: map[string]bool{}, ends: []int{}}
	fetched := map[string]bool{}
	for at, l := range lines {
		switch {
		case strings.HasPrefix(l.text, "##[group]"):
			group = strings.TrimPrefix(l.text, "##[group]")
			continue
		case l.text == "##[endgroup]":
			group, inEnv = "", false
			continue
		}
		switch l.step {
		case "Set up job":
			setUpLine(o, group, l.text)
		case "Fetch the exact head as data":
			if head, ok := strings.CutPrefix(l.text, "LZ_CANDIDATE_HEAD "); ok {
				fetched[head] = true
			}
		case checksStep, gateStep:
			if group != "" {
				inEnv = envLine(o, inEnv, l.text)
			} else if l.step == checksStep {
				scan.line(at, l.text)
			}
		}
	}
	scan.finish()
	// A job that fetched more than one head proves none of them.
	if len(fetched) == 1 {
		for head := range fetched {
			o.FetchedHead = head
		}
	}
	return timeTargets(o, lines, scan.ends)
}

// timeTargets sets each target's Seconds from the log's timestamps: the time
// from the checks step's first line, or the previous target's end line, to
// its own end line, rounded up to a second. The job's timeout covers the
// whole job, so OutsideSeconds is the rest of the log: from its first line to
// the checks step's first (the whole log if there is none), and from the last
// target's end to its last line. A line without a time, or a span that ends
// before it began, is refused rather than guessed at.
func timeTargets(o *RunJob, lines []logLine, ends []int) error {
	if len(lines) == 0 {
		return nil
	}
	times := make([]time.Time, len(lines))
	for i, l := range lines {
		t, err := time.Parse(time.RFC3339Nano, l.stamp)
		if err != nil {
			return fmt.Errorf("CAPTURE_LOG: line %d has no time: %q", i+1, l.stamp)
		}
		times[i] = t
	}
	since := slices.IndexFunc(lines, func(l logLine) bool { return l.step == checksStep })
	if since < 0 {
		since = len(lines) - 1
	}
	before := times[since].Sub(times[0])
	for k, end := range ends {
		took := times[end].Sub(times[since])
		if took < 0 {
			return fmt.Errorf("CAPTURE_LOG: target %s ends at %s, before it began at %s", o.Targets[k].Name, lines[end].stamp, lines[since].stamp)
		}
		o.Targets[k].Seconds = ceilSeconds(took)
		since = end
	}
	after := times[len(times)-1].Sub(times[since])
	if before < 0 || after < 0 {
		return fmt.Errorf("CAPTURE_LOG: the log's first or last line is not outside its targets (%s, %s)", lines[0].stamp, lines[len(lines)-1].stamp)
	}
	o.OutsideSeconds = ceilSeconds(before + after)
	return nil
}

// ceilSeconds is d rounded up to a whole second.
func ceilSeconds(d time.Duration) int {
	return int((d + time.Second - 1) / time.Second)
}

// goPackage is the package of a passing `go test` package line,
// `ok  \t<package>\t<time>[ <note>]`.
func goPackage(text string) (string, bool) {
	rest, ok := strings.CutPrefix(text, "ok  \t")
	if !ok {
		return "", false
	}
	pkg, _, _ := strings.Cut(rest, "\t")
	return pkg, true
}

// gitBlob is the git blob SHA of data, as GitHub reports file contents.
func gitBlob(data []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// foundationExpectation is what every run of the approved source must show:
// its jobs, by name in order, the steps of a job of the source and of the
// gate, and the pinned workflow environment every step sees.
type foundationExpectation struct {
	source       FoundationSource
	workflowBlob string
	sourceBlob   string
	jobs         []string
	targetSteps  []string
	gateSteps    []string
	env          map[string]string
	digests      map[string]string
}

// runEnvKeys are the checks step's variables whose value is the run's own:
// the head, checked separately, and the work directories.
var runEnvKeys = []string{"LZ_HEAD", "LZ_CANDIDATE", "LZ_RUNTIME"}

// gateEnvKeys are the gate's variables beyond the pinned ones: the head,
// checked separately, and the results it judged, judged with the outcome.
var gateEnvKeys = []string{"LZ_HEAD", "LZ_JOB_RESULTS"}

// hostedRunnerGroup is the runner group of GitHub-hosted, ephemeral runners.
const hostedRunnerGroup = "GitHub Actions"

// tokenPermissions is what `permissions: {}` leaves the job token.
var tokenPermissions = []string{"Metadata: read"}

// renderedSteps is the steps GitHub reports for the job of that name in the
// rendered workflow: Set up job, the job's own steps, Complete job.
func renderedSteps(workflow, name string) []string {
	steps := []string{"Set up job"}
	_, section, _ := strings.Cut(workflow, "\n  "+name+":\n")
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") {
			break // the next job
		}
		if step, ok := strings.CutPrefix(line, "      - name: "); ok {
			steps = append(steps, step)
		}
	}
	return append(steps, "Complete job")
}

// JudgeFoundationRuns judges the recorded runs against the approved source:
// at least one green run of the candidate head and one red control, each of
// this repository's foundation workflow, rendered from this exact source,
// with exactly the source's jobs and the gate, each on a hosted runner, the
// jobs with the pinned image and launcher and their fetched head the
// candidate; the head run completed green, every job and the gate with it,
// the gate having seen every job succeed, each job with its targets run, in
// order, each having discovered something, within its own budget, at a head
// whose target digests equal current, those of the judged tree
// (FoundationDigests); the red control failed in a job's checks, with failed
// assertions inside one listed target after the earlier ones passed, its
// other jobs passed or failed the same way, and its gate failed on exactly
// those results.
func JudgeFoundationRuns(source, record []byte, current map[string]string) []Finding {
	var findings []Finding
	add := func(rule, subject, format string, args ...any) {
		findings = append(findings, Finding{Rule: rule, Subject: subject, Detail: fmt.Sprintf(format, args...)})
	}
	s, err := ParseFoundationSource(source)
	if err != nil {
		add("SOURCE_SYNTAX", FoundationSourcePath, "%v", err)
		return findings
	}
	findings = append(findings, ValidateFoundationSource(s)...)
	if len(s.Targets()) == 0 {
		add("NO_TARGETS", "targets", "the workflow would run no check")
	}
	var r FoundationRuns
	if err := DecodeStrict(record, &r); err != nil {
		add("RUNS_SYNTAX", FoundationRunsPath, "%v", err)
		return findings
	}
	workflow := RenderFoundationWorkflow(s)
	want := foundationExpectation{
		source: s, workflowBlob: gitBlob([]byte(workflow)), sourceBlob: gitBlob(source),
		jobs: []string{FoundationCheck}, gateSteps: renderedSteps(workflow, FoundationCheck), digests: current,
		env: map[string]string{
			"LZ_IMAGE": RuntimeImage, "LZ_MANIFEST": s.Manifest, "LZ_LAYER": s.Layer,
			"LZ_ENTRY_SHA256": s.EntrySHA256, "LZ_BWRAP_SHA256": s.BwrapSHA256,
		},
	}
	for _, j := range s.Jobs {
		want.jobs = append(want.jobs, j.Name)
		want.targetSteps = renderedSteps(workflow, j.Name)
	}
	slices.Sort(want.jobs)

	roles := map[string]int{}
	seen := map[int64]bool{}
	for _, run := range r.Runs {
		subject := fmt.Sprintf("%s run %d", run.Role, run.RunID)
		switch {
		case seen[run.RunID]:
			add("RUN_DUPLICATE", subject, "recorded twice")
			continue
		case run.Role != RoleHead && run.Role != RoleRedControl:
			add("RUN_ROLE", subject, "role %q is neither %s nor %s", run.Role, RoleHead, RoleRedControl)
			continue
		}
		seen[run.RunID] = true
		roles[run.Role]++
		findings = append(findings, judgeRun(want, run, subject)...)
	}
	for _, role := range []string{RoleHead, RoleRedControl} {
		if roles[role] == 0 {
			add("RUN_MISSING", role, "no %s run recorded", role)
		}
	}
	return findings
}

// judgeRun judges one run of a known role.
func judgeRun(want foundationExpectation, run FoundationRun, subject string) []Finding {
	var findings []Finding
	add := func(rule, format string, args ...any) {
		findings = append(findings, Finding{Rule: rule, Subject: subject, Detail: fmt.Sprintf(format, args...)})
	}
	s := want.source
	if run.Repository != FoundationRepository || run.HeadRepository != run.Repository || run.WorkflowPath != FoundationWorkflow ||
		(run.Event != "push" && run.Event != "pull_request") {
		add("RUN_FOREIGN", "%s %s from %s on %s, want %s from %s on push or pull_request",
			run.Repository, run.WorkflowPath, run.HeadRepository, run.Event, FoundationWorkflow, FoundationRepository)
	}
	if !gitSHA.MatchString(run.Candidate) || run.HeadSHA != run.Candidate {
		add("RUN_STALE_HEAD", "candidate %q, run head %q", run.Candidate, run.HeadSHA)
	}
	// A green run is evidence for the judged tree only where everything that
	// decides each target's result is unchanged since the run's head.
	if run.Role == RoleHead && !maps.Equal(run.CheckDigests, want.digests) {
		var changed []string
		for _, id := range s.Targets() {
			if run.CheckDigests[id] != want.digests[id] {
				changed = append(changed, id)
			}
		}
		add("RUN_STALE_HEAD", "the judged tree differs from run head %s in what decides %v; %d digests recorded, %d current",
			run.HeadSHA, changed, len(run.CheckDigests), len(want.digests))
	}
	if run.WorkflowBlob != want.workflowBlob || run.SourceBlob != want.sourceBlob {
		add("RUN_STALE_SOURCE", "ran workflow %s and source %s, the approved are %s and %s",
			run.WorkflowBlob, run.SourceBlob, want.workflowBlob, want.sourceBlob)
	}
	judgeJobs(want, run, add)
	if run.Status != "completed" {
		add("RUN_INCOMPLETE", "status %q", run.Status)
		return findings
	}
	if run.Role == RoleHead {
		judgeGreen(run, s, add)
	} else {
		judgeRed(run, s, add)
	}
	return findings
}

// judgeJobs requires the run's jobs to be exactly the source's and the gate,
// the run's check run the gate's, and judges each job's identity.
func judgeJobs(want foundationExpectation, run FoundationRun, add func(string, string, ...any)) {
	names := []string{}
	for _, j := range run.Jobs {
		names = append(names, j.Name)
	}
	if !slices.Equal(names, want.jobs) {
		add("RUN_JOBS", "ran jobs %q, want %q", names, want.jobs)
	}
	if gate := run.job(FoundationCheck); gate != nil && run.CheckRunID != gate.CheckRunID {
		add("RUN_IDENTITY", "check run %d is not the gate's, %d", run.CheckRunID, gate.CheckRunID)
	}
	for _, j := range run.Jobs {
		if j.Name == FoundationCheck {
			judgeJobIdentity(run, j, jobExpectation{runner: want.source.Runner, steps: want.gateSteps, pinned: want.env, own: gateEnvKeys}, add)
			continue
		}
		i := slices.IndexFunc(want.source.Jobs, func(sj FoundationJob) bool { return sj.Name == j.Name })
		if i < 0 {
			continue // RUN_JOBS
		}
		pinned := maps.Clone(want.env)
		pinned["LZ_TARGETS"] = strings.Join(want.source.Jobs[i].Targets, " ")
		judgeJobIdentity(run, j, jobExpectation{runner: want.source.Runner, steps: want.targetSteps, pinned: pinned, own: runEnvKeys, required: runEnvKeys}, add)
		if j.FetchedHead != run.HeadSHA {
			add("RUN_STALE_HEAD", "job %s fetched %q, run head %q", j.Name, j.FetchedHead, run.HeadSHA)
		}
	}
}

// jobExpectation is what one job must show: its runner, its steps, the
// pinned variables of its main step's environment, the keys of its own it may
// add there and those it must.
type jobExpectation struct {
	runner        string
	steps         []string
	pinned        map[string]string
	own, required []string
}

// judgeJobIdentity requires a job to have run the rendered steps on the
// hosted runner with the empty token, with exactly the pinned variables and
// its own keys in the environment of its main step, the head among them the
// run's.
func judgeJobIdentity(run FoundationRun, j RunJob, want jobExpectation, add func(string, string, ...any)) {
	if names := stepNames(j.Steps); !slices.Equal(names, want.steps) {
		add("RUN_STEPS", "job %s ran steps %q, want %q", j.Name, names, want.steps)
	}
	if j.Env["LZ_HEAD"] != run.HeadSHA {
		add("RUN_STALE_HEAD", "job %s had LZ_HEAD %q, run head %q", j.Name, j.Env["LZ_HEAD"], run.HeadSHA)
	}
	judgeJobEnv(j, want, add)
	if !slices.Equal(j.RunnerLabels, []string{want.runner}) || j.RunnerGroup != hostedRunnerGroup || j.RunnerImage != want.runner {
		add("RUN_IDENTITY", "job %s ran on %v in group %q with image %q, want hosted %s", j.Name, j.RunnerLabels, j.RunnerGroup, j.RunnerImage, want.runner)
	}
	if !slices.Equal(j.Permissions, tokenPermissions) {
		add("RUN_IDENTITY", "job %s had token permissions %q, want %q", j.Name, j.Permissions, tokenPermissions)
	}
}

// judgeJobEnv requires exactly the pinned variables, with their values, and
// the job's own keys, the required ones among them, in its main step's
// environment.
func judgeJobEnv(j RunJob, want jobExpectation, add func(string, string, ...any)) {
	for _, key := range slices.Sorted(maps.Keys(j.Env)) {
		if _, pinned := want.pinned[key]; !pinned && !slices.Contains(want.own, key) {
			add("RUN_IDENTITY", "unexpected variable %s in job %s", key, j.Name)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(want.pinned)) {
		if got, ok := j.Env[key]; !ok || got != want.pinned[key] {
			add("RUN_IDENTITY", "job %s: %s was %q, want %q", j.Name, key, got, want.pinned[key])
		}
	}
	for _, key := range want.required {
		if _, ok := j.Env[key]; !ok {
			add("RUN_IDENTITY", "%s missing from job %s", key, j.Name)
		}
	}
}

func stepNames(steps []RunStep) []string {
	names := []string{}
	for _, step := range steps {
		names = append(names, step.Name)
	}
	return names
}

// outcomeRule is the rule a conclusion other than success breaks.
func outcomeRule(conclusion string) string {
	switch conclusion {
	case "success":
		return ""
	case "skipped":
		return "RUN_SKIPPED"
	case "cancelled":
		return "RUN_CANCELLED"
	}
	return "RUN_FAILED"
}

// judgeGreen requires the head run, each of its jobs and their every step to
// have succeeded, the gate to have seen every job of the source succeed, and
// each job of the source to have run its targets, in order, each having
// discovered something, within its budget.
func judgeGreen(run FoundationRun, s FoundationSource, add func(string, string, ...any)) {
	if rule := outcomeRule(run.Conclusion); rule != "" {
		add(rule, "run concluded %q", run.Conclusion)
	}
	for _, j := range run.Jobs {
		if rule := outcomeRule(j.Conclusion); rule != "" {
			add(rule, "job %s concluded %q", j.Name, j.Conclusion)
		}
		for _, step := range j.Steps {
			if rule := outcomeRule(step.Conclusion); rule != "" {
				add(rule, "job %s step %q concluded %q", j.Name, step.Name, step.Conclusion)
			}
		}
	}
	if gate := run.job(FoundationCheck); gate != nil && gate.Env["LZ_JOB_RESULTS"] != allSucceeded(s) {
		add("RUN_FAILED", "the gate saw %q, want %q", gate.Env["LZ_JOB_RESULTS"], allSucceeded(s))
	}
	for _, sj := range s.Jobs {
		if j := run.job(sj.Name); j != nil {
			judgeTargets(*j, sj, add)
			judgeBudget(*j, s, add)
		}
	}
}

// judgeTargets requires a job to have run its targets of the source, in
// order, each passed and having discovered something.
func judgeTargets(j RunJob, sj FoundationJob, add func(string, string, ...any)) {
	names := []string{}
	for _, target := range j.Targets {
		names = append(names, target.Name)
		if target.Status != "ok" {
			add("RUN_FAILED", "job %s target %s %s", j.Name, target.Name, target.Status)
		}
		if target.Discovered == 0 || target.Empty > 0 {
			add("RUN_ZERO_DISCOVERY", "job %s target %s discovered %d, ran no test in %d packages", j.Name, target.Name, target.Discovered, target.Empty)
		}
	}
	if !slices.Equal(names, sj.Targets) || j.TargetsDone != len(sj.Targets) {
		add("RUN_COUNT", "job %s ran %q and reported %d done, want %q", j.Name, names, j.TargetsDone, sj.Targets)
	}
}

// judgeBudget requires a job's targets and its time outside them, plus one
// target running into the entry's deadline, to fit the job's timeout, so
// that a suite slowing towards the timeout is refused before it times the job
// out (T055, D125). Each job is judged on its own: they run in parallel
// (D126 part A). Every target must carry a positive time and the time outside
// them may not be negative, or a missing or negative one would hide
// another's; the sum is a float so no value can wrap it.
func judgeBudget(j RunJob, s FoundationSource, add func(string, string, ...any)) {
	took := 0.0
	for _, target := range j.Targets {
		if target.Seconds <= 0 {
			add("RUN_UNTIMED", "job %s target %s took %d s", j.Name, target.Name, target.Seconds)
		}
		took += float64(target.Seconds)
	}
	if j.OutsideSeconds < 0 {
		add("RUN_UNTIMED", "job %s took %d s outside its targets", j.Name, j.OutsideSeconds)
	}
	outside := float64(j.OutsideSeconds)
	deadline, budget := FoundationChildDeadline.Seconds(), float64(s.TimeoutMinutes*60)
	if took+outside+deadline > budget {
		add("RUN_OVER_BUDGET", "job %s: targets took %.0f s and the job %.0f s outside them; with one %.0f s child deadline that is %.0f s, over the %d-minute job timeout (%.0f s)",
			j.Name, took, outside, deadline, took+outside+deadline, s.TimeoutMinutes, budget)
	}
}

// judgeRed requires the red control to have failed: in one job or more, each
// in its checks step, every earlier step having succeeded, inside one listed
// target after the targets before it passed, so that it is a behavioural red
// and not a broken fetch; every other job of the source passed all its
// targets; and the gate failed in its step on exactly the jobs' results, so
// the check named foundation went red for it.
func judgeRed(run FoundationRun, s FoundationSource, add func(string, string, ...any)) {
	if rule := outcomeRule(run.Conclusion); rule == "RUN_SKIPPED" || rule == "RUN_CANCELLED" {
		add(rule, "red control concluded %q", run.Conclusion)
		return
	}
	var why []string
	if run.Conclusion != "failure" {
		why = append(why, fmt.Sprintf("run concluded %q", run.Conclusion))
	}
	red := 0
	for _, sj := range s.Jobs {
		j := run.job(sj.Name)
		switch {
		case j == nil:
			continue // RUN_JOBS
		case redJob(*j, sj):
			red++
		case !passedJob(*j, sj):
			why = append(why, fmt.Sprintf("job %s concluded %q with targets %+v and %d done", j.Name, j.Conclusion, j.Targets, j.TargetsDone))
		}
	}
	if red == 0 {
		why = append(why, "no job failed in its checks")
	}
	results := jobResults(s, func(name string) string {
		if j := run.job(name); j != nil {
			return j.Conclusion
		}
		return ""
	})
	if gate := run.job(FoundationCheck); gate != nil &&
		(gate.Conclusion != "failure" || !stepsConcluded(gate.Steps, gateStep) || gate.Env["LZ_JOB_RESULTS"] != results) {
		why = append(why, fmt.Sprintf("the gate concluded %q on %q, want failure in its step on %q", gate.Conclusion, gate.Env["LZ_JOB_RESULTS"], results))
	}
	if len(why) > 0 {
		add("RUN_NOT_RED", "%s; want failed assertions inside one listed target of a job's checks step, and the gate failed for it", strings.Join(why, "; "))
	}
}

// stepsConcluded reports whether the step named failing failed and every
// other step succeeded.
func stepsConcluded(steps []RunStep, failing string) bool {
	for _, step := range steps {
		want := "success"
		if step.Name == failing {
			want = "failure"
		}
		if step.Conclusion != want {
			return false
		}
	}
	return true
}

// redJob reports whether a job failed in its checks step, inside one of its
// listed targets, with failed assertions, after the targets before it passed.
func redJob(j RunJob, sj FoundationJob) bool {
	n := len(j.Targets)
	red := j.Conclusion == "failure" && stepsConcluded(j.Steps, checksStep) && n > 0 && n <= len(sj.Targets) && j.TargetsDone == -1
	for i := 0; red && i < n; i++ {
		want := "ok"
		if i == n-1 {
			want = "failed"
		}
		red = j.Targets[i].Name == sj.Targets[i] && j.Targets[i].Status == want
	}
	// A build, tool or resource failure prints no failed assertion.
	return red && j.Targets[n-1].Failures > 0
}

// passedJob reports whether a job succeeded with every step and all its
// listed targets, in order.
func passedJob(j RunJob, sj FoundationJob) bool {
	names := []string{}
	for _, target := range j.Targets {
		if target.Status != "ok" {
			return false
		}
		names = append(names, target.Name)
	}
	return j.Conclusion == "success" && stepsConcluded(j.Steps, "") && slices.Equal(names, sj.Targets) && j.TargetsDone == len(sj.Targets)
}
