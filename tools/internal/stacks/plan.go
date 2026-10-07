// This file is `test:stack-plans` (005 T038; FR-007, FR-009, SC-002, contracts/checks.md
// *task test:stack-plans*, V006): every generated stack planned offline under its generated
// mocked test with fixture inputs, and the planned bucket names checked for NAME_COLLISION; and the
// change report of `stacks:generate`.
package stacks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// PlanOptions configures one offline planning run.
type PlanOptions struct {
	Root     string // project root: stacks/deployments.yaml, the generated stacks, and stages/, components/, modules/ (directories or links)
	Tofu     string // the pinned tofu binary
	Fixtures string // directory of fixture envelopes, <stage>.json (tests/fixtures/outputs/envelopes)
}

// ErrNoStacks is zero discovery in a manifest that has no rows: never a pass.
var ErrNoStacks = errors.New("no stacks: the manifest has no rows")

// TofuTCB is the pinned OpenTofu inside the offline entry.
const TofuTCB = "/tcb/tofu"

var tofuPin = regexp.MustCompile(`(?m)^opentofu\s*=\s*"([^"]+)"`)

// PinnedTofu returns the OpenTofu binary for the project at root: LZ_TOFU, else the entry's
// /tcb/tofu, else tofu on PATH. It must report exactly the version root/mise.toml pins; another
// one is refused, never skipped for the next candidate.
func PinnedTofu(root string) (string, error) {
	mise, err := os.ReadFile(filepath.Join(root, "mise.toml"))
	if err != nil {
		return "", fmt.Errorf("reading the opentofu pin: %w", err)
	}
	pin := tofuPin.FindSubmatch(mise)
	if pin == nil {
		return "", errors.New("mise.toml pins no opentofu version")
	}
	path := os.Getenv("LZ_TOFU")
	if path == "" {
		if _, err := os.Stat(TofuTCB); err == nil {
			path = TofuTCB
		} else if path, err = exec.LookPath("tofu"); err != nil {
			return "", fmt.Errorf("no tofu binary (set LZ_TOFU to the pinned %s): %v", pin[1], err)
		}
	}
	out, err := exec.Command(path, "version", "-json").Output()
	var v struct {
		Version string `json:"terraform_version"`
	}
	if err != nil || json.Unmarshal(out, &v) != nil || v.Version != string(pin[1]) {
		return "", fmt.Errorf("%s reports version %q (%v), want the pinned %s; set LZ_TOFU", path, v.Version, err, pin[1])
	}
	return path, nil
}

// offlineTest is the generated mocked test every stack is planned with (research R16).
const offlineTest = "tests/_lz_offline.tftest.hcl"

// planInputs are the fixed resolved references the offline plans use where no envelope carries
// one: the account directory of bootstrap's local backend (never read: plans run with
// -backend=false).
const offlineAccountDir = "/offline/accounts/fixture"

