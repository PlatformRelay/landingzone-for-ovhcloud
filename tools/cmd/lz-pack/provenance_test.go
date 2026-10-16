package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// packMainEnv makes the test binary run lz-pack's main with the JSON list of
// arguments it holds, so the command line is tested as the owner runs it.
// Only a child started by runPackMain (which also passes packMainRun) does.
const (
	packMainEnv = "LZ_PACK_TEST_MAIN_ARGS"
	packMainRun = "-test.run=^$"
)

func TestMain(m *testing.M) {
	if raw, ok := os.LookupEnv(packMainEnv); ok && slices.Equal(os.Args[1:], []string{packMainRun}) {
		var args []string
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(3)
		}
		os.Args = append([]string{"lz-pack"}, args...)
		flag.CommandLine = flag.NewFlagSet("lz-pack", flag.ExitOnError)
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runPackMain runs lz-pack's main in a child process and returns its output
// and exit code.
func runPackMain(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], packMainRun)
	child.Env = append(os.Environ(), packMainEnv+"="+string(raw))
	var out, errOut bytes.Buffer
	child.Stdout, child.Stderr = &out, &errOut
	err = child.Run()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		t.Fatal(err)
	}
	return out.String(), errOut.String(), child.ProcessState.ExitCode()
}

// statementCommit is the source commit the tests pack for; every digest in
// the fixtures differs from every other, so a swapped field is seen.
const statementCommit = "5d0c9e3b7a41f2c86e1d0b9a7c3f5e2d4b6a8c01"

// fixtureDigest is the bare hex sha256 of s.
func fixtureDigest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// statementFixture is a provenanceInput in the forms lz-pack holds them:
// manifest and layer as packed reports them ("sha256:<hex>"), resources.json
// and the entry as bare hex, like -manifest-sha.
func statementFixture() provenanceInput {
	return provenanceInput{
		Commit:    statementCommit,
		Manifest:  "sha256:" + fixtureDigest("manifest"),
		Layer:     "sha256:" + fixtureDigest("layer"),
		Resources: fixtureDigest("resources.json"),
		Entry:     fixtureDigest("lz-offline"),
		Builder:   "lz-pack@v0.0.0-test",
	}
}

type statementDigests struct {
	Name   string            `json:"name"`
	URI    string            `json:"uri"`
	Digest map[string]string `json:"digest"`
}

// statementView is the part of an in-toto Statement v1 with a SLSA
// provenance v1 predicate that FR-017 fixes.
type statementView struct {
	Type          string             `json:"_type"`
	Subject       []statementDigests `json:"subject"`
	PredicateType string             `json:"predicateType"`
	Predicate     struct {
		BuildDefinition struct {
			BuildType            string             `json:"buildType"`
			ExternalParameters   map[string]any     `json:"externalParameters"`
			ResolvedDependencies []statementDigests `json:"resolvedDependencies"`
		} `json:"buildDefinition"`
		RunDetails struct {
			Builder struct {
				ID string `json:"id"`
			} `json:"builder"`
		} `json:"runDetails"`
	} `json:"predicate"`
}

func decodeStatement(t *testing.T, data []byte) statementView {
	t.Helper()
	var s statementView
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("BEHAVIORAL_RED: statement is not JSON: %v\n%s", err, data)
	}
	return s
}

// keyFrame is one open JSON object or array while keyOrder walks tokens.
type keyFrame struct {
	object, wantKey, any bool
	last                 string
}

// keyOrder checks, token by token, that every object's keys are strictly
// ascending; whitespace and string escaping are free.
type keyOrder struct{ frames []keyFrame }

func (k *keyOrder) step(tok json.Token) error {
	n := len(k.frames)
	if n > 0 && k.frames[n-1].wantKey {
		return k.key(tok)
	}
	switch tok {
	case json.Delim('{'):
		k.frames = append(k.frames, keyFrame{object: true, wantKey: true})
		return nil
	case json.Delim('['):
		k.frames = append(k.frames, keyFrame{})
		return nil
	case json.Delim(']'):
		k.frames = k.frames[:n-1]
	}
	k.valueDone()
	return nil
}

