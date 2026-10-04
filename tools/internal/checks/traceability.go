package checks

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
)

// Task is one entry of a feature's tasks.md.
type Task struct {
	ID           string
	Paths        []string
	Requirements []string
	ADRs         []string
	// Verify holds the verify text; for a docs-only exemption it holds the
	// exemption's reason.
	Verify    string
	Exempt    bool
	Evidence  string
	Rationale map[string]string
}

const (
	KindBehavioral = "behavioral"
	KindDocs       = "docs"
)

// Requirement records the ADRs relevant to one requirement.
type Requirement struct {
	ADRs []string `json:"adrs"`
}

// CheckDefinition is one entry of the check registry. Scope selects the paths
// the check applies to; Scope and Inputs together are what its evidence must
// be fresh for.
type CheckDefinition struct {
	ID           string   `json:"id"`
	Requirements []string `json:"requirements"`
	Command      string   `json:"command"`
	Creator      string   `json:"creator"`
	Kind         string   `json:"kind"`
	Scope        []string `json:"scope"`
	Inputs       []string `json:"inputs,omitempty"`
	Review       bool     `json:"review,omitempty"`
}

// Registry is harness/checks.yaml. Evaluators are Task targets that judge
// checks rather than being checks; Producers are the evidence producers whose
// output DoD may accept as a pass.
type Registry struct {
	Requirements map[string]Requirement `json:"requirements"`
	Checks       []CheckDefinition      `json:"checks"`
	Evaluators   []string               `json:"evaluators"`
	Producers    []string               `json:"producers"`
}

// Trace is everything the trace rules read.
type Trace struct {
	Requirements []string
	Tasks        []Task
	Registry     Registry
	ADRs         []string
}

// Finding is one broken trace link.
type Finding struct {
	Rule    string
	Subject string
	Detail  string
}

// TraceRules lists every rule CheckTrace can report.
var TraceRules = []string{
	"BLANKET_ADR_LIST", "DUPLICATE_TASK", "INCOMPLETE_RATIONALE", "INVALID_EXEMPTION",
	"MISSING_EVIDENCE", "MISSING_PATHS", "MISSING_VERIFY", "UNCHECKED_REQUIREMENT",
	"UNKNOWN_ADR", "UNKNOWN_CREATOR", "UNKNOWN_REQUIREMENT", "UNMAPPED_REQUIREMENT",
	"UNMAPPED_TASK", "UNREGISTERED_REQUIREMENT", "UNRELATED_ADR",
	"MISSING_ADR", "UNKNOWN_CHECK", "CHECK_NOT_VERIFIED",
}

const (
	StatusPass           = "pass"
	StatusFail           = "fail"
	StatusBlocked        = "blocked"
	StatusNotRun         = "not-run"
	StatusReviewRequired = "review-required"
	StatusExempt         = "exempt"
)

// Evidence is the recorded observation of one check.
type Evidence struct {
	Check       string `json:"check"`
	Status      string `json:"status"`
	InputDigest string `json:"input_digest"`
	Discovered  int    `json:"discovered"`
	Failed      int    `json:"failed"`
	Reviewer    string `json:"reviewer,omitempty"`
	Producer    string `json:"producer"`
}

// Item is one check's DoD status.
type Item struct {
	Check  string
	Status string
	Reason string
}

// DoD is the definition-of-done verdict for one path.
type DoD struct {
	Items []Item
	Pass  bool
}

