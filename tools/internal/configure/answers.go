// Package configure turns an answers file into a deployments.yaml (spec 004, D127).
//
// This file is the answers file (FR-001, FR-008, data-model.md *Answers file*): its Go types,
// which together with that table are its definition (no JSON Schema is published), its strict
// decoding and the reader that admits only a regular file.
//
// The answers file is YAML written in its JSON-compatible flow form, as stacks/deployments.yaml
// is, decoded strictly through checks.DecodeStrict; whole-line `#` comments are allowed. A shared
// field keeps its own copy of the pattern the manifest gives the field it maps to
// (answerRules); TestAnswersPatternsMatchManifestSchema holds each copy equal to
// schemas/deployments.schema.json, so a drifted copy cannot let a value through that the
// manifest decoder would then refuse under the manifest's path.
//
// The package opens no network connection, runs nothing and reads no environment variable
// (FR-008; TestAnswersSourceHasNoAuthority).
package configure

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/checks"
)

// The answers file's fixed header values and its size bound (FR-001).
const (
	AnswersAPIVersion = "lz.platformrelay.dev/v1alpha1"
	AnswersKind       = "DeploymentAnswers"
	MaxAnswersBytes   = 64 << 10
)

// Refusal codes of DecodeAnswers and ReadAnswers (data-model.md *Refusal message*). When a
// document breaks several rules, the first in this order is reported: size, syntax, duplicate
// keys and a number beyond float64, INVALID_VALUE at its path (in document order); then checks.DecodeStrict's walk, depth first: an object's missing
// fields (in field declaration order), then its keys in sorted order, each refused as unknown
// or null or descended into; then a JSON type mismatch (in declaration order); the header; value
// rules (keys in sorted order); a VLAN id without a CIDR.
const (
	CodeAnswersSyntax      = "ANSWERS_SYNTAX"      // not one JSON-compatible document
	CodeAnswersTooLarge    = "ANSWERS_TOO_LARGE"   // more than MaxAnswersBytes
	CodeAnswersNotRegular  = "ANSWERS_NOT_REGULAR" // a symlink, a directory, another non-regular file or missing
	CodeDuplicateKey       = "DUPLICATE_KEY"       // a key twice in one object
	CodeUnknownField       = "UNKNOWN_FIELD"       // a key the answers file does not have, or a case variant
	CodeMissingField       = "MISSING_FIELD"       // a required key absent or null
	CodeUnsupportedVersion = "UNSUPPORTED_VERSION" // apiVersion or kind other than the constants
	CodeInvalidValue       = "INVALID_VALUE"       // pattern, enum or range violated; a JSON type mismatch; vlan_id without cidr
)

// AnswersError is the error of a refused answers file; Code is one of the Code constants.
// Detail is the field path of a field error (tenants[0].environments[0].regions[0].cidr), the
// line of a syntax error (line 9), the size of an oversized document, or the file path of an
// error of the file itself.
type AnswersError struct {
	Code   string
	Detail string
}

func (e *AnswersError) Error() string { return e.Code + ": " + e.Detail }

// Answers is a decoded answers file. Defaults are not applied here: an omitted optional string
// is "", an omitted vlan_id nil, an omitted boolean false (data-model.md gives the defaults the
// derivation applies). Every object is closed and a field without omitempty is required, as
// checks.DecodeStrict reads the tags.
type Answers struct {
	APIVersion         string   `json:"apiVersion"`
	Kind               string   `json:"kind"`
	Name               string   `json:"name"`
	Org                string   `json:"org"`
	Forge              string   `json:"forge"`
	StateRegion        string   `json:"state_region"`
	StateEndpoint      string   `json:"state_endpoint,omitempty"`
	StateRef           string   `json:"state_ref,omitempty"`
	SharedStateProject bool     `json:"shared_state_project,omitempty"`
	Tenants            []Tenant `json:"tenants"`
}

// Tenant is one tenants entry.
type Tenant struct {
	Name         string        `json:"name"`
	Environments []Environment `json:"environments"`
}

// Environment is one environment of a tenant: its project and regions.
type Environment struct {
	Name        string   `json:"name"`
	Regions     []Region `json:"regions"`
	ProjectRef  string   `json:"project_ref,omitempty"`
	ProjectMode string   `json:"project_mode,omitempty"`
	BudgetAlert bool     `json:"budget_alert,omitempty"`
	QuotaGuard  bool     `json:"quota_guard,omitempty"`
}

// Region is one region of an environment; without CIDR it has no network row.
type Region struct {
	Name   string `json:"name"`
	CIDR   string `json:"cidr,omitempty"`
	VlanID *int   `json:"vlan_id,omitempty"` // nil when omitted; a scalar pointer, so DecodeStrict still refuses null
}

