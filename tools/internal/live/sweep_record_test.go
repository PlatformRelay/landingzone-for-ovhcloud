package live

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Spec 007 FR-002 (T005): a probe `--cleanup` keeps the run's earlier record. keepRecord moves
// every entry of the run directory but inventory.jsonl, baseline.json, observations.json and
// attempts/ into attempts/<n>/ (directory 0700, files 0600; n = 1 + the highest existing number),
// copies observations.json there, leaves those three in place, and returns that directory, or ""
// when there was nothing to move. Nothing in attempts/ is ever rewritten (data-model.md *Run record
// archive*). T027/T028 lost their error text to the cleanup's overwrite.

// staysInPlace are the entries a cleanup reads again or appends to: never moved.
var staysInPlace = []string{"inventory.jsonl", "baseline.json", "observations.json"}

// recordFile is one file of a run record: its content and permission bits.
type recordFile struct {
	content string
	mode    fs.FileMode
}

// recordTree maps every regular file under dir (relative path) to its content and mode.
func recordTree(t *testing.T, dir string) map[string]recordFile {
	t.Helper()
	out := map[string]recordFile{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		out[rel] = recordFile{string(raw), fi.Mode().Perm()}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// writeRunRecord writes files (relative path -> content) into dir, 0600 in 0700 directories, as
// writeRecord does.
func writeRunRecord(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// fullRunRecord is a failed probe run's record as T027/T028 left it, plus the baseline (FR-006)
// and a check's observations.
func fullRunRecord() map[string]string {
	return map[string]string{
		"summary.json":          `{"run_id":"20261016T150207Z-be89","outcome":"fail","error":"leftover check: fail (0 leftovers, 1 errors)"}`,
		"leftovers.json":        `{"outcome":"fail","errors":["GET /cloud/project/p1/network/private: 500"],"attempts":[]}`,
		"errors.txt":            "Error: fake destroy failure\n",
		"listings/network.json": `[{"id":"pn-1","name":"lzprobe-net-20261016t150207z-be89"}]`,
		"inventory.jsonl":       `{"type":"ovh_cloud_project_network_private","id":"pn-1"}` + "\n",
		"baseline.json":         `{"run_id":"20261016T150207Z-be89","listed":{},"complete":true,"errors":[]}`,
		"observations.json":     `{"check":"storage-iam","observations":[]}`,
	}
}

// placeProblem says what is wrong with where rel (given with content) is after keepRecord, or "":
// observations.json copied into attempts/1/ and kept in place, the other stays-in-place entries
// kept unchanged and not copied, every other file moved unchanged into attempts/1/.
func placeProblem(rel, content string, got map[string]recordFile) string {
	archived, inSlot := got[filepath.Join("attempts", "1", rel)]
	top, onTop := got[rel]
	switch {
	case rel == "observations.json":
		if inSlot && archived.content == content && onTop && top.content == content {
			return ""
		}
		return fmt.Sprintf("archived %v (%q), in place %v (%q); want copied unchanged and kept in place", inSlot, archived.content, onTop, top.content)
	case slices.Contains(staysInPlace, rel):
		if !inSlot && onTop && top.content == content {
			return ""
		}
		return fmt.Sprintf("archived %v, in place %v (%q); want kept in place unchanged, not archived", inSlot, onTop, top.content)
	case inSlot && archived.content == content && !onTop:
		return ""
	}
	return fmt.Sprintf("archived %v (%q), still in place %v; want moved unchanged into attempts/1/", inSlot, archived.content, onTop)
}

// TestKeepRecordArchivesWholeRecord: given a run directory with the whole record / when keepRecord
// runs / then every file but the three stays-in-place ones is under attempts/1/ unchanged (errors.txt
// and listings/ included), observations.json is copied there and stays, inventory.jsonl and
// baseline.json stay and are not copied, attempts/1/ is 0700 and its files 0600 → exposes the
// overwrite that lost T027/T028's error text (two files kept only, or the listings dropped), and a
// cleanup that moves the files its undo and leftover check read again.
func TestKeepRecordArchivesWholeRecord(t *testing.T) {
	dir := t.TempDir()
	given := fullRunRecord()
	writeRunRecord(t, dir, given)
	kept, err := keepRecord(dir)
	slot := filepath.Join(dir, "attempts", "1")
	if err != nil || kept != slot {
		t.Errorf("keepRecord = %q, %v; want %q, nil", kept, err, slot)
	}
	got := recordTree(t, dir)
	for rel, content := range given {
		if p := placeProblem(rel, content, got); p != "" {
			t.Errorf("%s: %s", rel, p)
		}
	}
	for rel, f := range recordTree(t, slot) {
		if f.mode != 0o600 {
			t.Errorf("attempts/1/%s has mode %v, want 0600", rel, f.mode)
		}
	}
	if fi, err := os.Stat(slot); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("attempts/1: %v, %v; want a 0700 directory", fi, err)
	}
}

// TestKeepRecordNextSlot: given attempts/<n>/ already present / when keepRecord runs / then the
// record goes to attempts/<highest+1>/ and every earlier slot is unchanged → exposes reuse of an
// archive slot (a second cleanup writing over the first cleanup's archive).
func TestKeepRecordNextSlot(t *testing.T) {
	for _, tc := range []struct {
		name     string
		existing []string
		want     string
	}{
		{"second-cleanup", []string{"1"}, "2"},
		{"after-a-gap", []string{"1", "3"}, "4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			given := map[string]string{"summary.json": `{"outcome":"fail","error":"second"}`, "inventory.jsonl": "{}\n"}
			for _, n := range tc.existing {
				given[filepath.Join("attempts", n, "summary.json")] = `{"outcome":"fail","error":"attempt ` + n + `"}`
				given[filepath.Join("attempts", n, "errors.txt")] = "attempt " + n + "\n"
			}
			writeRunRecord(t, dir, given)
			before := recordTree(t, filepath.Join(dir, "attempts"))
			kept, err := keepRecord(dir)
			slot := filepath.Join(dir, "attempts", tc.want)
			if err != nil || kept != slot {
				t.Errorf("keepRecord = %q, %v; want %q, nil", kept, err, slot)
			}
			after := recordTree(t, filepath.Join(dir, "attempts"))
			for rel, f := range before {
				if after[rel] != f {
					t.Errorf("attempts/%s changed: %q, want %q", rel, after[rel].content, f.content)
				}
			}
			if f := after[filepath.Join(tc.want, "summary.json")]; f.content != given["summary.json"] {
				t.Errorf("attempts/%s/summary.json = %q, want the record moved there", tc.want, f.content)
			}
		})
	}
}

// TestKeepRecordNothingToMove: given a run directory with only inventory.jsonl / when keepRecord
// runs / then it returns "", nil, creates no attempts/ and leaves inventory.jsonl as it was →
// exposes a cleanup that fails, or archives an empty slot, when there is no earlier record.
func TestKeepRecordNothingToMove(t *testing.T) {
	dir := t.TempDir()
	given := map[string]string{"inventory.jsonl": `{"type":"ovh_cloud_project_network_private","id":"pn-1"}` + "\n"}
	writeRunRecord(t, dir, given)
	kept, err := keepRecord(dir)
	if err != nil || kept != "" {
		t.Errorf("keepRecord = %q, %v; want \"\", nil", kept, err)
	}
	got := recordTree(t, dir)
	want := map[string]recordFile{"inventory.jsonl": {given["inventory.jsonl"], 0o600}}
	if !maps.Equal(got, want) {
		t.Errorf("run directory %v, want only the unchanged inventory.jsonl", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "attempts")); err == nil {
		t.Error("attempts/ created with nothing to move")
	}
}

// TestRecordDirArchivesOnCleanupOnly (T007): given a run directory holding a record / when the
// runner prepares it for a run that is not a cleanup / then nothing moves → exposes an archive on
// every run (a chain or a fresh probe moving what its own run reads, such as the baseline).
func TestRecordDirArchivesOnCleanupOnly(t *testing.T) {
	dir := t.TempDir()
	given := fullRunRecord()
	writeRunRecord(t, dir, given)
	before := recordTree(t, dir)
	if err := (Runner{Dir: dir}).recordDir(); err != nil {
		t.Fatalf("recordDir: %v", err)
	}
	if got := recordTree(t, dir); !maps.Equal(got, before) {
		t.Errorf("run directory changed by a run that is not a cleanup: %v, want %v", got, before)
	}
}

// TestRecordDirRefusesWhenArchiveFails (T007): given a record whose archive cannot be made
// (attempts is a file, not a directory) / when a cleanup prepares the run directory / then it
// fails and the record stays where it was → exposes a cleanup that goes on and writes its own
// summary.json, leftovers.json and errors.txt over the record it could not keep (constitution V).
func TestRecordDirRefusesWhenArchiveFails(t *testing.T) {
	dir := t.TempDir()
	given := fullRunRecord()
	given["attempts"] = "not a directory\n"
	writeRunRecord(t, dir, given)
	before := recordTree(t, dir)
	if err := (Runner{Dir: dir, cleanupOnly: true}).recordDir(); err == nil {
		t.Error("recordDir of a cleanup whose archive failed = nil; want the failure")
	}
	if got := recordTree(t, dir); !maps.Equal(got, before) {
		t.Errorf("run directory after a failed archive: %v, want it unchanged", got)
	}
}
