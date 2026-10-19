package checks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// The update lane (006 FR-015, research R8): a Renovate configuration in
// .github/renovate.json that watches every pin source, never auto-merges,
// keeps the tools the runtime mirrors in one group labelled
// needs-entry-revision, and keeps provider bumps in one group with the schema
// snapshot, whose version must equal the lock pin. The check reads the
// configuration and the pin files; it does not run Renovate and never judges
// manager names. Its reading of Renovate is fail-closed: a key it cannot
// attribute to a pin source (a preset, a manager allow-list or file pattern,
// a manager or update type switched off, an ignored dependency) is a finding,
// not a pass, and a file pattern it cannot evaluate counts as matching where
// a match would take updates away.
//
// "The provider group holds the snapshot" is a naming rule: the snapshot is no
// dependency file, and ignorePaths may drop it (the committed configuration
// does, with every fixtures/ tree), so Renovate never edits it. What stops a
// provider bump without a re-captured snapshot is SCHEMA_VERSION on the bump's
// pull request, once test:update-lane runs in CI.
const (
	updateConfigPath  = ".github/renovate.json"
	runtimePinFile    = "mise.toml"
	runtimeLabel      = "needs-entry-revision"
	providerSchemaDir = "tests/fixtures/provider-schema"
)

// updatePinTrees hold the infrastructure roots whose lock files and
// required_providers blocks pin the provider. Fixture trees inside them and
// hidden directories (.terraform) are never pin sources; tests/ is not walked.
var updatePinTrees = []string{"components", "modules", "stacks", "stages"}

// updateOtherConfigs are the other file names Renovate reads its
// configuration from (believed, UNVERIFIED: research R8 reads no Renovate
// source); one of them may be read instead of .github/renovate.json, so the
// check would judge a configuration Renovate does not use.
var updateOtherConfigs = []string{
	"renovate.json", "renovate.json5", ".github/renovate.json5", ".gitlab/renovate.json", ".gitlab/renovate.json5",
	".renovaterc", ".renovaterc.json", ".renovaterc.json5",
}

// updateNarrowingKeys drop updates the check cannot attribute to a pin source.
var updateNarrowingKeys = []string{"enabledManagers", "ignoreDeps", "includePaths"}

var (
	// imagePin is the base image reference of the runtime, before its digest.
	imagePin = regexp.MustCompile(`(?m)^\s*LZ_OFFLINE_IMAGE\s*=\s*"([^"@]+)@`)
	// lockProvider is one provider block of a lock file.
	lockProvider = regexp.MustCompile(`(?ms)^provider\s+"([^"]+)"\s*\{(.*?)^\}`)
	lockVersion  = regexp.MustCompile(`(?m)^\s*version\s*=\s*"([^"]+)"`)
	snapshotName = regexp.MustCompile(`^ovh-(\d+\.\d+\.\d+)\.json$`)
	// packageRuleAt is the path of one packageRules entry, whose own enabled
	// key judgeCoverage reads; anything nested deeper is walked.
	packageRuleAt = regexp.MustCompile(`^/packageRules/[0-9]+$`)
	// globSpecial marks a pattern this reading cannot evaluate (brace sets,
	// classes, extglobs, negation).
	globSpecial = regexp.MustCompile(`[\[\]{}()!+@]`)
)

// updatePins are the pin sources of data-model *Update-lane configuration*
// found under a root.
type updatePins struct {
	files    []string            // every pin source, slash-separated and relative to the root
	provider []string            // lock files and files with a required_providers block
	image    string              // LZ_OFFLINE_IMAGE's reference, "" when mise.toml has none
	runtime  string              // mise.toml's text
	versions map[string][]string // ovh/ovh lock version → lock files pinning it
}

// updateRule is one packageRules entry as far as the check reads it.
type updateRule struct {
	index      int
	files      []string // matchFileNames
	deps       []string // matchDepNames, matchPackageNames
	restricted bool     // any other match* or exclude* key
	group      *string
	labels     []string
	hasLabels  bool
	enabled    *bool
}

