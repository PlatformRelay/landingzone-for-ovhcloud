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
	"strconv"
	"strings"
)

// Task is one entry of a feature's tasks.md.
type Task struct {
	ID string
	// Done is a ticked checkbox.
	Done         bool
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
// output DoD may accept as a pass; Procedures names, with a reason, the tasks
// verified by an approved external procedure instead of a runnable check.
type Registry struct {
	Requirements map[string]Requirement `json:"requirements"`
	Checks       []CheckDefinition      `json:"checks"`
	Evaluators   []string               `json:"evaluators"`
	Producers    []string               `json:"producers"`
	Procedures   map[string]string      `json:"procedures"`
}

// Trace is everything the trace rules read.
type Trace struct {
	// Spec is the traced spec's number, the leading digits of its directory
	// name ("005" for specs/005-…). It selects the registry's keys of that
	// spec — requirements, check requirements and creators, procedures — as
	// "005/FR-001", "005/T001"; unprefixed keys are spec 001's, and an empty
	// Spec traces spec 001. Another spec's entries are neither findings nor
	// coverage.
	Spec         string
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
	"UNRELATED_CHECK", "NO_APPLICABLE_CHECK", "UNKNOWN_PROCEDURE",
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
	// A definition is "- **FR-001**: …" (spec 001) or "- **FR-001 Name** — …"
	// (spec 005).
	specRequirement = regexp.MustCompile(`(?m)^- \*\*((?:FR|SC)-\d{3})(?:\*\*:| [^*\n]+\*\* —)`)
	taskLine        = regexp.MustCompile(`^- \[([ xX])\] (T\d{3})\b(.*)$`)
	taskField       = regexp.MustCompile(`^  - ([A-Z][A-Za-z ]*?):\s*(.*)$`)
	requirementID   = regexp.MustCompile(`\b(?:FR|SC)-\d{3}\b`)
	adrID           = regexp.MustCompile(`\b\d{4}\b`)
	rationaleEntry  = regexp.MustCompile(`^(\d{4})\b\s*(.*)$`)
	verifyCommand   = regexp.MustCompile("`[^`]+`")
	targetName      = regexp.MustCompile(`^[a-z][a-z0-9:_-]*$`)
	exemption       = regexp.MustCompile(`^exempt\s+(?:—|-{1,2})\s+docs-only\b;?\s*(.*)$`)
	evidenceFile    = regexp.MustCompile("^`[\\w.{},/-]*[\\w}]\\.[A-Za-z][A-Za-z0-9]*`")
	// A path is any token with a "/" or of the form name.ext, whatever the
	// extension. Prose such as "trace/DoD" counts too, which only ever makes
	// a docs-only exemption stricter.
	pathToken = regexp.MustCompile(`^[\w.{},-]*/[\w.{},/-]*$|^[\w.{},-]*[\w}]\.[A-Za-z][A-Za-z0-9]*$`)
	// placeholder is a documentation placeholder such as <checkout>.
	placeholder = regexp.MustCompile(`<[a-z][a-z-]*>`)
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

// verifyRun is one command of a Verify text that runs a check: a Task target
// with its arguments, or a direct test command with the package directories it
// selects exactly (Packages) and the directory trees it selects (Trees).
type verifyRun struct {
	Target   string
	Args     []string
	Packages []string
	Trees    []string
}

// shellSegments splits a command span into simple commands, honouring single
// and double quotes, at unquoted ";", "&&" and "||". Any other shell syntax —
// pipes, background, redirection, substitution, globbing, comments, escapes or
// an unbalanced quote — may move where commands begin and end, so a span using
// it anywhere yields no commands at all.
func shellSegments(span string) [][]string {
	span = placeholder.ReplaceAllString(span, "PLACEHOLDER")
	var segments [][]string
	var words []string
	var word strings.Builder
	inWord, bad := false, false
	endWord := func() {
		if inWord {
			words = append(words, word.String())
			word.Reset()
			inWord = false
		}
	}
	// An empty command between separators is kept as an empty segment, which
	// is not a run and so voids the span; only a trailing ";" may end the
	// span without one.
	afterSemicolon := false
	endSegment := func(semicolon bool) {
		endWord()
		segments = append(segments, words)
		words, afterSemicolon = nil, semicolon
	}
	for i := 0; i < len(span); i++ {
		c := span[i]
		switch {
		case c == '\'' || c == '"':
			closing := strings.IndexByte(span[i+1:], c)
			if closing < 0 {
				bad, i = true, len(span)
				continue
			}
			quoted := span[i+1 : i+1+closing]
			if c == '"' && strings.ContainsAny(quoted, "$`\\") {
				bad = true
			}
			word.WriteString(quoted)
			inWord, i = true, i+1+closing
		case c == ' ' || c == '\t':
			endWord()
		case c == ';':
			endSegment(true)
		case (c == '&' || c == '|') && i+1 < len(span) && span[i+1] == c:
			endSegment(false)
			i++
		case strings.IndexByte("&|<>$`()*?[]{}\\!#~", c) >= 0:
			bad = true
		default:
			word.WriteByte(c)
			inWord = true
		}
	}
	endWord()
	if len(words) > 0 || !afterSemicolon {
		endSegment(false)
	}
	if bad {
		return nil
	}
	return segments
}

// verifyRuns returns the commands inside a Verify text's command spans that
// actually run something. A command counts only in a restricted grammar:
// `task <target> [-- args]`, optionally through the offline entry
// (`…/lz-offline --candidate <checkout> -- task …`); `go [-C dir] test <pkgs>`
// with only -run (a non-empty pattern other than ^$), -count (a positive
// integer), -v, -tags or -timeout; and `[mise exec --] tofu -chdir=<dir> test`.
// Printing (`echo task x`), dry runs, listing flags and unsupported shell syntax
// do not count.
func verifyRuns(verify string) []verifyRun {
	var runs []verifyRun
	for _, span := range verifyCommand.FindAllString(verify, -1) {
		// Every command of a span must itself be a run: any other command
		// (cd, export, true, …) may change the directory, environment or
		// control flow the runs depend on, so such a span counts for nothing.
		var spanRuns []verifyRun
		segments := shellSegments(strings.Trim(span, "`"))
		for _, words := range segments {
			if run, ok := parseRun(words); ok {
				spanRuns = append(spanRuns, run)
			}
		}
		if len(spanRuns) == len(segments) {
			runs = append(runs, spanRuns...)
		}
	}
	return runs
}

func parseRun(words []string) (verifyRun, bool) {
	if len(words) >= 4 && strings.HasSuffix(words[0], "/lz-offline") && words[1] == "--candidate" && words[3] == "--" {
		words = words[4:]
	}
	if len(words) >= 3 && words[0] == "mise" && words[1] == "exec" && words[2] == "--" {
		words = words[3:]
	}
	switch {
	case len(words) >= 2 && words[0] == "task":
		if !targetName.MatchString(words[1]) || len(words) > 2 && words[2] != "--" {
			return verifyRun{}, false
		}
		run := verifyRun{Target: words[1]}
		if len(words) > 3 {
			run.Args = words[3:]
		}
		return run, true
	case len(words) == 3 && words[0] == "tofu" && strings.HasPrefix(words[1], "-chdir=") && words[2] == "test":
		return verifyRun{Trees: []string{path.Clean(strings.TrimPrefix(words[1], "-chdir="))}}, true
	case len(words) >= 2 && words[0] == "go":
		return parseGoTest(words[1:])
	}
	return verifyRun{}, false
}

// goTestFlags lists the go test flags a verifying run may use and whether each
// takes a value.
var goTestFlags = map[string]bool{"-run": true, "-count": true, "-v": false, "-tags": true, "-timeout": true}

func parseGoTest(words []string) (verifyRun, bool) {
	base := "."
	if len(words) >= 2 && words[0] == "-C" {
		base, words = words[1], words[2:]
	}
	if len(words) == 0 || words[0] != "test" {
		return verifyRun{}, false
	}
	words = words[1:]
	var run verifyRun
	// go test reads flags, then packages, then flags again.
	words, ok := goTestFlagList(words, true)
	if !ok {
		return verifyRun{}, false
	}
	for len(words) > 0 && !strings.HasPrefix(words[0], "-") {
		// Only "." and "./…" select directories of this module; anything else
		// is an import path, which says nothing about the task's files.
		if words[0] != "." && !strings.HasPrefix(words[0], "./") {
			words = words[1:]
			continue
		}
		if tree, ok := strings.CutSuffix(words[0], "/..."); ok {
			run.Trees = append(run.Trees, path.Join(base, tree))
		} else {
			run.Packages = append(run.Packages, path.Join(base, words[0]))
		}
		words = words[1:]
	}
	if _, ok := goTestFlagList(words, false); !ok {
		return verifyRun{}, false
	}
	return run, true
}

// goTestFlagList reads verifying go test flags from the front of words and
// returns the rest. With leading set it stops at the first non-flag word (the
// packages); otherwise every word must be a flag or a flag's value. A flag
// outside goTestFlags, a missing value, an empty or "^$" -run pattern or a
// -count below one refuses the run.
func goTestFlagList(words []string, leading bool) ([]string, bool) {
	for len(words) > 0 {
		if leading && !strings.HasPrefix(words[0], "-") {
			return words, true
		}
		name, value, inline := strings.Cut(words[0], "=")
		takesValue, known := goTestFlags[name]
		if !known {
			return nil, false
		}
		words = words[1:]
		if takesValue && !inline {
			if len(words) == 0 {
				return nil, false
			}
			value, words = words[0], words[1:]
		}
		switch name {
		case "-run":
			if value == "" || value == "^$" {
				return nil, false
			}
		case "-count":
			if n, _ := strconv.Atoi(value); n < 1 {
				return nil, false
			}
		}
	}
	return words, true
}

// exercises reports whether a test run covers one of the task's paths: a file
// in a selected package's own directory, or anything under a selected tree.
func exercises(run verifyRun, paths []string) bool {
	for _, p := range paths {
		// Only a file lies in its parent package; a directory is a package of
		// its own and must be selected itself. A trailing "/" marks a
		// directory; otherwise a dotted base name is taken as a file.
		file := !strings.HasSuffix(p, "/") && strings.Contains(path.Base(p), ".")
		p = strings.TrimSuffix(p, "/")
		for _, dir := range run.Packages {
			if p == dir || file && path.Dir(p) == dir {
				return true
			}
		}
		for _, dir := range run.Trees {
			if p == dir || strings.HasPrefix(p, dir+"/") || dir == "." {
				return true
			}
		}
	}
	return false
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
			tasks = append(tasks, Task{ID: m[2], Paths: textPaths(m[3]), Done: m[1] != " "})
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
		Procedures    map[string]string      `json:"procedures"`
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
	for id, reason := range file.Procedures {
		if strings.TrimSpace(reason) == "" {
			return Registry{}, fmt.Errorf("REGISTRY_PROCEDURE: %s has no reason", id)
		}
	}
	if err := registryKeys(file.Requirements, file.Checks, file.Procedures); err != nil {
		return Registry{}, err
	}
	return Registry{Requirements: file.Requirements, Checks: file.Checks, Evaluators: file.Evaluators, Producers: file.Producers, Procedures: file.Procedures}, nil
}

var (
	requirementKey = regexp.MustCompile(`^(?:\d{3}/)?(?:FR|SC)-\d{3}$`)
	taskKey        = regexp.MustCompile(`^(?:\d{3}/)?T\d{3}$`)
)

// registryKeys refuses a requirement, creator or procedure key that no spec
// owns: a malformed id, or a "001/" prefix (spec 001's keys are unprefixed).
// Such a key would be neither a finding nor coverage in any trace.
func registryKeys(requirements map[string]Requirement, checks []CheckDefinition, procedures map[string]string) error {
	bad := func(key string, form *regexp.Regexp) bool {
		return !form.MatchString(key) || strings.HasPrefix(key, "001/")
	}
	for key := range requirements {
		if bad(key, requirementKey) {
			return fmt.Errorf("REGISTRY_KEY: requirement %q", key)
		}
	}
	for _, c := range checks {
		for _, r := range c.Requirements {
			if bad(r, requirementKey) {
				return fmt.Errorf("REGISTRY_KEY: %s requirement %q", c.ID, r)
			}
		}
		if bad(c.Creator, taskKey) {
			return fmt.Errorf("REGISTRY_KEY: %s creator %q", c.ID, c.Creator)
		}
	}
	for key := range procedures {
		if bad(key, taskKey) {
			return fmt.Errorf("REGISTRY_KEY: procedure %q", key)
		}
	}
	return nil
}

// ownKey returns a registry key's bare id when the key belongs to spec: spec
// 001 (or an empty spec) owns the unprefixed keys, any other spec the keys
// prefixed with its number and "/".
func ownKey(spec, key string) (string, bool) {
	if spec == "" || spec == "001" {
		return key, !strings.Contains(key, "/")
	}
	return strings.CutPrefix(key, spec+"/")
}

// scopedCheck is a registry check as one spec sees it: only that spec's
// requirements, by bare id, and its creator's bare id when the spec creates it
// (own).
type scopedCheck struct {
	CheckDefinition
	own bool
}

// scope returns the registry as spec sees it: its own requirements and
// procedures under bare keys, and every check with only its own requirements.
// Another spec's check stays registered, so running it is UNRELATED_CHECK
// rather than UNKNOWN_CHECK.
func scope(spec string, reg Registry) (Registry, []scopedCheck) {
	scoped := Registry{Requirements: map[string]Requirement{}, Procedures: map[string]string{}, Evaluators: reg.Evaluators, Producers: reg.Producers}
	for key, r := range reg.Requirements {
		if id, ok := ownKey(spec, key); ok {
			scoped.Requirements[id] = r
		}
	}
	for key, reason := range reg.Procedures {
		if id, ok := ownKey(spec, key); ok {
			scoped.Procedures[id] = reason
		}
	}
	var checks []scopedCheck
	for _, c := range reg.Checks {
		s := scopedCheck{CheckDefinition: c}
		s.Requirements = nil
		for _, r := range c.Requirements {
			if id, ok := ownKey(spec, r); ok {
				s.Requirements = append(s.Requirements, id)
			}
		}
		if s.Creator, s.own = ownKey(spec, c.Creator); !s.own {
			s.Creator = ""
		}
		scoped.Checks = append(scoped.Checks, s.CheckDefinition)
		checks = append(checks, s)
	}
	return scoped, checks
}

// SpecChecks counts the checks that belong to spec: those it creates or that
// check one of its requirements.
func SpecChecks(spec string, reg Registry) int {
	_, checks := scope(spec, reg)
	n := 0
	for _, c := range checks {
		if c.own || len(c.Requirements) > 0 {
			n++
		}
	}
	return n
}

// evaluates reports whether an evaluator run over args (every check when there
// are none, else the checks whose scope covers the first argument) judges a
// check of one of the given requirements.
func evaluates(checks []CheckDefinition, args []string, requirements []string) bool {
	for _, c := range checks {
		if (len(args) == 0 || selects(c.Scope, path.Clean(args[0]))) && shares(c.Requirements, requirements) {
			return true
		}
	}
	return false
}

func shares(a, b []string) bool {
	for _, item := range a {
		if set(b)[item] {
			return true
		}
	}
	return false
}

func set(items []string) map[string]bool {
	m := map[string]bool{}
	for _, item := range items {
		m[item] = true
	}
	return m
}

// CheckTrace follows requirement → ADR → paths → check → evidence for every
// task and requirement of the traced spec and reports each broken link. The
// registry is read through the spec's keys (Trace.Spec): another spec's
// requirements, check requirements, creators and procedures are neither
// findings nor coverage.
func CheckTrace(t Trace) []Finding {
	var scopedChecks []scopedCheck
	t.Registry, scopedChecks = scope(t.Spec, t.Registry)
	var findings []Finding
	add := func(rule, subject, format string, args ...any) {
		findings = append(findings, Finding{Rule: rule, Subject: subject, Detail: fmt.Sprintf(format, args...)})
	}
	defined, knownADR := set(t.Requirements), set(t.ADRs)
	registered, evaluators := map[string]bool{}, set(t.Registry.Evaluators)
	checkRequirements := map[string][]string{}
	for _, c := range t.Registry.Checks {
		registered[c.ID] = true
		checkRequirements[c.ID] = c.Requirements
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
		} else if !verifyCommand.MatchString(task.Verify) {
			add("MISSING_VERIFY", task.ID, "Verify names no command")
		} else {
			targets, applicable := map[string]bool{}, false
			for _, run := range verifyRuns(task.Verify) {
				if run.Target == "" {
					applicable = applicable || exercises(run, task.Paths)
					continue
				}
				targets[run.Target] = true
				switch {
				case evaluators[run.Target]:
					applicable = applicable || evaluates(t.Registry.Checks, run.Args, task.Requirements)
				case !registered[run.Target]:
					add("UNKNOWN_CHECK", task.ID, "Verify runs task %s, which is neither a registered check nor an evaluator", run.Target)
				case shares(checkRequirements[run.Target], task.Requirements):
					applicable = true
				default:
					add("UNRELATED_CHECK", task.ID, "Verify runs task %s, which checks none of %v", run.Target, task.Requirements)
				}
			}
			runs[task.ID] = targets
			if !applicable && t.Registry.Procedures[task.ID] == "" {
				add("NO_APPLICABLE_CHECK", task.ID, "Verify runs no check or test of the task's own requirements or paths")
			}
		}
		if !evidenceFile.MatchString(task.Evidence) {
			add("MISSING_EVIDENCE", task.ID, "Evidence does not start with a quoted evidence file")
		}
	}

	checked := map[string]bool{}
	for _, c := range scopedChecks {
		for _, r := range c.Requirements {
			if !defined[r] {
				add("UNKNOWN_REQUIREMENT", c.ID, "%s is not defined by the spec", r)
			}
			checked[r] = true
		}
		if !c.own {
			continue
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
	for id := range t.Registry.Procedures {
		if _, ok := tasks[id]; !ok {
			add("UNKNOWN_PROCEDURE", id, "procedure registered for a task that does not exist")
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