// PlanStacks plans every stack of Root's manifest, in manifest row order, with the pinned OpenTofu
// under the stack's generated offline test, on a scratch copy of Root's stacks/, stages/,
// components/ and modules/ (links followed), so the candidate is never written. Each stack is
// initialised mirror-only against its stage's dependency lock file (-backend=false
// -lockfile=readonly) and tested with a var file of fixture inputs (envelopeInputs). An adopted
// project is planned without its _lz_import.tf (P6: `tofu test` crashes on an import into a
// mocked resource; the import is pinned statically). The plans that passed are returned; every
// stack that did not plan is named in the error, and only when all planned are the bucket names
// checked: a collision is CheckPlannedNames' *GenerateError. No manifest is ErrNoManifest.
func PlanStacks(opts PlanOptions) ([]StackPlan, error) {
	data, err := os.ReadFile(filepath.Join(opts.Root, ManifestPath))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoManifest
	}
	if err != nil {
		return nil, err
	}
	m, err := DecodeManifest(data)
	if err != nil {
		return nil, err
	}
	if len(m.Instances) == 0 {
		return nil, ErrNoStacks
	}
	var failures []error
	for _, in := range m.Instances {
		if _, err := os.Stat(filepath.Join(opts.Root, filepath.FromSlash(in.Path), offlineTest)); err != nil {
			failures = append(failures, fmt.Errorf("%s: not generated: no %s (run stacks:generate)", in.Path, offlineTest))
		}
	}
	if len(failures) > 0 {
		return nil, errors.Join(failures...)
	}
	scratch, err := os.MkdirTemp("", "lz-stack-plans-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(scratch)
	work := filepath.Join(scratch, "root")
	// A tenant repository holds no stages, components or modules of its own: its stages/ is
	// supplied from outside it (T037 decision 1), and the components and modules the stages call
	// come from beside that stages/ when the root has none.
	stages, err := filepath.EvalSymlinks(filepath.Join(opts.Root, "stages"))
	if err != nil {
		return nil, fmt.Errorf("stages: %w", err)
	}
	if fi, err := os.Lstat(filepath.Join(opts.Root, "stacks")); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		return nil, errors.New("stacks: a link is refused: stacks/ is the candidate's own tree, never a supplied one")
	}
	for _, dir := range []string{"stacks", "stages", "components", "modules"} {
		src := filepath.Join(opts.Root, dir)
		if _, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) {
			src = filepath.Join(filepath.Dir(stages), dir)
		}
		if err := copyResolved(src, filepath.Join(work, dir), dir); err != nil {
			return nil, err
		}
	}
	home, cache := filepath.Join(scratch, "home"), filepath.Join(scratch, "plugins")
	for _, d := range []string{home, cache} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}

	type result struct {
		plan []byte
		err  error
	}
	results := make([]result, len(m.Instances))
	blocked := make([]bool, len(m.Instances))
	planOne := func(i int) {
		in := m.Instances[i]
		vars, err := envelopeInputs(m, in, opts.Fixtures)
		if err != nil {
			results[i].err = fmt.Errorf("%s: blocked: %w", in.Path, err)
			blocked[i] = true
			return
		}
		plan, err := planGeneratedStack(opts.Tofu, work, in, vars, home, cache)
		if err != nil {
			err = fmt.Errorf("%s: %w", in.Path, err)
		}
		results[i] = result{plan, err}
	}
	// The first stack that reaches tofu fills the plugin cache alone (OpenTofu does not support
	// concurrent writes to it); the rest run four at a time.
	first := 0
	for ; first < len(m.Instances); first++ {
		if planOne(first); !blocked[first] {
			break
		}
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i := first + 1; i < len(m.Instances); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			planOne(i)
		}()
	}
	wg.Wait()

	var plans []StackPlan
	for i, r := range results {
		if r.err != nil {
			failures = append(failures, r.err)
			continue
		}
		plans = append(plans, StackPlan{Stack: m.Instances[i].Path, Plan: r.plan})
	}
	if len(failures) > 0 {
		return plans, errors.Join(failures...)
	}
	return plans, CheckPlannedNames(plans)
}