type updateManager struct {
	FilePatterns []string `json:"managerFilePatterns"`
	MatchStrings []string `json:"matchStrings"`
}

type updateConfig struct {
	Extends        []string                     `json:"extends"`
	IgnorePaths    []string                     `json:"ignorePaths"`
	CustomManagers []updateManager              `json:"customManagers"`
	PackageRules   []map[string]json.RawMessage `json:"packageRules"`
	raw            map[string]any
	rules          []updateRule
}

// CheckUpdateLane judges the dependency-update configuration under root (.github/renovate.json)
// against the repository's pin sources and its provider schema snapshot (006 FR-015, SC-004) and
// returns one finding per breach, with the rules UPDATE_UNCOVERED, UPDATE_AUTOMERGE,
// UPDATE_RUNTIME_GROUP, UPDATE_PROVIDER_GROUP, SCHEMA_VERSION, SCHEMA_RECORD and UPDATE_SYNTAX.
// The pin sources are those of data-model *Update-lane configuration*; fixture trees (anything
// under tests/ or a fixtures/ directory, tests/check/fixtures/updatelane and
// tools/cmd/lz-offline/fixtures among them) are test data, never pin sources.
func CheckUpdateLane(root string) []Finding {
	pins, findings := readUpdatePins(root)
	findings = append(findings, otherUpdateConfigs(root)...)
	cfg, cfgFindings := readUpdateConfig(root)
	findings = append(findings, cfgFindings...)
	if cfg != nil {
		findings = append(findings, judgeUpdateConfig(cfg, pins)...)
	}
	return append(findings, judgeSchemaSnapshots(root, pins)...)
}

// otherUpdateConfigs reports every other Renovate configuration file under root.
func otherUpdateConfigs(root string) []Finding {
	var findings []Finding
	for _, rel := range updateOtherConfigs {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel))); !errors.Is(err, fs.ErrNotExist) {
			findings = append(findings, updateFinding("UPDATE_UNCOVERED", rel,
				"a second Renovate configuration, which Renovate may read instead of %s; the check judges only that one", updateConfigPath))
		}
	}
	return findings
}

func updateFinding(rule, subject, format string, args ...any) Finding {
	return Finding{Rule: rule, Subject: subject, Detail: fmt.Sprintf(format, args...)}
}

// readUpdatePins finds the pin sources under root. An unreadable source is
// reported as unwatched rather than skipped.
func readUpdatePins(root string) (updatePins, []Finding) {
	pins := updatePins{versions: map[string][]string{}}
	var findings []Finding
	for _, rel := range []string{runtimePinFile, "mise.live.toml", "tools/go.mod"} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			findings = append(findings, updateFinding("UPDATE_UNCOVERED", rel, "pin source unreadable: %v", err))
			continue
		}
		pins.files = append(pins.files, rel)
		if rel == runtimePinFile {
			pins.runtime = string(data)
			if m := imagePin.FindSubmatch(data); m != nil {
				pins.image = string(m[1])
			}
		}
	}
	for _, tree := range updatePinTrees {
		if err := walkPinTree(root, tree, &pins); err != nil {
			findings = append(findings, updateFinding("UPDATE_UNCOVERED", tree, "pin tree unreadable: %v", err))
		}
	}
	return pins, findings
}

// walkPinTree adds the lock files and required_providers files under tree.
// A tree that does not exist holds no pin; any other error, a dangling link
// inside the tree among them, is the caller's finding.
func walkPinTree(root, tree string, pins *updatePins) error {
	dir := filepath.Join(root, tree)
	if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return pins.visit(root, p, d)
	})
}