func (k *keyOrder) key(tok json.Token) error {
	f := &k.frames[len(k.frames)-1]
	if tok == json.Delim('}') {
		k.frames = k.frames[:len(k.frames)-1]
		k.valueDone()
		return nil
	}
	name, _ := tok.(string)
	if f.any && name <= f.last {
		return fmt.Errorf("keys not sorted: %q after %q", name, f.last)
	}
	f.any, f.last, f.wantKey = true, name, false
	return nil
}

func (k *keyOrder) valueDone() {
	if n := len(k.frames); n > 0 && k.frames[n-1].object {
		k.frames[n-1].wantKey = true
	}
}

// statementCanonical reports whether data is one JSON value whose object
// keys are sorted at every depth, with no duplicate key.
func statementCanonical(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var order keyOrder
	for {
		tok, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := order.step(tok); err != nil {
			return err
		}
	}
}

// statementLeaves flattens a decoded JSON value into dotted paths.
func statementLeaves(value any, path string, into map[string]any) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			statementLeaves(child, path+"."+key, into)
		}
	case []any:
		for i, child := range v {
			statementLeaves(child, fmt.Sprintf("%s[%d]", path, i), into)
		}
	default:
		into[path] = v
	}
}

func leavesOf(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("BEHAVIORAL_RED: statement is not JSON: %v", err)
	}
	leaves := map[string]any{}
	statementLeaves(value, "", leaves)
	return leaves
}

// timeKeys are the keys SLSA provenance v1 and common encoders use for when
// a build ran.
var timeKeys = []string{"startedon", "finishedon", "buildstartedon", "buildfinishedon", "timestamp", "time", "created", "date"}

// timeLike reports a leaf that records when the statement was made: a leaf
// under a key used for times, at any depth, or a string that parses as a time.
func timeLike(path string, value any) bool {
	for _, segment := range strings.Split(strings.ToLower(path), ".") {
		if key, _, _ := strings.Cut(segment, "["); slices.Contains(timeKeys, key) {
			return true
		}
	}
	s, ok := value.(string)
	if !ok {
		return false
	}
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano, time.DateTime, time.DateOnly, time.RFC1123Z, time.UnixDate} {
		if _, err := time.Parse(layout, s); err == nil {
			return true
		}
	}
	return false
}

// statementKeys are the only top-level keys of an unsigned in-toto Statement
// v1; a DSSE envelope or a signature field would add or replace them.
var statementKeys = []string{"_type", "predicate", "predicateType", "subject"}

// statementWellFormed checks what every statement must be, whoever wrote it:
// an unsigned in-toto Statement v1 with a SLSA provenance v1 predicate, keys
// sorted at every depth, and no leaf recording a time.
func statementWellFormed(t *testing.T, data []byte) {
	t.Helper()
	if err := statementCanonical(data); err != nil {
		t.Errorf("BEHAVIORAL_RED: %v", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatalf("BEHAVIORAL_RED: statement is not a JSON object: %v", err)
	}
	keys := make([]string, 0, len(top))
	for key := range top {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, statementKeys) {
		t.Errorf("BEHAVIORAL_RED: top-level keys %v, want an unsigned statement's %v", keys, statementKeys)
	}
	s := decodeStatement(t, data)
	if s.Type != "https://in-toto.io/Statement/v1" || s.PredicateType != "https://slsa.dev/provenance/v1" {
		t.Errorf("BEHAVIORAL_RED: _type %q predicateType %q", s.Type, s.PredicateType)
	}
	for path, value := range leavesOf(t, data) {
		if timeLike(path, value) {
			t.Errorf("BEHAVIORAL_RED: statement records a time at %s: %v", path, value)
		}
	}
}

// Given a provenanceInput, when provenance renders twice, then the bytes are
// identical, keys are sorted at every depth and no leaf is a time: a
// statement that is not reproducible cannot be compared across packs.
func TestProvenanceReproducible(t *testing.T) {
	first, err := provenance(statementFixture())
	if err != nil {
		t.Fatalf("BEHAVIORAL_RED: valid input refused: %v", err)
	}
	second, err := provenance(statementFixture())
	if err != nil {
		t.Fatalf("BEHAVIORAL_RED: valid input refused the second time: %v", err)
	}
	if len(first) == 0 || !bytes.Equal(first, second) {
		t.Errorf("BEHAVIORAL_RED: two renders differ:\n%s\n%s", first, second)
	}
	statementWellFormed(t, first)
}

// subjectsAre checks that the statement's subjects are exactly the manifest
// and layer digests, each a sha256 digest set with a name of its own.
func subjectsAre(t *testing.T, s statementView, manifest, layer string) {
	t.Helper()
	var got []string
	names := map[string]bool{}
	for _, subject := range s.Subject {
		if len(subject.Digest) != 1 || subject.Name == "" || names[subject.Name] {
			t.Errorf("BEHAVIORAL_RED: subject %+v: want one sha256 digest and a name of its own", subject)
		}
		names[subject.Name] = true
		got = append(got, subject.Digest["sha256"])
	}
	want := []string{strings.TrimPrefix(manifest, "sha256:"), strings.TrimPrefix(layer, "sha256:")}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("BEHAVIORAL_RED: subjects %v, want the manifest and the layer %v", got, want)
	}
}

