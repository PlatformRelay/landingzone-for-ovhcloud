package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// Spec 007 FR-002 (T005) through the entry: `lz-live probe --cleanup <run-id>` first moves the run
// directory's whole record, except inventory.jsonl, baseline.json and observations.json, into
// attempts/<n>/, copies observations.json there, then writes its own; observations.json stays in
// place unchanged, so Recorded and the undo of every later cleanup still read what the run
// observed (runner.go:786-794). T027/T028's error text was lost to the cleanup's overwrite.

const netListing = "/v1/cloud/project/p1/network/private"

// recordedPolicy is the policy an accepted IAM write created (a policy uuid, as recordedIDs
// demands).
const recordedPolicy = "5f0c6a1e-2b3d-4c5e-8f9a-0b1c2d3e4f5a"

// failedNetRun runs the net probe with a failing destroy and an lzprobe- leftover, so the run keeps
// its files and writes summary.json, leftovers.json, errors.txt and inventory.jsonl; it returns the
// run id and its record directory with the marker and the leftover removed for the cleanup.
func (w *probeWorld) failedNetRun(t *testing.T) (id, rec string) {
	t.Helper()
	marker := filepath.Join(filepath.Dir(w.git), "destroy-fails")
	writePrivate(t, marker, "")
	w.listings[netListing] = `[{"id":"pn-old","name":"lzprobe-old","status":"ACTIVE","type":"private","vlanId":0,"regions":[]}]`
	code, stdout, _ := w.run(t, "probe", "--reviewed-sha", fakeHead, "tests/live/probes/net")
	if code == 0 {
		t.Fatalf("premise: a probe whose destroy failed exited 0:\n%s", stdout)
	}
	id = runIDOf(t, stdout)
	rec = filepath.Join(w.checkout, ".local", "live", id)
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	delete(w.listings, netListing)
	return id, rec
}

// recordFiles is treeOf without the directories.
func recordFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := treeOf(t, dir)
	for rel := range out {
		if strings.HasSuffix(rel, "/") {
			delete(out, rel)
		}
	}
	return out
}

// cleanupPlaceProblem says what is wrong with where rel (content before the cleanup) is after it,
// or "": observations.json copied into attempts/1/ and kept in place unchanged, baseline.json kept
// unchanged and inventory.jsonl kept (the cleanup may append to it), neither archived, every other
// file under attempts/1/ unchanged.
func cleanupPlaceProblem(rel, content string, after map[string]string) string {
	archived, inSlot := after[filepath.Join("attempts", "1", rel)]
	top, onTop := after[rel]
	switch {
	case rel == "observations.json":
		if inSlot && archived == content && top == content {
			return ""
		}
		return fmt.Sprintf("copy in attempts/1/ %v, in place unchanged %v; want both", inSlot && archived == content, top == content)
	case rel == "baseline.json" || rel == "inventory.jsonl":
		if !inSlot && onTop && (rel == "inventory.jsonl" || top == content) {
			return ""
		}
		return fmt.Sprintf("archived %v, in place %v; want kept in place, not archived", inSlot, onTop)
	case inSlot && archived == content:
		return ""
	}
	return fmt.Sprintf("not under attempts/1/ unchanged (archived %v); the cleanup overwrote or dropped the run's record", inSlot)
}