// visit skips hidden and fixture directories and reads candidate pin files.
func (pins *updatePins) visit(root, p string, d fs.DirEntry) error {
	if d.IsDir() {
		if strings.HasPrefix(d.Name(), ".") || d.Name() == "fixtures" {
			return fs.SkipDir
		}
		return nil
	}
	if d.Name() != ".terraform.lock.hcl" && !providerSource(d.Name()) {
		return nil
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	pins.addProviderFile(filepath.ToSlash(rel), d.Name(), data)
	return nil
}

// providerSource says whether name is an OpenTofu configuration file that
// can hold a required_providers block.
func providerSource(name string) bool {
	return strings.HasSuffix(name, ".tf") || strings.HasSuffix(name, ".tofu") ||
		strings.HasSuffix(name, ".tf.json") || strings.HasSuffix(name, ".tofu.json")
}

func (pins *updatePins) addProviderFile(rel, name string, data []byte) {
	if name != ".terraform.lock.hcl" {
		if strings.Contains(string(data), "required_providers") {
			pins.files = append(pins.files, rel)
			pins.provider = append(pins.provider, rel)
		}
		return
	}
	pins.files = append(pins.files, rel)
	pins.provider = append(pins.provider, rel)
	for _, m := range lockProvider.FindAllSubmatch(data, -1) {
		if !strings.HasSuffix(string(m[1]), "/ovh/ovh") {
			continue
		}
		if v := lockVersion.FindSubmatch(m[2]); v != nil {
			pins.versions[string(v[1])] = append(pins.versions[string(v[1])], rel)
		}
	}
}

// readUpdateConfig reads the configuration as a JSON object of the shape the
// check judges. A missing configuration leaves every pin source unwatched.
func readUpdateConfig(root string) (*updateConfig, []Finding) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(updateConfigPath)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, []Finding{updateFinding("UPDATE_UNCOVERED", updateConfigPath, "no update-lane configuration: every pin source is unwatched")}
	}
	if err != nil {
		return nil, []Finding{updateFinding("UPDATE_SYNTAX", updateConfigPath, "unreadable: %v", err)}
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, []Finding{updateFinding("UPDATE_SYNTAX", updateConfigPath, "not JSON: %v", err)}
	}
	object, ok := raw.(map[string]any)
	if !ok {
		return nil, []Finding{updateFinding("UPDATE_SYNTAX", updateConfigPath, "not a JSON object")}
	}
	cfg := &updateConfig{raw: object}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, []Finding{updateFinding("UPDATE_SYNTAX", updateConfigPath, "unexpected shape: %v", err)}
	}
	for i, fields := range cfg.PackageRules {
		rule, err := parseUpdateRule(i, fields)
		if err != nil {
			return nil, []Finding{updateFinding("UPDATE_SYNTAX", updateConfigPath, "packageRules[%d]: %v", i, err)}
		}
		cfg.rules = append(cfg.rules, rule)
	}
	return cfg, nil
}

// parseUpdateRule reads the keys the check judges; any other match* or
// exclude* key narrows the rule to some dependencies.
func parseUpdateRule(index int, fields map[string]json.RawMessage) (updateRule, error) {
	rule := updateRule{index: index}
	for _, key := range slices.Sorted(maps.Keys(fields)) {
		value := fields[key]
		var err error
		switch key {
		case "matchFileNames":
			err = json.Unmarshal(value, &rule.files)
		case "matchDepNames", "matchPackageNames":
			var names []string
			err = json.Unmarshal(value, &names)
			rule.deps = append(rule.deps, names...)
		case "groupName":
			err = json.Unmarshal(value, &rule.group)
		case "labels":
			rule.hasLabels = true
			err = json.Unmarshal(value, &rule.labels)
		case "enabled":
			err = json.Unmarshal(value, &rule.enabled)
		default:
			rule.restricted = rule.restricted || strings.HasPrefix(key, "match") || strings.HasPrefix(key, "exclude")
		}
		if err != nil {
			return rule, fmt.Errorf("%s: %v", key, err)
		}
	}
	return rule, nil
}

// narrowed says whether the rule applies to some dependencies of a file only.
func (r updateRule) narrowed() bool { return r.restricted || len(r.deps) > 0 }

// appliesTo says whether the rule can apply to a dependency of file.
func (r updateRule) appliesTo(file string) bool {
	if len(r.files) == 0 {
		return true
	}
	for _, g := range r.files {
		if globMatch(g, file) {
			return true
		}
	}
	return false
}

