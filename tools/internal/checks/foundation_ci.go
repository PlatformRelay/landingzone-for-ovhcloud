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
)

// FoundationSource is pipelines/github/foundation-source.json: the approved
// inputs of the foundation workflow. The workflow itself is rendered from it
// by RenderFoundationWorkflow, so a reviewed change to this file and to the
// renderer is the only way the workflow changes.
type FoundationSource struct {
	Runner         string            `json:"runner"`
	TimeoutMinutes int               `json:"timeout_minutes"`
	Manifest       string            `json:"manifest"`
	Layer          string            `json:"layer"`
	EntrySHA256    string            `json:"entry_sha256"`
	BwrapSHA256    string            `json:"bwrap_sha256"`
	Targets        []string          `json:"targets"`
	Deferred       map[string]string `json:"deferred"`
}

const (
	// RuntimeImage is the published runtime image, without registry.
	RuntimeImage = "platformrelay/landingzone-for-ovhcloud/runtime"
	// FoundationWorkflow and FoundationSourcePath are where both live.
	FoundationWorkflow   = ".github/workflows/foundation.yml"
	FoundationSourcePath = "pipelines/github/foundation-source.json"
	// foundationCreator is the task that creates the CI and its own checks.
	foundationCreator = "T023"
	maxTimeoutMinutes = 10
)

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

// RenderFoundationWorkflow is the only workflow the check accepts. It grants
// the token no permission, uses no Action and no secret, runs on pushes to
// and pull requests against main, fetches the exact head as data, fetches
// the runtime layer by digest, installs the pinned launcher and runs every
// target through the offline entry.
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
	line("jobs:")
	line("  foundation:")
	line("    runs-on: %s", s.Runner)
	line("    timeout-minutes: %d", s.TimeoutMinutes)
	line("    env:")
	line("      LZ_HEAD: ${{ github.event.pull_request.head.sha || github.sha }}")
	line("      LZ_IMAGE: %s", RuntimeImage)
	line("      LZ_MANIFEST: %s", s.Manifest)
	line("      LZ_LAYER: %s", s.Layer)
	line("      LZ_ENTRY_SHA256: %s", s.EntrySHA256)
	line("      LZ_BWRAP_SHA256: %s", s.BwrapSHA256)
	line("      LZ_TARGETS: %s", strings.Join(s.Targets, " "))
	line("    steps:")
	// The runner context is not available to job-level env.
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
		`token=$(curl -fsS "https://ghcr.io/token?scope=repository:$LZ_IMAGE:pull" | jq -r .token)`,
		`curl -fsSL -H "Authorization: Bearer $token" -H "Accept: application/vnd.oci.image.manifest.v1+json" -o "$RUNNER_TEMP/manifest.json" "https://ghcr.io/v2/$LZ_IMAGE/manifests/$LZ_MANIFEST"`,
		`echo "${LZ_MANIFEST#sha256:}  $RUNNER_TEMP/manifest.json" | sha256sum -c -`,
		`test "$(jq -r '.layers | length' "$RUNNER_TEMP/manifest.json")" = 1`,
		`test "$(jq -r '.layers[0].digest' "$RUNNER_TEMP/manifest.json")" = "$LZ_LAYER"`,
		`curl -fsSL -H "Authorization: Bearer $token" -o "$RUNNER_TEMP/layer.tgz" "https://ghcr.io/v2/$LZ_IMAGE/blobs/$LZ_LAYER"`,
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
	return b.String()
}

// ValidateFoundationSource checks what can be judged from the source alone:
// a hosted runner, a bounded timeout, well-formed digests, and plain target
// names. ci-workflow renders only a source without findings, so no name can
// inject into the workflow. Deferred names are not rendered, and only the
// deferrable ones are accepted.
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
	for _, id := range s.Targets {
		if AdmitTarget(id) != nil {
			add("CHECK_NAME", id, "not a plain Task target")
		}
	}
	return findings
}