// fieldRule is the value rule of one answers field and the manifest field it maps to. Paths
// join object keys with "." and mark an array element with "[]". A rule holds exactly one of a
// pattern, an enum or a range.
type fieldRule struct {
	Answers  string
	Manifest string // its path in schemas/deployments.schema.json
	Pattern  string
	Enum     []string
	Range    []int // inclusive minimum and maximum of a number
}

// Copies of the manifest's patterns (tools/internal/stacks/manifest.go), held equal to
// schemas/deployments.schema.json by TestAnswersPatternsMatchManifestSchema.
const (
	metadataNamePattern  = `^[a-z][a-z0-9-]{0,62}$`
	segmentPattern       = `^[a-z][a-z0-9]{0,15}$`
	regionPattern        = `^[A-Z]+[0-9]+$`
	projectRefPattern    = `^[A-Z][A-Z0-9_]{0,62}$`
	forgePattern         = `^[a-z0-9]([a-z0-9.-]*[a-z0-9])?(/[A-Za-z0-9_-][A-Za-z0-9._-]*)+$`
	storageRegionPattern = `^[a-z]+$`
	endpointPattern      = `^https://[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`
	cidrPattern          = `^[0-9]{1,3}(\.[0-9]{1,3}){3}/[0-9]{1,2}$`
)

// answerRules holds the rule of every value of the answers file except the header constants and
// the booleans.
var answerRules = func() []fieldRule {
	env, menv := "tenants[].environments[]", "spec.tenants[].environments[]"
	return []fieldRule{
		{Answers: "name", Manifest: "metadata.name", Pattern: metadataNamePattern},
		{Answers: "org", Manifest: "spec.org", Pattern: segmentPattern},
		{Answers: "forge", Manifest: "spec.forge", Pattern: forgePattern},
		{Answers: "state_region", Manifest: "spec.state.region", Pattern: storageRegionPattern},
		{Answers: "state_endpoint", Manifest: "spec.state.endpoint", Pattern: endpointPattern},
		{Answers: "state_ref", Manifest: "spec.state.project.ref", Pattern: projectRefPattern},
		{Answers: "tenants[].name", Manifest: "spec.tenants[].name", Pattern: segmentPattern},
		{Answers: env + ".name", Manifest: menv + ".name", Pattern: segmentPattern},
		{Answers: env + ".project_ref", Manifest: menv + ".project.ref", Pattern: projectRefPattern},
		{Answers: env + ".project_mode", Manifest: menv + ".project.mode", Enum: []string{"adopt", "reference"}},
		{Answers: env + ".regions[].name", Manifest: menv + ".regions[].name", Pattern: regionPattern},
		{Answers: env + ".regions[].cidr", Manifest: menv + ".regions[].network.cidr", Pattern: cidrPattern},
		{Answers: env + ".regions[].vlan_id", Manifest: menv + ".regions[].network.vlan_id", Range: []int{0, 4000}},
	}
}()

// rulesByPath indexes answerRules by answers path, with each pattern compiled once.
var rulesByPath = func() map[string]compiledRule {
	index := map[string]compiledRule{}
	for _, r := range answerRules {
		c := compiledRule{fieldRule: r}
		if r.Pattern != "" {
			c.re = regexp.MustCompile(r.Pattern)
		}
		index[r.Answers] = c
	}
	return index
}()

type compiledRule struct {
	fieldRule
	re *regexp.Regexp
}

func refuse(code, format string, a ...any) error {
	return &AnswersError{Code: code, Detail: fmt.Sprintf(format, a...)}
}

// DecodeAnswers decodes an answers file strictly. It applies no default.
func DecodeAnswers(data []byte) (Answers, error) {
	if len(data) > MaxAnswersBytes {
		return Answers{}, refuse(CodeAnswersTooLarge, "%d bytes", len(data))
	}
	doc := stripComments(data)
	if err := (&keyWalker{data: doc, dec: json.NewDecoder(bytes.NewReader(doc))}).value(""); err != nil {
		return Answers{}, err
	}
	var a Answers
	if err := checks.DecodeStrict(doc, &a); err != nil {
		return Answers{}, decodeError(doc, err)
	}
	if a.APIVersion != AnswersAPIVersion {
		return Answers{}, refuse(CodeUnsupportedVersion, "apiVersion")
	}
	if a.Kind != AnswersKind {
		return Answers{}, refuse(CodeUnsupportedVersion, "kind")
	}
	var generic any
	if err := json.Unmarshal(doc, &generic); err != nil {
		return Answers{}, syntaxError(doc, err) // unreachable: DecodeStrict accepted the document
	}
	if err := checkValues(generic, "", ""); err != nil {
		return Answers{}, err
	}
	if err := vlanNeedsCIDR(a); err != nil {
		return Answers{}, err
	}
	return a, nil
}