// mayApply says whether the rule could apply to file: a pattern this reading
// cannot evaluate counts as matching, so a rule taking updates away fails
// closed.
func (r updateRule) mayApply(file string) bool {
	return r.appliesTo(file) || slices.ContainsFunc(r.files, unreadablePattern)
}

// matchesFile says whether the rule names file in matchFileNames.
func (r updateRule) matchesFile(file string) bool { return len(r.files) > 0 && r.appliesTo(file) }

func judgeUpdateConfig(cfg *updateConfig, pins updatePins) []Finding {
	findings := judgeConfigKeys(cfg)
	for _, file := range pins.files {
		findings = append(findings, judgeCoverage(cfg, file)...)
	}
	if pins.image != "" && !imageManaged(cfg, pins) {
		findings = append(findings, updateFinding("UPDATE_UNCOVERED", runtimePinFile,
			"no customManagers entry reads LZ_OFFLINE_IMAGE (%s) from %s", pins.image, runtimePinFile))
	}
	findings = append(findings, judgeRuntimeGroup(cfg, pins)...)
	return append(findings, judgeProviderGroup(cfg, pins)...)
}

// judgeConfigKeys walks the whole configuration in key order (judgeConfigKey),
// then reports any preset, whose settings (automerge among them) are not read
// offline, and the top-level keys that drop updates.
func judgeConfigKeys(cfg *updateConfig) []Finding {
	var findings []Finding
	walkConfig(cfg.raw, "", func(at, key string, value any) {
		if f, ok := judgeConfigKey(at, key, value); ok {
			findings = append(findings, f)
		}
	})
	if len(cfg.Extends) > 0 {
		findings = append(findings, updateFinding("UPDATE_AUTOMERGE", updateConfigPath+":/extends",
			"extends presets %v whose settings the check cannot read; a preset may auto-merge", cfg.Extends))
	}
	for _, key := range updateNarrowingKeys {
		if v, ok := cfg.raw[key]; ok && v != nil {
			findings = append(findings, updateFinding("UPDATE_UNCOVERED", updateConfigPath+":/"+key,
				"drops updates the check cannot attribute to a pin source"))
		}
	}
	return findings
}

// judgeConfigKey reports automerge set to anything but false; enabled set to
// anything but true outside packageRules (the configuration, a manager or an
// update type switched off; packageRules are judged per pin source); and file
// patterns outside customManagers (a manager told to read other files).
func judgeConfigKey(at, key string, value any) (Finding, bool) {
	subject := updateConfigPath + ":" + at + "/" + key
	switch {
	case key == "automerge" && value != false:
		return updateFinding("UPDATE_AUTOMERGE", subject, "auto-merges updates; every bump needs a review"), true
	case key == "enabled" && value != true && !packageRuleAt.MatchString(at):
		return updateFinding("UPDATE_UNCOVERED", subject, "switches updates off"), true
	case (key == "managerFilePatterns" || key == "fileMatch") && !strings.HasPrefix(at, "/customManagers/"):
		return updateFinding("UPDATE_UNCOVERED", subject, "redefines the files a manager reads; manager names are not judged (R8)"), true
	}
	return Finding{}, false
}

// walkConfig calls visit for every key of every object, in key order.
func walkConfig(value any, at string, visit func(at, key string, child any)) {
	switch v := value.(type) {
	case map[string]any:
		for _, key := range slices.Sorted(maps.Keys(v)) {
			visit(at, key, v[key])
			walkConfig(v[key], at+"/"+key, visit)
		}
	case []any:
		for i, child := range v {
			walkConfig(child, fmt.Sprintf("%s/%d", at, i), visit)
		}
	}
}