// copyResolved copies the tree at src to dst, leaving OpenTofu working files out. A link at src
// itself is followed (a supplied stages/, components/ or modules/); a link, device or other
// non-regular file inside the tree is refused, named as label/<path>, so the copy never reads
// outside the tree or loops. A missing src is not an error: a tenant repository may hold no
// components or modules of its own.
func copyResolved(src, dst, label string) error {
	real, err := filepath.EvalSymlinks(src)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return filepath.WalkDir(real, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if tofuWorkingFile(d.Name(), d.IsDir()) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(real, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type()&fs.ModeSymlink != 0:
			return fmt.Errorf("%s: a link inside a copied tree is refused", filepath.ToSlash(filepath.Join(label, rel)))
		case !d.Type().IsRegular():
			return fmt.Errorf("%s: not a regular file", filepath.ToSlash(filepath.Join(label, rel)))
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// planGeneratedStack plans one stack of the scratch root under its generated offline test only
// (-filter: another test file in the stack never runs) and returns the test_plan of that file's
// run `plan`.
func planGeneratedStack(tofu, root string, in Instance, vars map[string]any, home, cache string) ([]byte, error) {
	dir := filepath.Join(root, filepath.FromSlash(in.Path))
	if err := os.Remove(filepath.Join(dir, "_lz_import.tf")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	lock := filepath.Join(dir, ".terraform.lock.hcl")
	if _, err := os.Stat(lock); errors.Is(err, fs.ErrNotExist) {
		data, err := os.ReadFile(filepath.Join(root, "stages", in.Stage, ".terraform.lock.hcl"))
		if err != nil {
			return nil, fmt.Errorf("no dependency lock file for stage %s: %w", in.Stage, err)
		}
		if err := os.WriteFile(lock, data, 0o644); err != nil {
			return nil, err
		}
	}
	varFile := filepath.Join(dir, "_lz_offline.tfvars.json")
	data, err := json.Marshal(vars)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(varFile, data, 0o600); err != nil {
		return nil, err
	}
	run := func(args ...string) ([]byte, error) {
		cmd := exec.Command(tofu, append([]string{"-chdir=" + dir}, args...)...)
		cmd.Env = tofuEnvironment(home, cache)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return out, fmt.Errorf("tofu %s: %v: %s %s", args[0], err, bytes.TrimSpace(stderr.Bytes()), tailLines(out, 5))
		}
		return out, nil
	}
	if _, err := run("init", "-backend=false", "-lockfile=readonly", "-input=false", "-no-color"); err != nil {
		return nil, err
	}
	out, err := run("test", "-json", "-verbose", "-no-color", "-filter="+offlineTest, "-var-file="+varFile)
	if err != nil {
		return nil, err
	}
	var plan []byte
	passed := false
	for _, line := range bytes.Split(out, []byte("\n")) {
		var msg struct {
			Type    string          `json:"type"`
			File    string          `json:"@testfile"`
			Run     string          `json:"@testrun"`
			Plan    json.RawMessage `json:"test_plan"`
			Summary struct {
				Status string `json:"status"`
			} `json:"test_summary"`
		}
		if json.Unmarshal(line, &msg) != nil {
			continue
		}
		switch {
		case msg.Type == "test_plan" && msg.File == offlineTest && msg.Run == "plan":
			if plan != nil {
				return nil, fmt.Errorf("more than one plan of run \"plan\" in %s", offlineTest)
			}
			plan = msg.Plan
		case msg.Type == "test_summary":
			passed = msg.Summary.Status == "pass"
		}
	}
	if plan == nil || !passed {
		return nil, fmt.Errorf("no passing plan of run \"plan\": %s", tailLines(out, 5))
	}
	return plan, nil
}

// tofuEnvironment is the planning children's whole environment: PATH, the entry's tofu CLI
// configuration (provider mirror) and proxy and certificate settings when set, a scratch HOME and
// plugin cache. Nothing else of the caller's environment reaches them, credentials included.
func tofuEnvironment(home, cache string) []string {
	env := []string{"HOME=" + home, "TF_PLUGIN_CACHE_DIR=" + cache, "TF_IN_AUTOMATION=1"}
	for _, name := range []string{"PATH", "TF_CLI_CONFIG_FILE", "HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "NO_PROXY", "no_proxy", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}
	return env
}

func tailLines(out []byte, n int) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return strings.Join(lines[max(0, len(lines)-n):], "\n")
}

// envelopeInputs are the root inputs the live lane would give one stack, from the fixture
// envelopes (contracts/checks.md *task test:stack-plans*): each data producer's published values,
// scoped to the consumer (tenant, environment and the environment's regions from the manifest),
// and the resolved references (data-model *Resolved-reference input*) from the envelope that
// publishes them: the state project from bootstrap's, the tenants' projects from project's. A
// project stack's own project id is not one of them: its generated offline test sets the id and the
// matching URN on the mocked project (stacks/_lz/offline.tm.hcl, KD-3). A producer stage
// without a fixture envelope is an error: the consumer is blocked, never given a placeholder.
func envelopeInputs(m *Manifest, in Instance, fixtures string) (map[string]any, error) {
	envelope := func(stage string) (map[string]any, error) {
		data, err := os.ReadFile(filepath.Join(fixtures, stage+".json"))
		if err != nil {
			return nil, fmt.Errorf("no fixture envelope for producer stage %s: %w", stage, err)
		}
		var doc struct {
			Stage  string         `json:"stage"`
			Values map[string]any `json:"values"`
		}
		if err := json.Unmarshal(data, &doc); err != nil || doc.Stage != stage || doc.Values == nil {
			return nil, fmt.Errorf("fixture envelope %s.json is not a %s envelope (%v)", stage, stage, err)
		}
		return doc.Values, nil
	}
	value := func(stage, name string) (string, error) {
		values, err := envelope(stage)
		if err != nil {
			return "", err
		}
		s, ok := values[name].(string)
		if !ok || s == "" {
			return "", fmt.Errorf("fixture envelope %s.json has no %s", stage, name)
		}
		return s, nil
	}
	vars := map[string]any{}
	var err error
	switch in.Stage {
	case "bootstrap":
		vars["lz_account_dir"] = offlineAccountDir
		vars["state_project_id"], err = value("bootstrap", "state_project_id")
	case "tenant-state":
		vars["state_project_id"], err = value("bootstrap", "state_project_id")
	case "account-governance":
		var id, urn string
		if id, err = value("project", "project_id"); err == nil {
			urn, err = value("project", "project_urn")
		}
		tenants := map[string]any{}
		for _, tn := range m.Tenants {
			tenants[tn.Name] = map[string]any{"project_id": id, "project_urn": urn}
		}
		vars["tenants"] = tenants
	}
	if err != nil {
		return nil, err
	}
	stageOf := map[string]string{}
	for _, r := range m.Instances {
		stageOf[r.ID] = r.Stage
	}
	for _, e := range m.External {
		stageOf[e.ID] = e.Stage
	}
	var regions []string
	for _, tn := range m.Tenants {
		for _, e := range tn.Environments {
			if tn.Name == in.Tenant && e.Name == in.Environment {
				regions = e.Regions
			}
		}
	}
	for _, e := range in.Edges {
		if e.Kind != EdgeData {
			continue
		}
		stage := stageOf[e.Producer]
		values, err := envelope(stage)
		if err != nil {
			return nil, err
		}
		scoped := maps.Clone(values)
		for name, v := range map[string]any{"tenant": in.Tenant, "environment": in.Environment, "regions": regions} {
			if _, ok := scoped[name]; ok {
				scoped[name] = v
			}
		}
		vars[strings.ReplaceAll(stage, "-", "_")] = scoped
	}
	return vars, nil
}

// GenerateReport runs Generate on opts.Root and returns, sorted, the paths (relative to the root,
// slash-separated) of the files under stacks/ it created, changed or removed, as stacks:check
// compares them.
func GenerateReport(opts GenerateOptions) ([]string, error) {
	before, err := checkedFiles(opts.Root)
	if err != nil {
		return nil, err
	}
	if err := Generate(opts); err != nil {
		return nil, err
	}
	after, err := checkedFiles(opts.Root)
	if err != nil {
		return nil, err
	}
	var changed []string
	for rel, data := range after {
		if old, ok := before[rel]; !ok || !bytes.Equal(old, data) {
			changed = append(changed, rel)
		}
	}
	for rel := range before {
		if _, ok := after[rel]; !ok {
			changed = append(changed, rel)
		}
	}
	slices.Sort(changed)
	return changed, nil
}