// CheckFoundationCI judges the approved source and the committed workflow.
// The source must pin a hosted runner, a bounded timeout and well-formed
// digests, and account for every argument-free check of a closed task or of
// T023 itself: each runs, or is deferred with a reason. The workflow must be
// exactly the rendered one.
func CheckFoundationCI(source, workflow []byte, registry Registry, tasks []Task) []Finding {
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

	done := map[string]bool{}
	for _, t := range tasks {
		done[t.ID] = t.Done
	}
	known := map[string]bool{}
	required := map[string]bool{}
	for _, c := range registry.Checks {
		known[c.ID] = true
		if c.Command == "task "+c.ID && (done[c.Creator] || c.Creator == foundationCreator) {
			required[c.ID] = true
		}
	}
	if len(s.Targets) == 0 {
		add("NO_TARGETS", "targets", "the workflow would run no check")
	}
	seen := map[string]bool{}
	for _, id := range s.Targets {
		switch {
		case seen[id]:
			add("CHECK_DUPLICATE", id, "listed twice")
		case !known[id]:
			add("CHECK_UNKNOWN", id, "not in the check registry")
		case !required[id]:
			add("CHECK_NOT_RUNNABLE", id, "its creator is open or its command takes an argument")
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
// is the commit the run is evidence for; the rest is observed.
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
	JobConclusion  string            `json:"job_conclusion"`
	RunnerLabels   []string          `json:"runner_labels"`
	RunnerGroup    string            `json:"runner_group"`
	RunnerImage    string            `json:"runner_image"`
	Permissions    []string          `json:"permissions"`
	Steps          []RunStep         `json:"steps"`
	Env            map[string]string `json:"env"`
	FetchedHead    string            `json:"fetched_head"`
	Targets        []RunTarget       `json:"targets"`
	TargetsDone    int               `json:"targets_done"`
	CheckDigests   map[string]string `json:"check_digests"`
}

// RunStep is one step of the run's job as GitHub reported it.
type RunStep struct {
	Name       string `json:"name"`
	Conclusion string `json:"conclusion"`
}

// RunTarget is one target of the checks step, in log order: ok when the
// workflow printed LZ_TARGET_OK for it, failed when Task reported it failed.
// Discovered counts its passing package and summary lines, Empty its
// packages that ran no test, Failures the failed test assertions (`--- FAIL:`
// lines) of a failed target.
type RunTarget struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Discovered int    `json:"discovered"`
	Empty      int    `json:"empty"`
	Failures   int    `json:"failures"`
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

// ObserveFoundationRun derives the observation of one run from its capture.
// It refuses a capture that is not one completed-or-not run of exactly one
// job, a blob that is not a git SHA, or an empty log; it judges nothing.
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
	var jobs struct {
		TotalCount int `json:"total_count"`
		Jobs       []struct {
			ID          int64    `json:"id"`
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
		} `json:"jobs"`
	}
	if err := json.Unmarshal(c.Jobs, &jobs); err != nil {
		return FoundationRun{}, fmt.Errorf("CAPTURE_JOBS: %w", err)
	}
	if jobs.TotalCount != 1 || len(jobs.Jobs) != 1 {
		return FoundationRun{}, fmt.Errorf("CAPTURE_JOBS: %d jobs, want the one foundation job", jobs.TotalCount)
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
	job := jobs.Jobs[0]
	if job.RunID != run.ID || job.RunAttempt != run.Attempt || job.HeadSHA != run.HeadSHA {
		return FoundationRun{}, fmt.Errorf("CAPTURE_JOBS: job of run %d attempt %d at %s, want run %d attempt %d at %s",
			job.RunID, job.RunAttempt, job.HeadSHA, run.ID, run.Attempt, run.HeadSHA)
	}
	o := FoundationRun{
		Role: role, Candidate: candidate, RunID: run.ID, Attempt: run.Attempt, CheckRunID: job.ID,
		Repository: run.Repository.FullName, HeadRepository: run.HeadRepository.FullName,
		Event: run.Event, HeadSHA: run.HeadSHA, WorkflowPath: run.Path,
		WorkflowBlob: blobs[0], SourceBlob: blobs[1],
		Status: run.Status, Conclusion: deref(run.Conclusion), JobConclusion: deref(job.Conclusion),
		RunnerLabels: append([]string{}, job.Labels...), RunnerGroup: job.RunnerGroup,
		Permissions: []string{}, Steps: []RunStep{}, Env: map[string]string{}, Targets: []RunTarget{}, TargetsDone: -1,
		CheckDigests: maps.Clone(c.Digests),
	}
	for _, s := range job.Steps {
		o.Steps = append(o.Steps, RunStep{Name: s.Name, Conclusion: deref(s.Conclusion)})
	}
	observeLog(&o, string(c.Log))
	return o, nil
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

// observeLog reads `gh run view --log` output: job, step and a timestamped
// line, tab-separated. It takes the runner image and token permissions from
// the job set-up, the fetched head, the checks step's environment and each
// target's outcome and discovery.
func observeLog(o *FoundationRun, log string) {
	var group string
	inEnv := false
	current, failures := RunTarget{}, 0
	fetched := map[string]bool{}
	for _, raw := range strings.Split(log, "\n") {
		fields := strings.SplitN(raw, "\t", 3)
		if len(fields) != 3 {
			continue
		}
		step := fields[1]
		text := strings.TrimPrefix(fields[2], "\uFEFF")
		if _, rest, ok := strings.Cut(text, " "); ok {
			text = rest
		} else {
			text = ""
		}
		text = strings.TrimRight(text, "\r")
		switch {
		case strings.HasPrefix(text, "##[group]"):
			group = strings.TrimPrefix(text, "##[group]")
			continue
		case text == "##[endgroup]":
			group, inEnv = "", false
			continue
		}
		switch step {
		case "Set up job":
			switch {
			case group == "Runner Image" && strings.HasPrefix(text, "Image: "):
				o.RunnerImage = strings.TrimPrefix(text, "Image: ")
			case group == "GITHUB_TOKEN Permissions":
				o.Permissions = append(o.Permissions, text)
			}
		case "Fetch the exact head as data":
			if head, ok := strings.CutPrefix(text, "LZ_CANDIDATE_HEAD "); ok {
				fetched[head] = true
			}
		case checksStep:
			if group != "" {
				if text == "env:" {
					inEnv = true
				} else if name, value, ok := strings.Cut(strings.TrimPrefix(text, "  "), ": "); inEnv && ok && strings.HasPrefix(text, "  ") {
					o.Env[name] = value
				}
				continue
			}
			if strings.HasPrefix(strings.TrimLeft(text, " "), "--- FAIL: ") {
				failures++
			}
			if name, ok := strings.CutPrefix(text, "LZ_TARGET_OK "); ok {
				current.Name, current.Status = name, "ok"
				o.Targets = append(o.Targets, current)
				current, failures = RunTarget{}, 0
			} else if m := failedTask.FindStringSubmatch(text); m != nil {
				current.Name, current.Status = m[1], "failed"
				o.Targets = append(o.Targets, current)
				current = RunTarget{}
			} else if count, ok := strings.CutPrefix(text, "LZ_TARGETS_DONE "); ok {
				if n, err := strconv.Atoi(count); err == nil {
					o.TargetsDone = n
				}
			} else if strings.HasPrefix(text, "ok  \t") && strings.Contains(text, "[no tests to run]") {
				current.Empty++
			} else if strings.HasPrefix(text, "ok  \t") || summaryLine.MatchString(text) {
				current.Discovered++
			}
		}
	}
	// Go prints a failed target's assertions around Task's failure line, so
	// every one since the last passing target belongs to the failed one.
	if n := len(o.Targets); n > 0 && o.Targets[n-1].Status == "failed" {
		o.Targets[n-1].Failures = failures
	}
	// A run that fetched more than one head proves none of them.
	if len(fetched) == 1 {
		for head := range fetched {
			o.FetchedHead = head
		}
	}
}

// gitBlob is the git blob SHA of data, as GitHub reports file contents.
func gitBlob(data []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// foundationExpectation is what every run of the approved source must show.
type foundationExpectation struct {
	source       FoundationSource
	workflowBlob string
	sourceBlob   string
	steps        []string
	env          map[string]string
	digests      map[string]string
}

// runEnvKeys are the checks step's variables whose value is the run's own:
// the head, checked separately, and the work directories.
var runEnvKeys = []string{"LZ_HEAD", "LZ_CANDIDATE", "LZ_RUNTIME"}

// hostedRunnerGroup is the runner group of GitHub-hosted, ephemeral runners.
const hostedRunnerGroup = "GitHub Actions"

// tokenPermissions is what `permissions: {}` leaves the job token.
var tokenPermissions = []string{"Metadata: read"}

// JudgeFoundationRuns judges the recorded runs against the approved source:
// at least one green run of the candidate head and one red control, each of
// this repository's foundation workflow, rendered from this exact source, on
// a hosted runner with the pinned image and launcher, its fetched head the
// candidate; the head run completed green with every target run, in order,
// each having discovered something, at a head whose target digests equal
// current, those of the judged tree (FoundationDigests); the red control
// failed with failed assertions inside one listed target after the earlier
// ones passed.
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
	if len(s.Targets) == 0 {
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
		steps: []string{"Set up job"}, digests: current,
		env: map[string]string{
			"LZ_IMAGE": RuntimeImage, "LZ_MANIFEST": s.Manifest, "LZ_LAYER": s.Layer,
			"LZ_ENTRY_SHA256": s.EntrySHA256, "LZ_BWRAP_SHA256": s.BwrapSHA256, "LZ_TARGETS": strings.Join(s.Targets, " "),
		},
	}
	for _, line := range strings.Split(workflow, "\n") {
		if name, ok := strings.CutPrefix(line, "      - name: "); ok {
			want.steps = append(want.steps, name)
		}
	}
	want.steps = append(want.steps, "Complete job")

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
	if names := stepNames(run.Steps); !slices.Equal(names, want.steps) {
		add("RUN_STEPS", "ran steps %q, want %q", names, want.steps)
	}
	if !gitSHA.MatchString(run.Candidate) || run.HeadSHA != run.Candidate || run.FetchedHead != run.HeadSHA || run.Env["LZ_HEAD"] != run.HeadSHA {
		add("RUN_STALE_HEAD", "candidate %q, run head %q, fetched %q, LZ_HEAD %q", run.Candidate, run.HeadSHA, run.FetchedHead, run.Env["LZ_HEAD"])
	}
	// A green run is evidence for the judged tree only where everything that
	// decides each target's result is unchanged since the run's head.
	if run.Role == RoleHead && !maps.Equal(run.CheckDigests, want.digests) {
		var changed []string
		for _, id := range s.Targets {
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
	for _, key := range slices.Sorted(maps.Keys(run.Env)) {
		if _, pinned := want.env[key]; !pinned && !slices.Contains(runEnvKeys, key) {
			add("RUN_IDENTITY", "unexpected variable %s in the checks step", key)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(want.env)) {
		if got, ok := run.Env[key]; !ok || got != want.env[key] {
			add("RUN_IDENTITY", "%s was %q, want %q", key, got, want.env[key])
		}
	}
	for _, key := range runEnvKeys {
		if _, ok := run.Env[key]; !ok {
			add("RUN_IDENTITY", "%s missing from the checks step", key)
		}
	}
	if !slices.Equal(run.RunnerLabels, []string{s.Runner}) || run.RunnerGroup != hostedRunnerGroup || run.RunnerImage != s.Runner {
		add("RUN_IDENTITY", "runner %v in group %q with image %q, want hosted %s", run.RunnerLabels, run.RunnerGroup, run.RunnerImage, s.Runner)
	}
	if !slices.Equal(run.Permissions, tokenPermissions) {
		add("RUN_IDENTITY", "token permissions %q, want %q", run.Permissions, tokenPermissions)
	}
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

// judgeGreen requires the head run, its job and every step to have
// succeeded, and every target of the source to have run, in order, each
// having discovered something.
func judgeGreen(run FoundationRun, s FoundationSource, add func(string, string, ...any)) {
	for _, o := range [][2]string{{"run", run.Conclusion}, {"job", run.JobConclusion}} {
		if rule := outcomeRule(o[1]); rule != "" {
			add(rule, "%s concluded %q", o[0], o[1])
		}
	}
	for _, step := range run.Steps {
		if rule := outcomeRule(step.Conclusion); rule != "" {
			add(rule, "step %q concluded %q", step.Name, step.Conclusion)
		}
	}
	names := []string{}
	for _, target := range run.Targets {
		names = append(names, target.Name)
		if target.Status != "ok" {
			add("RUN_FAILED", "target %s %s", target.Name, target.Status)
		}
		if target.Discovered == 0 || target.Empty > 0 {
			add("RUN_ZERO_DISCOVERY", "target %s discovered %d, ran no test in %d packages", target.Name, target.Discovered, target.Empty)
		}
	}
	if !slices.Equal(names, s.Targets) || run.TargetsDone != len(s.Targets) {
		add("RUN_COUNT", "ran %q and reported %d done, want %q", names, run.TargetsDone, s.Targets)
	}
}

// judgeRed requires the red control to have failed in the checks step, every
// earlier step having succeeded, inside one listed target after the targets
// before it passed, so that it is a behavioural red and not a broken fetch.
func judgeRed(run FoundationRun, s FoundationSource, add func(string, string, ...any)) {
	if rule := outcomeRule(run.Conclusion); rule == "RUN_SKIPPED" || rule == "RUN_CANCELLED" {
		add(rule, "red control concluded %q", run.Conclusion)
		return
	}
	red := run.Conclusion == "failure" && run.JobConclusion == "failure"
	for _, step := range run.Steps {
		want := "success"
		if step.Name == checksStep {
			want = "failure"
		}
		red = red && step.Conclusion == want
	}
	n := len(run.Targets)
	red = red && n > 0 && n <= len(s.Targets) && run.TargetsDone == -1
	for i := 0; red && i < n; i++ {
		want := "ok"
		if i == n-1 {
			want = "failed"
		}
		red = run.Targets[i].Name == s.Targets[i] && run.Targets[i].Status == want
	}
	// A build, tool or resource failure prints no failed assertion.
	red = red && run.Targets[n-1].Failures > 0
	if !red {
		add("RUN_NOT_RED", "concluded %q (job %q) with targets %+v and %d done; want failed assertions inside one listed target of the checks step",
			run.Conclusion, run.JobConclusion, run.Targets, run.TargetsDone)
	}
}