// judgeCoverage reports a pin source no group rule names in matchFileNames,
// one an ignorePaths entry drops, and one a rule disables.
func judgeCoverage(cfg *updateConfig, file string) []Finding {
	what := "pin source"
	if providerSource(path.Base(file)) {
		what = "required_providers block"
	}
	for _, pattern := range cfg.IgnorePaths {
		if ignoredBy(pattern, file) {
			return []Finding{updateFinding("UPDATE_UNCOVERED", file, "%s dropped by ignorePaths entry %q", what, pattern)}
		}
	}
	for _, r := range cfg.rules {
		if r.mayApply(file) && r.enabled != nil && !*r.enabled {
			return []Finding{updateFinding("UPDATE_UNCOVERED", file, "%s disabled by packageRules[%d]", what, r.index)}
		}
	}
	for _, r := range cfg.rules {
		if r.group != nil && *r.group != "" && !r.narrowed() && r.matchesFile(file) {
			return nil
		}
	}
	return []Finding{updateFinding("UPDATE_UNCOVERED", file, "%s: no packageRules group names it in matchFileNames", what)}
}

// unreadablePattern says whether this glob reading cannot evaluate pattern:
// brace sets, classes, extglobs, negation, or a /regex/.
func unreadablePattern(pattern string) bool {
	return globSpecial.MatchString(pattern) || (len(pattern) > 1 && strings.HasPrefix(pattern, "/") && strings.HasSuffix(pattern, "/"))
}

// ignoredBy reads an ignorePaths entry as Renovate is believed to (a
// substring or a glob, research R8: UNVERIFIED); a pattern this reading
// cannot evaluate counts as ignoring the file.
func ignoredBy(pattern, file string) bool {
	return unreadablePattern(pattern) || strings.Contains(file, pattern) || globMatch(pattern, file)
}

// imageManaged says whether a custom manager reads mise.toml and one of its
// match strings, compiled, matches mise.toml with depName the image reference.
func imageManaged(cfg *updateConfig, pins updatePins) bool {
	for _, m := range cfg.CustomManagers {
		if managerReads(m, runtimePinFile) && slices.ContainsFunc(m.MatchStrings, func(expr string) bool {
			return extractsImage(expr, pins)
		}) {
			return true
		}
	}
	return false
}

// extractsImage says whether expr, read as Go's RE2 (named groups as
// (?<name>…)), captures depName equal to the image reference in mise.toml.
func extractsImage(expr string, pins updatePins) bool {
	re, err := regexp.Compile(expr)
	if err != nil || re.SubexpIndex("depName") < 0 {
		return false
	}
	m := re.FindStringSubmatch(pins.runtime)
	return m != nil && m[re.SubexpIndex("depName")] == pins.image
}

// managerReads matches a managerFilePatterns entry: /regex/ or a glob.
func managerReads(m updateManager, file string) bool {
	for _, p := range m.FilePatterns {
		if len(p) > 1 && strings.HasPrefix(p, "/") && strings.HasSuffix(p, "/") {
			re, err := regexp.Compile(p[1 : len(p)-1])
			if err == nil && re.MatchString(file) {
				return true
			}
		} else if globMatch(p, file) {
			return true
		}
	}
	return false
}

// lastSetting returns the index of the last rule that applies to every
// dependency of file and sets what has says it sets (later rules override
// earlier ones), or -1.
func lastSetting(rules []updateRule, file string, has func(updateRule) bool) int {
	last := -1
	for i, r := range rules {
		if has(r) && !r.narrowed() && r.appliesTo(file) {
			last = i
		}
	}
	return last
}

func setsGroup(r updateRule) bool  { return r.group != nil }
func setsLabels(r updateRule) bool { return r.hasLabels }

// lastGroup returns the index of the rule whose group holds every dependency of file, or -1.
func lastGroup(rules []updateRule, file string) int { return lastSetting(rules, file, setsGroup) }

