package checks

import (
	"fmt"
	"regexp"
	"slices"
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
	line("      LZ_CANDIDATE: ${{ runner.temp }}/candidate")
	line("      LZ_RUNTIME: ${{ runner.temp }}/runtime")
	line("      LZ_IMAGE: %s", RuntimeImage)
	line("      LZ_LAYER: %s", s.Layer)
	line("      LZ_ENTRY_SHA256: %s", s.EntrySHA256)
	line("      LZ_BWRAP_SHA256: %s", s.BwrapSHA256)
	line("      LZ_TARGETS: %s", strings.Join(s.Targets, " "))
	line("    steps:")
	script("Fetch the exact head as data",
		`git init -q "$LZ_CANDIDATE"`,
		`git -C "$LZ_CANDIDATE" fetch -q --depth=1 --no-tags "$GITHUB_SERVER_URL/$GITHUB_REPOSITORY" "$LZ_HEAD"`,
		`git -c core.hooksPath=/dev/null -c filter.lfs.process= -c filter.lfs.smudge= -c filter.lfs.required=false -C "$LZ_CANDIDATE" checkout -q --detach FETCH_HEAD`,
		`test "$(git -C "$LZ_CANDIDATE" rev-parse HEAD)" = "$LZ_HEAD"`,
		`echo "LZ_CANDIDATE_HEAD $LZ_HEAD"`)
	script("Fetch the runtime layer by digest",
		`token=$(curl -fsS "https://ghcr.io/token?scope=repository:$LZ_IMAGE:pull" | jq -r .token)`,
		`curl -fsSL -H "Authorization: Bearer $token" -o "$RUNNER_TEMP/layer.tgz" "https://ghcr.io/v2/$LZ_IMAGE/blobs/$LZ_LAYER"`,
		`echo "${LZ_LAYER#sha256:}  $RUNNER_TEMP/layer.tgz" | sha256sum -c -`,
		`mkdir "$LZ_RUNTIME"`,
		`tar -xzpf "$RUNNER_TEMP/layer.tgz" -C "$LZ_RUNTIME"`,
		`echo "$LZ_ENTRY_SHA256  $LZ_RUNTIME/runtime/lz-offline" | sha256sum -c -`,
		`echo "$LZ_BWRAP_SHA256  $LZ_RUNTIME/host/bwrap" | sha256sum -c -`)
	script("Install the pinned launcher",
		`sudo install -m 0755 "$LZ_RUNTIME/host/bwrap" /usr/bin/bwrap`,
		`if [ -e /proc/sys/kernel/apparmor_restrict_unprivileged_userns ]; then sudo sysctl -q -w kernel.apparmor_restrict_unprivileged_userns=0; fi`)
	script("Run the foundation checks inside the offline entry",
		`for target in $LZ_TARGETS; do`,
		`  "$LZ_RUNTIME/runtime/lz-offline" --candidate "$LZ_CANDIDATE" -- task "$target"`,
		`  echo "LZ_TARGET_OK $target"`,
		`done`,
		`echo "LZ_TARGETS_DONE $(wc -w <<<"$LZ_TARGETS")"`)
	return b.String()
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