// dependency returns the resolved dependency carrying the sha256 digest.
func dependency(s statementView, sha string) (statementDigests, bool) {
	for _, d := range s.Predicate.BuildDefinition.ResolvedDependencies {
		if d.Digest["sha256"] == sha {
			return d, true
		}
	}
	return statementDigests{}, false
}

// materialsAre checks the statement names the source commit among its
// external parameters, resources.json and the entry among its resolved
// dependencies, a build type and a builder naming lz-pack.
func materialsAre(t *testing.T, data []byte, s statementView, resources, entry, builder string) {
	t.Helper()
	commit := false
	for path, value := range leavesOf(t, data) {
		commit = commit || strings.HasPrefix(path, ".predicate.buildDefinition.externalParameters.") && value == statementCommit
	}
	if !commit {
		t.Errorf("BEHAVIORAL_RED: externalParameters %v do not name commit %s", s.Predicate.BuildDefinition.ExternalParameters, statementCommit)
	}
	if d, ok := dependency(s, resources); !ok || !strings.Contains(d.Name+" "+d.URI, "resources.json") {
		t.Errorf("BEHAVIORAL_RED: no resolved dependency named resources.json with digest %s: %+v", resources, s.Predicate.BuildDefinition.ResolvedDependencies)
	}
	if d, ok := dependency(s, entry); !ok || d.Name == "" && d.URI == "" {
		t.Errorf("BEHAVIORAL_RED: no named resolved dependency for the entry digest %s: %+v", entry, s.Predicate.BuildDefinition.ResolvedDependencies)
	}
	if s.Predicate.BuildDefinition.BuildType == "" {
		t.Errorf("BEHAVIORAL_RED: buildDefinition has no buildType")
	}
	if id := s.Predicate.RunDetails.Builder.ID; !strings.Contains(id, "lz-pack") || !strings.Contains(id, builder) {
		t.Errorf("BEHAVIORAL_RED: builder id %q does not name lz-pack and %q", id, builder)
	}
}

// Given the statement, then it is an in-toto Statement v1 with a SLSA
// provenance v1 predicate, its subjects are the image and its materials name
// the source commit, resources.json and the entry: a statement missing any of
// them does not bind the image to what built it.
func TestProvenanceStatement(t *testing.T) {
	in := statementFixture()
	data, err := provenance(in)
	if err != nil {
		t.Fatalf("BEHAVIORAL_RED: valid input refused: %v", err)
	}
	statementWellFormed(t, data)
	s := decodeStatement(t, data)
	subjectsAre(t, s, in.Manifest, in.Layer)
	materialsAre(t, data, s, in.Resources, in.Entry, in.Builder)
}