var (
	specRequirement = regexp.MustCompile(`(?m)^- \*\*((?:FR|SC)-\d{3})\*\*:`)
	taskLine        = regexp.MustCompile(`^- \[[ xX]\] (T\d{3})\b(.*)$`)
	taskField       = regexp.MustCompile(`^  - ([A-Z][A-Za-z ]*?):\s*(.*)$`)
	requirementID   = regexp.MustCompile(`\b(?:FR|SC)-\d{3}\b`)
	adrID           = regexp.MustCompile(`\b\d{4}\b`)
	rationaleEntry  = regexp.MustCompile(`^(\d{4})\b\s*(.*)$`)
	verifyCommand   = regexp.MustCompile("`[^`]+`")
	taskTarget      = regexp.MustCompile(`(?:^|[\s;(&|])task\s+([a-z][a-z0-9:_-]*)`)
	exemption       = regexp.MustCompile(`^exempt\s+(?:—|-{1,2})\s+docs-only\b;?\s*(.*)$`)
	// A path is a token ending in a known file extension or in "/"; prose
	// such as "trace/DoD" is neither.
	pathToken = regexp.MustCompile(`^(?:[\w.{},-]+/)*[\w.{},-]*\.(?:md|go|mod|ya?ml|json|hcl|tf|py|sh|toml|rego)$|^(?:[\w.{},-]+/)+$`)
)

// ParseSpec returns the requirement and success-criterion ids a spec defines,
// in order; mentions in prose do not define one.
func ParseSpec(text string) []string {
	var ids []string
	for _, m := range specRequirement.FindAllStringSubmatch(text, -1) {
		ids = append(ids, m[1])
	}
	return ids
}

// textPaths returns the path tokens of a title or evidence line, up to an
// em-dash annotation such as "— closed …".
func textPaths(text string) []string {
	text, _, _ = strings.Cut(text, " — ")
	var paths []string
	for _, token := range strings.Fields(text) {
		if token = strings.TrimLeft(strings.TrimRight(token, ",;:).`"), "(`"); pathToken.MatchString(token) {
			paths = append(paths, token)
		}
	}
	return paths
}

// verifyTargets returns the Task targets named inside the command spans of a
// Verify text.
func verifyTargets(verify string) []string {
	var targets []string
	for _, span := range verifyCommand.FindAllString(verify, -1) {
		for _, m := range taskTarget.FindAllStringSubmatch(strings.Trim(span, "`"), -1) {
			targets = append(targets, m[1])
		}
	}
	return targets
}

// ParseTasks reads every `- [ ] Tnnn` entry and its fields. A field given
// twice, or no task at all, is an error.
func ParseTasks(text string) ([]Task, error) {
	var tasks []Task
	current := -1
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if m := taskLine.FindStringSubmatch(line); m != nil {
			tasks = append(tasks, Task{ID: m[1], Paths: textPaths(m[2])})
			current, seen = len(tasks)-1, map[string]bool{}
			continue
		}
		if current < 0 || line == "" {
			continue
		}
		if !strings.HasPrefix(line, "  ") {
			current = -1
			continue
		}
		m := taskField.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key, value := m[1], strings.TrimSpace(m[2])
		t := &tasks[current]
		switch key {
		case "Requirements", "Verify", "Evidence", "Aggregate ADR rationale":
			if seen[key] {
				return nil, fmt.Errorf("REPEATED_FIELD: %s %s", t.ID, key)
			}
			seen[key] = true
		}
		switch key {
		case "Requirements":
			requirements, adrs, _ := strings.Cut(value, "ADRs:")
			adrs, _, _ = strings.Cut(adrs, "Depends on")
			t.Requirements = requirementID.FindAllString(requirements, -1)
			t.ADRs = adrID.FindAllString(adrs, -1)
		case "Verify":
			if e := exemption.FindStringSubmatch(value); e != nil {
				t.Exempt, t.Verify = true, strings.TrimSpace(e[1])
			} else {
				t.Verify = value
			}
		case "Evidence":
			t.Evidence = value
		case "Aggregate ADR rationale":
			t.Rationale = map[string]string{}
			for _, part := range strings.Split(value, ";") {
				part = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(part), "."))
				if e := rationaleEntry.FindStringSubmatch(part); e != nil {
					t.Rationale[e[1]] = e[2]
				} else {
					t.Rationale[part] = ""
				}
			}
		}
	}
	if len(tasks) == 0 {
		return nil, errors.New("NO_TASKS: no task entries discovered")
	}
	return tasks, nil
}