// judgeRuntimeGroup holds every runtime-mirrored tool (mise.toml, the image
// among them) in one group labelled needs-entry-revision. A rule after the
// group's (or after the labels') that may apply to some of them and sets
// another group (or labels without the label) takes them out.
func judgeRuntimeGroup(cfg *updateConfig, pins updatePins) []Finding {
	if !slices.Contains(pins.files, runtimePinFile) {
		return nil
	}
	last := lastGroup(cfg.rules, runtimePinFile)
	if last < 0 {
		return []Finding{updateFinding("UPDATE_RUNTIME_GROUP", runtimePinFile, "no group holds the runtime-mirrored tools")}
	}
	group := *cfg.rules[last].group
	if group == "" {
		return []Finding{updateFinding("UPDATE_RUNTIME_GROUP", runtimePinFile, "packageRules[%d] leaves the runtime-mirrored tools ungrouped", last)}
	}
	labels := lastSetting(cfg.rules, runtimePinFile, setsLabels)
	var findings []Finding
	if labels < 0 || !slices.Contains(cfg.rules[labels].labels, runtimeLabel) {
		findings = append(findings, updateFinding("UPDATE_RUNTIME_GROUP", runtimePinFile, "group %q is not labelled %s", group, runtimeLabel))
	}
	for i, r := range cfg.rules {
		if !r.mayApply(runtimePinFile) {
			continue
		}
		if i > last && r.group != nil && *r.group != group {
			findings = append(findings, updateFinding("UPDATE_RUNTIME_GROUP", runtimeSubject(r, pins.image),
				"packageRules[%d] moves runtime-mirrored tools from %q to %q", r.index, group, *r.group))
		}
		if i > labels && r.hasLabels && !slices.Contains(r.labels, runtimeLabel) {
			findings = append(findings, updateFinding("UPDATE_RUNTIME_GROUP", runtimeSubject(r, pins.image),
				"packageRules[%d] labels runtime-mirrored tools without %s", r.index, runtimeLabel))
		}
	}
	return findings
}

// runtimeSubject names the tools a narrowed rule picks, or the rule.
func runtimeSubject(r updateRule, image string) string {
	if len(r.deps) == 0 {
		return fmt.Sprintf("%s:packageRules[%d]", runtimePinFile, r.index)
	}
	subject := strings.Join(r.deps, ",")
	if image != "" && slices.Contains(r.deps, image) {
		subject += " (LZ_OFFLINE_IMAGE)"
	}
	return subject
}

// judgeProviderGroup requires the group of every provider pin to hold the
// schema snapshot of each pinned version, and no later rule that may apply to
// a provider pin to move it to another group.
func judgeProviderGroup(cfg *updateConfig, pins updatePins) []Finding {
	groups := map[string]bool{}
	moved := map[int]bool{}
	var findings []Finding
	for _, file := range pins.provider {
		last := lastGroup(cfg.rules, file)
		if last < 0 {
			continue
		}
		group := *cfg.rules[last].group
		if group == "" && !moved[last] {
			moved[last] = true
			findings = append(findings, updateFinding("UPDATE_PROVIDER_GROUP", file, "packageRules[%d] leaves provider pins ungrouped", last))
		}
		groups[group] = true
		for _, r := range cfg.rules[last+1:] {
			if r.group != nil && *r.group != group && !moved[r.index] && r.mayApply(file) {
				moved[r.index] = true
				findings = append(findings, updateFinding("UPDATE_PROVIDER_GROUP", file,
					"packageRules[%d] moves provider pins from %q, which holds the schema snapshot, to %q", r.index, group, *r.group))
			}
		}
	}
	for _, version := range slices.Sorted(maps.Keys(pins.versions)) {
		findings = append(findings, judgeSnapshotGroup(cfg, version, groups)...)
	}
	return findings
}

// judgeSnapshotGroup requires the snapshot of version to be named by the rule
// whose group holds it, and that group to be every provider pin's.
func judgeSnapshotGroup(cfg *updateConfig, version string, groups map[string]bool) []Finding {
	snapshot := path.Join(providerSchemaDir, "ovh-"+version+".json")
	held := ""
	if last := lastGroup(cfg.rules, snapshot); last >= 0 && cfg.rules[last].matchesFile(snapshot) {
		held = *cfg.rules[last].group
	}
	var findings []Finding
	for _, group := range slices.Sorted(maps.Keys(groups)) {
		if group != held {
			findings = append(findings, updateFinding("UPDATE_PROVIDER_GROUP", snapshot,
				"provider group %q does not hold the schema snapshot (its group: %q)", group, held))
		}
	}
	return findings
}