// ReadAnswers reads the answers file at path: a regular file whose last path component is not a
// symlink (parent directories are the caller's), of at most MaxAnswersBytes (FR-008). It does
// not decode it.
func ReadAnswers(path string) ([]byte, error) {
	data, err := readRegular(path, MaxAnswersBytes)
	switch {
	case errors.Is(err, fs.ErrNotExist) || errors.Is(err, errNotRegular):
		return nil, refuse(CodeAnswersNotRegular, "%s", path)
	case errors.Is(err, errTooLarge):
		return nil, refuse(CodeAnswersTooLarge, "%s", path)
	case err != nil:
		return nil, fmt.Errorf("read answers file: %w", err)
	}
	return data, nil
}

// The reasons readRegular refuses a file it could open or name.
var (
	errNotRegular = errors.New("not a regular file")
	errTooLarge   = errors.New("larger than the bound")
)

// readRegular reads path when it is a regular file whose last component is not a symlink, of at
// most limit bytes: errNotRegular or errTooLarge otherwise, or the I/O error (fs.ErrNotExist for
// an absent file). ReadAnswers and ReadCurrentManifest share it (FR-008).
func readRegular(path string, limit int) ([]byte, error) {
	// O_NOFOLLOW refuses a symlink as the last component (ELOOP); O_NONBLOCK keeps a FIFO from
	// blocking the open, and the mode check below refuses it.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR) {
		return nil, errNotRegular
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errNotRegular
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, errTooLarge
	}
	return data, nil
}

// stripComments blanks whole-line `#` comments, keeping line numbers: a JSON string cannot span
// lines, so a line whose first non-blank character is `#` is never inside a value.
func stripComments(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			lines[i] = ""
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

// keyWalker finds the first duplicate key of a document with its field path, which
// checks.DecodeStrict reports by key alone, and the first syntax error with its line.
type keyWalker struct {
	data []byte
	dec  *json.Decoder
}

func (w *keyWalker) value(path string) error {
	token, err := w.dec.Token()
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) { // a number beyond float64 (1e400) in a well-formed document
		if path == "" {
			path = "(document)"
		}
		return refuse(CodeInvalidValue, "%s", path)
	}
	if err != nil {
		return syntaxError(w.data, err)
	}
	switch token {
	case json.Delim('{'):
		return w.object(path)
	case json.Delim('['):
		return w.array(path)
	}
	return nil
}

func (w *keyWalker) object(path string) error {
	seen := map[string]bool{}
	for w.dec.More() {
		token, err := w.dec.Token()
		if err != nil {
			return syntaxError(w.data, err)
		}
		key, _ := token.(string) // the decoder refuses a non-string key
		sub := joinPath(path, key)
		if seen[key] {
			return refuse(CodeDuplicateKey, "%s", sub)
		}
		seen[key] = true
		if err := w.value(sub); err != nil {
			return err
		}
	}
	return w.close()
}

func (w *keyWalker) array(path string) error {
	for i := 0; w.dec.More(); i++ {
		if err := w.value(fmt.Sprintf("%s[%d]", path, i)); err != nil {
			return err
		}
	}
	return w.close()
}

func (w *keyWalker) close() error {
	if _, err := w.dec.Token(); err != nil {
		return syntaxError(w.data, err)
	}
	return nil
}

func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// syntaxError reports a syntax error at the line of the offending byte, or at the last line
// with content when the document ends early.
func syntaxError(data []byte, err error) error {
	at := int64(len(data))
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) && syntaxErr.Offset < at {
		at = syntaxErr.Offset
	}
	line := 1 + bytes.Count(bytes.TrimRight(data[:at], " \t\r\n"), []byte("\n"))
	return refuse(CodeAnswersSyntax, "line %d", line)
}

// decodeError maps a checks.DecodeStrict error to its refusal code and field path.
func decodeError(data []byte, err error) error {
	var typeErr *json.UnmarshalTypeError
	msg := err.Error()
	field := func(suffix string) string {
		return strings.TrimPrefix(strings.TrimPrefix(strings.TrimSuffix(msg, suffix), "$"), ".")
	}
	switch {
	case errors.As(err, &typeErr):
		return refuse(CodeInvalidValue, "%s", mismatchPath(data))
	case strings.HasPrefix(msg, CodeDuplicateKey): // the keyWalker reports these first, with their path
		return refuse(CodeDuplicateKey, "%s", strings.TrimPrefix(msg, CodeDuplicateKey+": "))
	case strings.HasSuffix(msg, ": unknown field"):
		return refuse(CodeUnknownField, "%s", field(": unknown field"))
	case strings.HasSuffix(msg, ": required"):
		return refuse(CodeMissingField, "%s", field(": required"))
	case strings.HasSuffix(msg, ": null"):
		return refuse(CodeMissingField, "%s", field(": null"))
	}
	return syntaxError(data, err)
}