// Given an input missing one of the values the statement binds, or holding
// something that is not that value (a ref or a short id for the commit, a
// string that is not a sha256 digest in its field's form), when provenance
// renders, then it refuses rather than write a statement without that
// material. Each row first renders the full input (the control), so a
// provenance that refuses everything fails the row.
func TestProvenanceRefusesMissingInput(t *testing.T) {
	for name, drop := range map[string]func(*provenanceInput){
		"commit":             func(in *provenanceInput) { in.Commit = "" },
		"manifest":           func(in *provenanceInput) { in.Manifest = "" },
		"layer":              func(in *provenanceInput) { in.Layer = "" },
		"resources":          func(in *provenanceInput) { in.Resources = "" },
		"entry":              func(in *provenanceInput) { in.Entry = "" },
		"builder":            func(in *provenanceInput) { in.Builder = "" },
		"commit-ref":         func(in *provenanceInput) { in.Commit = "main" },
		"commit-short":       func(in *provenanceInput) { in.Commit = statementCommit[:7] },
		"commit-upper":       func(in *provenanceInput) { in.Commit = strings.ToUpper(statementCommit) },
		"manifest-not-hex":   func(in *provenanceInput) { in.Manifest = "sha256:" + strings.Repeat("z", 64) },
		"layer-unprefixed":   func(in *provenanceInput) { in.Layer = strings.TrimPrefix(in.Layer, "sha256:") },
		"resources-prefixed": func(in *provenanceInput) { in.Resources = "sha256:" + in.Resources },
		"entry-short":        func(in *provenanceInput) { in.Entry = in.Entry[:63] },
	} {
		t.Run(name, func(t *testing.T) {
			in := statementFixture()
			if _, err := provenance(in); err != nil {
				t.Fatalf("BEHAVIORAL_RED: control: valid input refused: %v", err)
			}
			drop(&in)
			if data, err := provenance(in); err == nil || data != nil {
				t.Errorf("BEHAVIORAL_RED: input with %s rendered %s (err %v)", name, data, err)
			}
		})
	}
}

// packedDigests reads the layer and manifest digests lz-pack prints.
func packedDigests(t *testing.T, stdout string) (layer, manifest string) {
	t.Helper()
	if _, err := fmt.Sscanf(stdout, "layer %s\nmanifest %s\n", &layer, &manifest); err != nil {
		t.Fatalf("BEHAVIORAL_RED: lz-pack printed %q: %v", stdout, err)
	}
	return layer, manifest
}

// packWithCommit runs lz-pack for the bundle into a new layout under a temp
// directory and returns the layout, the statement and the digests printed.
func packWithCommit(t *testing.T, dir, launcher, manifestSHA, name string) (out string, statement []byte, layer, manifest string) {
	t.Helper()
	out = filepath.Join(t.TempDir(), name)
	stdout, stderr, code := runPackMain(t, "-bundle", dir, "-manifest-sha", manifestSHA, "-bwrap", launcher, "-out", out, "-commit", statementCommit)
	if code != 0 {
		t.Fatalf("BEHAVIORAL_RED: lz-pack -commit exit %d: %s", code, stderr)
	}
	layer, manifest = packedDigests(t, stdout)
	statement, err := os.ReadFile(filepath.Join(out, "provenance.json"))
	if err != nil {
		t.Fatalf("BEHAVIORAL_RED: no provenance.json beside the packed image: %v", err)
	}
	return out, statement, layer, manifest
}

// layoutDigests reads the manifest digest from the layout's index.json and
// the layer digest from that manifest, as a registry would.
func layoutDigests(t *testing.T, out string) (layer, manifest string) {
	t.Helper()
	var index struct {
		Manifests []struct {
			Digest string `json:"digest"`
		} `json:"manifests"`
	}
	var image struct {
		Layers []struct {
			Digest string `json:"digest"`
		} `json:"layers"`
	}
	data, err := os.ReadFile(filepath.Join(out, "index.json"))
	if err == nil {
		err = json.Unmarshal(data, &index)
	}
	if err != nil || len(index.Manifests) != 1 {
		t.Fatalf("index.json: %v %+v", err, index)
	}
	data, err = os.ReadFile(blob(out, index.Manifests[0].Digest))
	if err == nil {
		err = json.Unmarshal(data, &image)
	}
	if err != nil || len(image.Layers) != 1 {
		t.Fatalf("manifest: %v %+v", err, image)
	}
	return image.Layers[0].Digest, index.Manifests[0].Digest
}