// judgeSchemaSnapshots requires one snapshot per pinned ovh/ovh version, no
// snapshot of another version, and beside each snapshot its command record
// naming the snapshot's version and digest.
func judgeSchemaSnapshots(root string, pins updatePins) []Finding {
	dir := filepath.Join(root, filepath.FromSlash(providerSchemaDir))
	snapshots := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return []Finding{updateFinding("SCHEMA_VERSION", providerSchemaDir, "unreadable: %v", err)}
	}
	for _, e := range entries {
		if m := snapshotName.FindStringSubmatch(e.Name()); m != nil && e.Type().IsRegular() {
			snapshots[m[1]] = true
		}
	}
	var findings []Finding
	for _, version := range slices.Sorted(maps.Keys(pins.versions)) {
		if !snapshots[version] {
			findings = append(findings, updateFinding("SCHEMA_VERSION", strings.Join(pins.versions[version], ","),
				"pins ovh/ovh %s; no snapshot %s/ovh-%s.json", version, providerSchemaDir, version))
		}
	}
	for _, version := range slices.Sorted(maps.Keys(snapshots)) {
		name := path.Join(providerSchemaDir, "ovh-"+version+".json")
		if _, pinned := pins.versions[version]; !pinned {
			findings = append(findings, updateFinding("SCHEMA_VERSION", name,
				"snapshot of ovh/ovh %s; the locks pin %v", version, slices.Sorted(maps.Keys(pins.versions))))
		}
		findings = append(findings, judgeSchemaRecord(dir, version)...)
	}
	return findings
}

// judgeSchemaRecord checks ovh-<version>.command.txt against its snapshot.
func judgeSchemaRecord(dir, version string) []Finding {
	record := path.Join(providerSchemaDir, "ovh-"+version+".command.txt")
	snapshot, err := os.ReadFile(filepath.Join(dir, "ovh-"+version+".json"))
	if err != nil {
		return []Finding{updateFinding("SCHEMA_RECORD", record, "snapshot unreadable: %v", err)}
	}
	data, err := os.ReadFile(filepath.Join(dir, "ovh-"+version+".command.txt"))
	if err != nil {
		return []Finding{updateFinding("SCHEMA_RECORD", record, "no command record: %v", err)}
	}
	fields := recordFields(string(data))
	digest := sha256.Sum256(snapshot)
	var findings []Finding
	if provider := strings.Fields(fields["provider"]); len(provider) != 2 || !strings.HasSuffix(provider[0], "/ovh/ovh") || provider[1] != version {
		findings = append(findings, updateFinding("SCHEMA_RECORD", record, "provider %q is not ovh/ovh %s", fields["provider"], version))
	}
	if fields["sha256"] != hex.EncodeToString(digest[:]) {
		findings = append(findings, updateFinding("SCHEMA_RECORD", record, "sha256 %q is not the snapshot's", fields["sha256"]))
	}
	return findings
}

// recordFields reads the record's "key: value" lines; a repeated key keeps
// its first value.
func recordFields(text string) map[string]string {
	fields := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if _, seen := fields[key]; ok && !seen {
			fields[key] = strings.TrimSpace(value)
		}
	}
	return fields
}

// globMatch matches a slash-separated path against a minimatch-style glob
// with dotfiles allowed: ** spans directories, * and ? stay within one.
// Brace sets and classes are read literally, so they match nothing they do
// not spell out.
func globMatch(pattern, name string) bool {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch {
		case strings.HasPrefix(pattern[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case strings.HasPrefix(pattern[i:], "**"):
			b.WriteString(".*")
			i++
		case pattern[i] == '*':
			b.WriteString("[^/]*")
		case pattern[i] == '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	return err == nil && re.MatchString(name)
}