// typeMismatch finds the first value of a decoded document whose JSON type differs from its
// field's Go type, in field declaration order, and returns its indexed path ("" for the top
// level): json.UnmarshalTypeError names the field without the form FR-004 prints.
func typeMismatch(v any, t reflect.Type, path string) (string, bool) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		return structMismatch(v, t, path)
	case reflect.Slice:
		return sliceMismatch(v, t, path)
	case reflect.String:
		_, ok := v.(string)
		return path, !ok
	case reflect.Bool:
		_, ok := v.(bool)
		return path, !ok
	case reflect.Int:
		// the document is decoded with UseNumber, so the literal is judged as encoding/json
		// judges it for an int: 1.0, 1e0 and an overflow are type errors, as 1.5 is
		n, ok := v.(json.Number)
		if ok {
			_, err := strconv.ParseInt(string(n), 10, strconv.IntSize)
			ok = err == nil
		}
		return path, !ok
	}
	return "", false
}

func structMismatch(v any, t reflect.Type, path string) (string, bool) {
	object, ok := v.(map[string]any)
	if !ok {
		return path, true
	}
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if item, present := object[name]; present {
			if p, bad := typeMismatch(item, t.Field(i).Type, joinPath(path, name)); bad {
				return p, true
			}
		}
	}
	return "", false
}

func sliceMismatch(v any, t reflect.Type, path string) (string, bool) {
	array, ok := v.([]any)
	if !ok {
		return path, true
	}
	for i, item := range array {
		if p, bad := typeMismatch(item, t.Elem(), fmt.Sprintf("%s[%d]", path, i)); bad {
			return p, true
		}
	}
	return "", false
}

// mismatchPath is the indexed path of a type error, "(document)" for the top level.
func mismatchPath(data []byte) string {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var generic any
	if dec.Decode(&generic) != nil {
		return "(document)" // unreachable: DecodeStrict parsed the document before the type error
	}
	path, found := typeMismatch(generic, reflect.TypeFor[Answers](), "")
	if !found || path == "" { // not found: unreachable while typeMismatch knows every field kind
		return "(document)"
	}
	return path
}

// checkValues applies answerRules to every string and number of the decoded document, keys in
// sorted order. rulePath is the path with "[]" for array elements, path the one with indices.
func checkValues(v any, rulePath, path string) error {
	switch v := v.(type) {
	case map[string]any:
		return checkObject(v, rulePath, path)
	case []any:
		return checkArray(v, rulePath, path)
	case string:
		if path == "apiVersion" || path == "kind" {
			return nil
		}
		return checkString(rulesByPath[rulePath], path, v)
	case float64:
		return checkNumber(rulesByPath[rulePath], path, v)
	}
	return nil
}

func checkObject(v map[string]any, rulePath, path string) error {
	for _, k := range slices.Sorted(maps.Keys(v)) {
		if err := checkValues(v[k], joinPath(rulePath, k), joinPath(path, k)); err != nil {
			return err
		}
	}
	return nil
}

func checkArray(v []any, rulePath, path string) error {
	for i, item := range v {
		if err := checkValues(item, rulePath+"[]", fmt.Sprintf("%s[%d]", path, i)); err != nil {
			return err
		}
	}
	return nil
}

func checkString(rule compiledRule, path, v string) error {
	switch {
	case rule.Enum != nil:
		if !slices.Contains(rule.Enum, v) {
			return refuse(CodeInvalidValue, "%s", path)
		}
	case rule.re != nil:
		if !rule.re.MatchString(v) {
			return refuse(CodeInvalidValue, "%s", path)
		}
	default: // a string without a rule: none crosses the boundary unchecked
		return refuse(CodeInvalidValue, "%s", path)
	}
	return nil
}

func checkNumber(rule compiledRule, path string, v float64) error {
	if len(rule.Range) != 2 || v < float64(rule.Range[0]) || v > float64(rule.Range[1]) {
		return refuse(CodeInvalidValue, "%s", path)
	}
	return nil
}

// vlanNeedsCIDR refuses a vlan_id on a region without cidr: without a network the id would be
// dropped silently.
func vlanNeedsCIDR(a Answers) error {
	for i, t := range a.Tenants {
		for j, e := range t.Environments {
			for k, r := range e.Regions {
				if r.VlanID != nil && r.CIDR == "" {
					return refuse(CodeInvalidValue, "tenants[%d].environments[%d].regions[%d].vlan_id", i, j, k)
				}
			}
		}
	}
	return nil
}