// TestProbeCleanupKeepsRecord: given a probe run directory with summary.json, leftovers.json,
// errors.txt, listings/network.json, inventory.jsonl and baseline.json (and a check's
// observations.json) / when `--cleanup <run-id>` runs against fakes / then every file but
// inventory.jsonl, baseline.json and observations.json is under attempts/1/ unchanged, a copy of
// observations.json is there too, those three stay in place, and the cleanup's own records are
// written beside → exposes the overwrite that lost T027/T028's error text, errors.txt and the
// listings included.
func TestProbeCleanupKeepsRecord(t *testing.T) {
	w := newProbeWorld(t)
	id, rec := w.failedNetRun(t)
	for _, f := range []string{"summary.json", "leftovers.json", "errors.txt", "inventory.jsonl"} {
		if _, err := os.Stat(filepath.Join(rec, f)); err != nil {
			t.Fatalf("premise: the failed run wrote no %s: %v", f, err)
		}
	}
	sum := sha256.Sum256([]byte("p1"))
	writePrivate(t, filepath.Join(rec, "listings", "network.json"), `[{"id":"pn-old","name":"lzprobe-old"}]`)
	writePrivate(t, filepath.Join(rec, "baseline.json"), fmt.Sprintf(`{"run_id":%q,"project_sha256":%q,"listed":{},"complete":true,"errors":[]}`, id, hex.EncodeToString(sum[:])))
	writePrivate(t, filepath.Join(rec, "observations.json"), `{"check":"none","observations":[]}`)
	before := recordFiles(t, rec)

	code, stdout, stderr := w.run(t, "probe", "--reviewed-sha", fakeHead, "--cleanup", id)
	if code != 0 {
		t.Fatalf("premise: cleanup exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	after := recordFiles(t, rec)
	for rel, content := range before {
		if p := cleanupPlaceProblem(rel, content, after); p != "" {
			t.Errorf("%s: %s", rel, p)
		}
	}
	if s := after["summary.json"]; !strings.Contains(s, `"outcome":"pass"`) || !strings.Contains(s, id) {
		t.Errorf("summary.json in place %q: want the cleanup's own pass record beside the archive", s)
	}
	if _, ok := after["leftovers.json"]; !ok {
		t.Error("the cleanup wrote no leftovers.json beside the archive")
	}
	w.noSecret(t, stdout, stderr)
}

// TestProbeCleanupWithoutRecord: given a run directory with only inventory.jsonl / when cleanup
// runs / then it proceeds (exit 0, its own summary written) and archives nothing → exposes a
// cleanup that fails on absence (keepRecord's "" is TestKeepRecordNothingToMove's).
func TestProbeCleanupWithoutRecord(t *testing.T) {
	w := newProbeWorld(t)
	id, rec := w.failedNetRun(t)
	for rel := range recordFiles(t, rec) {
		if rel != "inventory.jsonl" {
			if err := os.Remove(filepath.Join(rec, rel)); err != nil {
				t.Fatal(err)
			}
		}
	}
	_ = os.Remove(filepath.Join(rec, "listings"))
	code, stdout, stderr := w.run(t, "probe", "--reviewed-sha", fakeHead, "--cleanup", id)
	if code != 0 {
		t.Errorf("cleanup of a run without a record: exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(rec, "attempts")); err == nil {
		t.Error("attempts/ created with no earlier record")
	}
	if s, _ := os.ReadFile(filepath.Join(rec, "summary.json")); !strings.Contains(string(s), `"outcome":"pass"`) {
		t.Errorf("summary.json %q: want the cleanup's pass record", s)
	}
}

// acceptIAMWrite answers the tenant's IAM write as accepted (a policy OVHcloud created) and every
// policy DELETE with 204, recording who deleted which path; every other request goes to next.
func (w *probeWorld) acceptIAMWrite(next func(http.ResponseWriter, *http.Request) bool) *[]string {
	var mu sync.Mutex
	deleted := new([]string)
	w.apiHook = func(rw http.ResponseWriter, r *http.Request) bool {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v2/iam/policy":
			rw.WriteHeader(http.StatusCreated)
			fmt.Fprintf(rw, `{"id":%q,"name":"x","createdAt":"2026-10-06T12:00:00Z"}`, recordedPolicy)
			return true
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v2/iam/policy/"):
			mu.Lock()
			*deleted = append(*deleted, tokenClients[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]+" "+r.URL.Path)
			mu.Unlock()
			rw.WriteHeader(http.StatusNoContent)
			return true
		}
		return next(rw, r)
	}
	return deleted
}

// failedIAMRun runs the storage-iam probe with an accepted IAM write and a failing destroy, so the
// root is kept and observations.json records the write's policy id; it returns the run id, its
// record directory, observations.json as the run wrote it and the policy DELETEs seen.
func (w *probeWorld) failedIAMRun(t *testing.T) (id, rec string, obs []byte, deleted *[]string) {
	t.Helper()
	w.withIAMS3(t)
	w.storageIAMRoot(t)
	var seen []string
	w.iamAnswers(false, &seen)
	deleted = w.acceptIAMWrite(w.apiHook)
	w.publishIAMCheck(t, iamRun)
	marker := filepath.Join(filepath.Dir(w.git), "destroy-fails")
	writePrivate(t, marker, "")
	code, stdout, _ := w.run(t, "probe", "--reviewed-sha", fakeHead, "tests/live/probes/storage-iam")
	if code == 0 {
		t.Fatalf("premise: a probe whose destroy failed exited 0:\n%s", stdout)
	}
	id = runIDOf(t, stdout)
	rec = filepath.Join(w.checkout, ".local", "live", id)
	obs, err := os.ReadFile(filepath.Join(rec, "observations.json"))
	if err != nil || !strings.Contains(string(obs), recordedPolicy) {
		t.Fatalf("premise: observations.json %q (%v) does not record the accepted write's policy id", obs, err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	return id, rec, obs, deleted
}

// archivedObservations fails unless, after two cleanups, observations.json is in place unchanged,
// attempts/2/ holds a copy of it and attempts/1/ is as the first cleanup left it.
func archivedObservations(t *testing.T, rec string, obs []byte, first map[string]string) {
	t.Helper()
	after := recordFiles(t, rec)
	if after["observations.json"] != string(obs) {
		t.Errorf("observations.json in place = %q, want the run's, unchanged", after["observations.json"])
	}
	if got := after[filepath.Join("attempts", "2", "observations.json")]; got != string(obs) {
		t.Errorf("after the second cleanup attempts/2/observations.json = %q, want a copy of the run's", got)
	}
	for rel, content := range first {
		if after[filepath.Join("attempts", "1", rel)] != content {
			t.Errorf("attempts/1/%s changed by the second cleanup", rel)
		}
	}
}

// TestProbeCleanupKeepsObservationsForUndo: given a storage-iam probe run whose observations.json
// records a policy id that the policy listing omits (a lagging listing) / when two `--cleanup
// <run-id>` runs follow in fresh processes, the first failing its leftover check / then the second
// cleanup's undo still receives the recorded policy id through Recorded and deletes that policy by
// id, and each cleanup archived a copy of observations.json → exposes archiving that hides
// observations.json from session.undo (runner.go:786-794; storageiam.go:181,532).
func TestProbeCleanupKeepsObservationsForUndo(t *testing.T) {
	w := newProbeWorld(t)
	id, rec, obs, deleted := w.failedIAMRun(t)

	// The first cleanup fails its leftover check: the run keeps its files.
	w.listings[netListing] = `[{"id":"pn-old","name":"lzprobe-old","status":"ACTIVE","type":"private","vlanId":0,"regions":[]}]`
	code, stdout, stderr := w.run(t, "probe", "--reviewed-sha", fakeHead, "--cleanup", id)
	if code == 0 {
		t.Fatalf("premise: a cleanup with an lzprobe- leftover exited 0:\n%s", stdout)
	}
	if _, err := os.Stat(filepath.Join(w.probeDir(id), "passphrase.env")); err != nil {
		t.Fatalf("premise: the failed cleanup did not keep the run's files: %v\n%s", err, stderr)
	}
	first := recordFiles(t, filepath.Join(rec, "attempts", "1"))
	if first["observations.json"] != string(obs) {
		t.Errorf("after the first cleanup attempts/1/observations.json = %q, want a copy of the run's", first["observations.json"])
	}

	// The second cleanup, a fresh run() as a new process would be (new deps, a new check and its
	// closures; nothing of the first cleanup kept in memory), with the policy listing still lagging.
	delete(w.listings, netListing)
	*deleted = nil
	code, stdout, stderr = w.run(t, "probe", "--reviewed-sha", fakeHead, "--cleanup", id)
	if code != 0 {
		t.Errorf("second cleanup: exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if want := "EU.sandboxadmin /v2/iam/policy/" + recordedPolicy; !slices.Contains(*deleted, want) {
		t.Errorf("second cleanup's deletes %v: want %q, the recorded id the lagging listing omits", *deleted, want)
	}
	if strings.Contains(stdout, "no observations.json") {
		t.Errorf("second cleanup's undo ran without the run's observations.json:\n%s", stdout)
	}
	archivedObservations(t, rec, obs, first)
	w.noSecret(t, stdout, stderr)
}