// layoutHoldsOnly checks the layout holds the image and its statement and
// nothing else: no signature or envelope beside the statement.
func layoutHoldsOnly(t *testing.T, out string) {
	t.Helper()
	files, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, file := range files {
		names = append(names, file.Name())
	}
	if want := []string{"blobs", "index.json", "oci-layout", "provenance.json"}; !slices.Equal(names, want) {
		t.Errorf("BEHAVIORAL_RED: layout holds %v, want %v", names, want)
	}
}

// Given the two reproducibility packs (the same bundle, its timestamps
// changed in between), then each layout carries provenance.json, the two
// statements are byte-identical, their subjects are the manifest and layer
// the layout holds, and their materials are this commit, this resources.json
// and this entry: a statement about anything but the packed image fails.
func TestPackWritesProvenance(t *testing.T) {
	b := prepared(t)
	out, first, layer, manifest := packWithCommit(t, b.Dir, b.Launcher, b.ManifestSHA, "a")
	layoutHoldsOnly(t, out)
	if storedLayer, storedManifest := layoutDigests(t, out); storedLayer != layer || storedManifest != manifest {
		t.Errorf("BEHAVIORAL_RED: lz-pack printed layer %s manifest %s, the layout holds %s %s", layer, manifest, storedLayer, storedManifest)
	}
	if digestOf(t, filepath.Join(b.Dir, "resources.json")) != "sha256:"+b.ManifestSHA {
		t.Fatal("fixture: -manifest-sha is not the resources.json digest")
	}
	later := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, path := range []string{filepath.Join(b.Dir, "lz-offline"), filepath.Join(b.Dir, "resources"), b.Launcher} {
		if err := os.Chtimes(path, later, later); err != nil {
			t.Fatal(err)
		}
	}
	_, second, layer2, manifest2 := packWithCommit(t, b.Dir, b.Launcher, b.ManifestSHA, "b")
	if layer != layer2 || manifest != manifest2 {
		t.Fatalf("fixture: packs differ: %s %s / %s %s", layer, manifest, layer2, manifest2)
	}
	if !bytes.Equal(first, second) {
		t.Errorf("BEHAVIORAL_RED: the two packs' statements differ:\n%s\n%s", first, second)
	}
	statementWellFormed(t, first)
	s := decodeStatement(t, first)
	subjectsAre(t, s, manifest, layer)
	entry := strings.TrimPrefix(digestOf(t, filepath.Join(b.Dir, "lz-offline")), "sha256:")
	materialsAre(t, first, s, b.ManifestSHA, entry, "lz-pack@")
	if _, version, _ := strings.Cut(s.Predicate.RunDetails.Builder.ID, "lz-pack@"); version == "" {
		t.Errorf("BEHAVIORAL_RED: builder id %q names no lz-pack version", s.Predicate.RunDetails.Builder.ID)
	}
}

// Given no -commit, lz-pack refuses and leaves no layout: every packed image
// carries a statement naming its source commit. The control packs the same
// bundle with -commit.
func TestPackProvenanceNeedsCommit(t *testing.T) {
	b := prepared(t)
	packWithCommit(t, b.Dir, b.Launcher, b.ManifestSHA, "control")
	out := filepath.Join(t.TempDir(), "layout")
	_, stderr, code := runPackMain(t, "-bundle", b.Dir, "-manifest-sha", b.ManifestSHA, "-bwrap", b.Launcher, "-out", out)
	if code == 0 {
		t.Errorf("BEHAVIORAL_RED: lz-pack without -commit exit 0")
	}
	if _, err := os.Lstat(out); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("BEHAVIORAL_RED: lz-pack without -commit left %s (%v; %s)", out, err, stderr)
	}
}