// noDuplicateKeys walks a JSON document and refuses any object that repeats a
// key; decoding into a map or struct would silently keep the last one.
func noDuplicateKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	var walk func() error
	walk = func() error {
		token, err := dec.Token()
		if err != nil {
			return err
		}
		switch token {
		case json.Delim('{'):
			keys := map[string]bool{}
			for dec.More() {
				key, err := dec.Token()
				if err != nil {
					return err
				}
				if keys[key.(string)] {
					return fmt.Errorf("DUPLICATE_KEY: %s", key)
				}
				keys[key.(string)] = true
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		case json.Delim('['):
			for dec.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		}
		return nil
	}
	return walk()
}

// conforms checks the parts of a decoded JSON document that json.Unmarshal
// would let through: object keys must equal a field's JSON name exactly
// (Unmarshal also accepts other letter cases), fields without omitempty must be
// present, and no value may be null (Unmarshal reads null as the zero value).
// Type and shape mismatches are left to Unmarshal, which rejects them.
func conforms(value any, t reflect.Type, at string) error {
	if value == nil {
		return fmt.Errorf("%s: null", at)
	}
	switch t.Kind() {
	case reflect.Struct:
		object, _ := value.(map[string]any)
		fields := map[string]reflect.Type{}
		for i := 0; i < t.NumField(); i++ {
			name, options, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
			fields[name] = t.Field(i).Type
			if _, present := object[name]; !present && !strings.Contains(options, "omitempty") {
				return fmt.Errorf("%s.%s: required", at, name)
			}
		}
		for key, item := range object {
			field, known := fields[key]
			if !known {
				return fmt.Errorf("%s.%s: unknown field", at, key)
			}
			if err := conforms(item, field, at+"."+key); err != nil {
				return err
			}
		}
	case reflect.Map:
		object, _ := value.(map[string]any)
		for key, item := range object {
			if err := conforms(item, t.Elem(), at+"."+key); err != nil {
				return err
			}
		}
	case reflect.Slice:
		array, _ := value.([]any)
		for i, item := range array {
			if err := conforms(item, t.Elem(), fmt.Sprintf("%s[%d]", at, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

// DecodeStrict decodes exactly one JSON document into v, a pointer to a
// struct, refusing duplicate or case-variant keys, unknown fields, missing
// required fields, nulls, mismatched types and trailing data.
func DecodeStrict(data []byte, v any) error {
	if err := noDuplicateKeys(data); err != nil {
		return err
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		return err
	}
	if err := conforms(document, reflect.TypeOf(v).Elem(), "$"); err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func cleanRelative(p string) bool {
	return p != "" && !path.IsAbs(p) && path.Clean(p) == p && p != "." && p != ".." && !strings.HasPrefix(p, "../")
}

// ParseRegistry decodes harness/checks.yaml. The file is YAML written in its
// JSON-compatible flow form and is decoded strictly as JSON.
func ParseRegistry(data []byte) (Registry, error) {
	var file struct {
		SchemaVersion int                    `json:"schema_version"`
		Requirements  map[string]Requirement `json:"requirements"`
		Evaluators    []string               `json:"evaluators"`
		Producers     []string               `json:"producers"`
		Checks        []CheckDefinition      `json:"checks"`
	}
	if err := DecodeStrict(data, &file); err != nil {
		return Registry{}, fmt.Errorf("REGISTRY_SYNTAX: %w", err)
	}
	if file.SchemaVersion != 1 {
		return Registry{}, fmt.Errorf("REGISTRY_VERSION: %d", file.SchemaVersion)
	}
	if len(file.Checks) == 0 {
		return Registry{}, errors.New("REGISTRY_EMPTY: no checks")
	}
	ids := map[string]bool{}
	for _, c := range file.Checks {
		if c.ID == "" || ids[c.ID] {
			return Registry{}, fmt.Errorf("REGISTRY_CHECK_ID: %q missing or repeated", c.ID)
		}
		ids[c.ID] = true
		if c.Kind != KindBehavioral && c.Kind != KindDocs {
			return Registry{}, fmt.Errorf("REGISTRY_KIND: %s: %q", c.ID, c.Kind)
		}
		if c.Kind == KindBehavioral && c.Command != "task "+c.ID && !strings.HasPrefix(c.Command, "task "+c.ID+" ") {
			return Registry{}, fmt.Errorf("REGISTRY_COMMAND: %s must run task %s", c.ID, c.ID)
		}
		if len(c.Scope) == 0 {
			return Registry{}, fmt.Errorf("REGISTRY_SCOPE: %s has no scope", c.ID)
		}
		for _, s := range append(append([]string{}, c.Scope...), c.Inputs...) {
			if !cleanRelative(s) {
				return Registry{}, fmt.Errorf("REGISTRY_SCOPE: %s: %q is not a clean relative path", c.ID, s)
			}
		}
	}
	return Registry{Requirements: file.Requirements, Checks: file.Checks, Evaluators: file.Evaluators, Producers: file.Producers}, nil
}

func set(items []string) map[string]bool {
	m := map[string]bool{}
	for _, item := range items {
		m[item] = true
	}
	return m
}

// CheckTrace follows requirement → ADR → paths → check → evidence for every
// task and requirement and reports each broken link.
func CheckTrace(t Trace) []Finding {
	var findings []Finding
	add := func(rule, subject, format string, args ...any) {
		findings = append(findings, Finding{Rule: rule, Subject: subject, Detail: fmt.Sprintf(format, args...)})
	}
	defined, knownADR := set(t.Requirements), set(t.ADRs)
	registered, evaluators := map[string]bool{}, set(t.Registry.Evaluators)
	for _, c := range t.Registry.Checks {
		registered[c.ID] = true
	}
	specWide := map[string]bool{}
	for _, r := range t.Requirements {
		for _, a := range t.Registry.Requirements[r].ADRs {
			specWide[a] = true
		}
	}

	tasks, mapped := map[string]Task{}, map[string]bool{}
	runs := map[string]map[string]bool{}
	for _, task := range t.Tasks {
		if _, seen := tasks[task.ID]; seen {
			add("DUPLICATE_TASK", task.ID, "task defined more than once")
		}
		tasks[task.ID] = task
		if len(task.Requirements) == 0 {
			add("UNMAPPED_TASK", task.ID, "task names no requirement")
		}
		listed := set(task.ADRs)
		relevant := map[string]bool{}
		for _, r := range task.Requirements {
			if !defined[r] {
				add("UNKNOWN_REQUIREMENT", task.ID, "%s is not defined by the spec", r)
				continue
			}
			mapped[r] = true
			covered := false
			for _, a := range t.Registry.Requirements[r].ADRs {
				relevant[a] = true
				covered = covered || listed[a]
			}
			if !covered && len(t.Registry.Requirements[r].ADRs) > 0 {
				add("MISSING_ADR", task.ID, "lists none of the ADRs relevant to %s", r)
			}
		}
		for _, a := range task.ADRs {
			if !knownADR[a] {
				add("UNKNOWN_ADR", task.ID, "ADR %s does not exist", a)
			} else if !relevant[a] {
				add("UNRELATED_ADR", task.ID, "ADR %s is relevant to none of %v", a, task.Requirements)
			}
		}
		blanket := len(specWide) > 1
		for a := range specWide {
			blanket = blanket && listed[a]
		}
		if blanket && len(task.Rationale) == 0 {
			add("BLANKET_ADR_LIST", task.ID, "lists every ADR of the spec without a per-ADR rationale")
		}
		if len(task.Rationale) > 0 {
			complete := len(task.Rationale) == len(listed)
			for a, why := range task.Rationale {
				complete = complete && listed[a] && strings.TrimSpace(why) != ""
			}
			if !complete {
				add("INCOMPLETE_RATIONALE", task.ID, "rationale must give a reason for exactly the listed ADRs")
			}
		}
		if len(task.Paths) == 0 {
			add("MISSING_PATHS", task.ID, "title names no path")
		}
		if task.Exempt {
			docsOnly := strings.TrimSpace(task.Verify) != ""
			for _, p := range task.Paths {
				docsOnly = docsOnly && strings.HasSuffix(p, ".md")
			}
			if !docsOnly {
				add("INVALID_EXEMPTION", task.ID, "a docs-only exemption needs a reason and Markdown paths only")
			}
		} else {
			if !verifyCommand.MatchString(task.Verify) {
				add("MISSING_VERIFY", task.ID, "Verify names no command")
			}
			runs[task.ID] = set(verifyTargets(task.Verify))
			for target := range runs[task.ID] {
				if !registered[target] && !evaluators[target] {
					add("UNKNOWN_CHECK", task.ID, "Verify runs task %s, which is neither a registered check nor an evaluator", target)
				}
			}
		}
		if len(textPaths(task.Evidence)) == 0 {
			add("MISSING_EVIDENCE", task.ID, "Evidence names no evidence path")
		}
	}

	checked := map[string]bool{}
	for _, c := range t.Registry.Checks {
		for _, r := range c.Requirements {
			if !defined[r] {
				add("UNKNOWN_REQUIREMENT", c.ID, "%s is not defined by the spec", r)
			}
			checked[r] = true
		}
		creator, ok := tasks[c.Creator]
		switch {
		case !ok:
			add("UNKNOWN_CREATOR", c.ID, "creator %s is not a task", c.Creator)
		case c.Kind == KindDocs && !creator.Exempt:
			add("INVALID_EXEMPTION", c.ID, "docs check created by %s, which is not a docs-only task", c.Creator)
		case c.Kind != KindDocs && !runs[c.Creator][c.ID]:
			add("CHECK_NOT_VERIFIED", c.ID, "creator %s's Verify never runs task %s", c.Creator, c.ID)
		}
	}
	for _, r := range t.Requirements {
		if !mapped[r] {
			add("UNMAPPED_REQUIREMENT", r, "no task implements it")
		}
		if len(t.Registry.Requirements[r].ADRs) == 0 {
			add("UNREGISTERED_REQUIREMENT", r, "the registry records no relevant ADRs")
		}
		if !checked[r] {
			add("UNCHECKED_REQUIREMENT", r, "no registered check verifies it")
		}
	}
	return findings
}

var errUnreadable = errors.New("SCOPE_UNREADABLE")

// noSymlinkComponents refuses a scope reached through a symlink.
func noSymlinkComponents(root, scope string) error {
	current := root
	for _, part := range strings.Split(scope, "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s is a symlink", errUnreadable, current)
		}
	}
	return nil
}

// walkScope visits every regular file under each scope entry with its path
// relative to root. A missing scope wraps fs.ErrNotExist; a symlink or special
// file is refused.
func walkScope(root string, scope []string, visit func(rel, file string) error) error {
	for _, s := range scope {
		if !cleanRelative(s) {
			return fmt.Errorf("%w: %q is not a clean relative path", errUnreadable, s)
		}
		if err := noSymlinkComponents(root, s); err != nil {
			return err
		}
		err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(s)), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !d.Type().IsRegular() {
				return fmt.Errorf("%w: %s is not a regular file", errUnreadable, p)
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			return visit(filepath.ToSlash(rel), p)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Digest is the SHA-256 of the relative paths and contents of every file
// under each scope entry.
func Digest(root string, scope []string) (string, error) {
	h := sha256.New()
	err := walkScope(root, scope, func(rel, file string) error {
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(h, "%s %s\n", hex.EncodeToString(sum[:]), rel)
		return nil
	})
	if err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// CheckDigest binds evidence to everything that decides a check's result: its
// registry definition, the files it applies to and its execution inputs.
func CheckDigest(root string, c CheckDefinition) (string, error) {
	files, err := Digest(root, append(append([]string{}, c.Scope...), c.Inputs...))
	if err != nil {
		return "", err
	}
	definition, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append(append(definition, '\n'), files...))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func selects(scope []string, target string) bool {
	for _, s := range scope {
		if target == "." || target == s || strings.HasPrefix(target, s+"/") || strings.HasPrefix(s, target+"/") {
			return true
		}
	}
	return false
}

func scopeStatus(err error) (string, string) {
	if errors.Is(err, fs.ErrNotExist) {
		return StatusNotRun, "SCOPE_MISSING"
	}
	return StatusFail, "SCOPE_UNREADABLE"
}

var errNotDocs = errors.New("not documentation")

// judge decides one check's status. Only evidence that names the check, is
// fresh for its definition, scope and inputs, reports a self-consistent pass
// and comes from an admitted producer counts as a pass.
func judge(reg Registry, c CheckDefinition, root string, evidence map[string]Evidence) Item {
	result := func(status, reason string) Item { return Item{Check: c.ID, Status: status, Reason: reason} }
	if c.Kind == KindDocs {
		err := walkScope(root, c.Scope, func(rel, _ string) error {
			if !strings.HasSuffix(rel, ".md") {
				return fmt.Errorf("%w: %s", errNotDocs, rel)
			}
			return nil
		})
		if errors.Is(err, errNotDocs) {
			return result(StatusFail, "INVALID_EXEMPTION")
		}
		if err != nil {
			return result(scopeStatus(err))
		}
		return result(StatusExempt, "")
	}
	e, ok := evidence[c.ID]
	if !ok {
		return result(StatusNotRun, "NO_EVIDENCE")
	}
	if e.Check != c.ID {
		return result(StatusFail, "EVIDENCE_CHECK_MISMATCH")
	}
	current, err := CheckDigest(root, c)
	if err != nil {
		return result(scopeStatus(err))
	}
	if e.InputDigest != current {
		return result(StatusNotRun, "STALE_EVIDENCE")
	}
	switch e.Status {
	case StatusPass:
		if e.Discovered <= 0 {
			return result(StatusFail, "ZERO_DISCOVERY")
		}
		if e.Failed != 0 {
			return result(StatusFail, "INCONSISTENT_PASS")
		}
		if !set(reg.Producers)[e.Producer] {
			return result(StatusReviewRequired, "UNATTESTED_EVIDENCE")
		}
		if c.Review && strings.TrimSpace(e.Reviewer) == "" {
			return result(StatusReviewRequired, "REVIEW_REQUIRED")
		}
		return result(StatusPass, "")
	case StatusFail:
		return result(StatusFail, "REPORTED_FAIL")
	case StatusBlocked:
		return result(StatusBlocked, "REPORTED_BLOCKED")
	}
	return result(StatusFail, "INVALID_STATUS")
}

// EvaluateDoD judges every registered check whose scope contains the path or
// lies under it. The DoD passes only when at least one check applies and each
// one passes or is a docs exemption.
func EvaluateDoD(reg Registry, root, target string, evidence map[string]Evidence) DoD {
	target = path.Clean(filepath.ToSlash(target))
	var d DoD
	for _, c := range reg.Checks {
		if selects(c.Scope, target) {
			d.Items = append(d.Items, judge(reg, c, root, evidence))
		}
	}
	d.Pass = len(d.Items) > 0
	for _, item := range d.Items {
		d.Pass = d.Pass && (item.Status == StatusPass || item.Status == StatusExempt)
	}
	return d
}
