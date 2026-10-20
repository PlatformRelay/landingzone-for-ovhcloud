// Package alerting is the budget-alert probe check (spec 007 FR-007, FR-008): its Observe reads
// the root's alert back before the destroy, and its Sweep deletes, against the run's baseline, the
// alerts the run may have created.
//
// An alert has no name (kb api/v1/cloud.json, cloud.alerting.Alerting), so the Sweep cannot find
// the run's alert by a run-derived name as the other probes' sweeps do: it trusts only the run's
// baseline (data-model *Probe baseline*), the project's alert ids listed before the first apply.
// With a valid baseline it reads each listed id absent from it and deletes it only if it matches
// the root's plan (threshold 1, delay 3600, the account's own address from GET /me); any other is
// reported and fails the sweep. It never deletes a baseline id. A pre-create run (nothing can
// have been created) sends no request; an untrusted run is refused before any request.
package alerting

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

// The root's plan (tests/live/probes/budget-alert/main.tf, data-model *Budget-alert probe check*).
const (
	planThreshold = 1
	planDelay     = 3600
)

// stateFile is the root's state file name under state/probes/<run-id>/ (probe.tf's pattern).
const stateFile = "budget-alert.tfstate"

// alertKind is the alert's provider type, the baseline's key (live namelessKinds).
const alertKind = "ovh_cloud_project_alerting"

// Options are what the check needs from lz-live (data-model *Fixed signatures*).
type Options struct {
	API   live.API
	Admin live.Credential
	// RunDir is a run's record directory (.local/live/<run-id>/), which holds baseline.json and
	// inventory.jsonl.
	RunDir func(runID string) string
	// StateDir is accounts/<account>/state: the recovery copy is baselines/<run-id>.json and the
	// run's state probes/<run-id>/budget-alert.tfstate under it.
	StateDir string
	// Project is the run's project (probe.env's), which Observe reads the alert in.
	Project string
}

// Check is the budget-alert root's check: Observe reads the alert back (FR-008), Undo has nothing
// to undo (the check writes nothing), Sweep finishes the run against its baseline (FR-007).
func Check(o Options) *live.ProbeCheck {
	return &live.ProbeCheck{
		Output: "budget_alert_check",
		Public: []string{"alert_id"},
		Observe: func(ctx context.Context, env map[string]string, term io.Writer) ([]byte, error) {
			return o.observe(ctx, env["alert_id"], term)
		},
		Undo:  func(context.Context, map[string]string, io.Writer) error { return nil },
		Sweep: o.sweep,
	}
}

// observation is the read-back record (data-model *Budget-alert probe check*); threshold and
// delay are absent when the alert could not be read.
type observation struct {
	AlertID       string `json:"alert_id"`
	ProjectSHA256 string `json:"project_sha256"`
	Threshold     *int64 `json:"monthly_threshold,omitempty"`
	Delay         *int64 `json:"delay,omitempty"`
	MatchesPlan   bool   `json:"matches_plan"`
}

func digest(id string) string {
	h := sha256.Sum256([]byte(id))
	return hex.EncodeToString(h[:])
}

// observe reads the alert in the run's project (Options.Project) with the sandbox admin and
// records what the API answers; a missing alert, or a value other than the plan's, is an error
// naming it, beside a record with matches_plan false.
func (o Options) observe(ctx context.Context, id string, term io.Writer) ([]byte, error) {
	c, err := live.NewAPIChecker(ctx, o.API, o.Admin)
	if err != nil {
		return nil, err
	}
	obs := observation{AlertID: id, ProjectSHA256: digest(o.Project)}
	a, err := c.Do(ctx, live.APICall{Op: live.APIGetAlert, Project: o.Project, ID: id})
	switch {
	case err != nil:
		return nil, fmt.Errorf("budget alert read-back: %w", err)
	case a.Status == http.StatusNotFound:
		return record(obs, fmt.Errorf("budget alert %s: not found (404) in the run's project: missing", id))
	case a.Alert == nil:
		return record(obs, fmt.Errorf("budget alert %s: the read-back answered status %d", id, a.Status))
	}
	obs.Threshold, obs.Delay = &a.Alert.MonthlyThreshold, &a.Alert.Delay
	email, err := accountEmail(ctx, c)
	if err != nil {
		return record(obs, err)
	}
	diffs := planDiffs(*a.Alert, email)
	obs.MatchesPlan = len(diffs) == 0
	fmt.Fprintf(term, "LZ-LIVE observe budget-alert %s: threshold %d, delay %d, matches plan %v\n", id, a.Alert.MonthlyThreshold, a.Alert.Delay, obs.MatchesPlan)
	if !obs.MatchesPlan {
		return record(obs, fmt.Errorf("budget alert %s differs from the plan: %s", id, strings.Join(diffs, "; ")))
	}
	return record(obs, nil)
}

func record(obs observation, err error) ([]byte, error) {
	raw, merr := json.Marshal(obs)
	if merr != nil {
		return nil, errors.Join(err, merr)
	}
	return raw, err
}

// accountEmail is the account's own address (GET /me), the plan's recipient; read through
// APIChecker.Read, so a Sweep's retry covers it (T027) and Observe sends it once.
func accountEmail(ctx context.Context, c *live.APIChecker) (string, error) {
	me, err := c.Read(ctx, live.APICall{Op: live.APIGetMe})
	switch {
	case err != nil:
		return "", fmt.Errorf("GET /me: %w", err)
	case me.Status < 200 || me.Status > 299:
		return "", fmt.Errorf("GET /me answered status %d", me.Status)
	case me.Email == "":
		return "", errors.New("GET /me answered no address")
	}
	return me.Email, nil
}

// planDiffs names each way a read alert differs from the root's plan; none means it matches. The
// recipients (email, deprecated, and emails) must all be the account's address, and there must be
// one. The address is never named.
func planDiffs(a live.APIAlert, email string) []string {
	var diffs []string
	if a.MonthlyThreshold != planThreshold {
		diffs = append(diffs, fmt.Sprintf("threshold %d (plan %d)", a.MonthlyThreshold, planThreshold))
	}
	if a.Delay != planDelay {
		diffs = append(diffs, fmt.Sprintf("delay %d (plan %d)", a.Delay, planDelay))
	}
	recipients := slices.DeleteFunc(append([]string{a.Email}, a.Emails...), func(s string) bool { return s == "" })
	if len(recipients) == 0 || slices.ContainsFunc(recipients, func(s string) bool { return s != email }) {
		diffs = append(diffs, "recipient is not the account's own address")
	}
	return diffs
}

// sweep classes the run from its files (live.LoadBaseline, live.ClassifyRun): pre-create passes
// with no request; untrusted is refused with the baseline's error before any request;
// baseline-backed deletes, in project (probe.env's, the runner's argument), the run's
// plan-matching alerts the baseline does not list.
func (o Options) sweep(ctx context.Context, runID, project string, term io.Writer) error {
	b, class, err := o.classify(runID, project)
	switch class {
	case live.RunPreCreate:
		fmt.Fprintf(term, "LZ-LIVE sweep budget-alert %s: pre-create run, nothing to delete\n", runID)
		return nil
	case live.RunUntrusted:
		return fmt.Errorf("budget-alert sweep of run %s refused, nothing deleted: %w", runID, err)
	}
	return o.sweepBacked(ctx, project, b.Listed[alertKind], term)
}

// classify reads the run's baseline (record copy, else recovery copy), state and inventory. A
// state file that cannot be examined counts as state; an unreadable inventory is untrusted.
func (o Options) classify(runID, project string) (live.Baseline, live.RunClass, error) {
	if !live.RunIDPattern.MatchString(runID) || o.RunDir == nil || o.StateDir == "" {
		return live.Baseline{}, live.RunUntrusted, fmt.Errorf("BASELINE_INVALID: run %q has no record or state directory", runID)
	}
	rec := o.RunDir(runID)
	b, loadErr := live.LoadBaseline(rec, filepath.Join(o.StateDir, "baselines", runID+".json"), runID, project)
	_, serr := os.Lstat(filepath.Join(o.StateDir, "probes", runID, stateFile))
	hasState := !errors.Is(serr, fs.ErrNotExist)
	inv, ierr := live.ReadInventory(rec)
	if ierr != nil && !errors.Is(ierr, fs.ErrNotExist) {
		return b, live.RunUntrusted, errors.Join(loadErr, fmt.Errorf("BASELINE_INVALID: inventory unreadable: %w", ierr))
	}
	return b, live.ClassifyRun(hasState, len(inv) > 0, b, loadErr), loadErr
}

// sweepBacked lists the project's alerts, deletes each one absent from the baseline that matches
// the plan, reports the others, and lists again: it fails while a deleted id is still listed, or
// when a non-baseline alert was reported or could not be read or deleted. Its reads (the listings,
// GET /me, each alert read) go through APIChecker.Read, which retries them with the leftover
// check's retry the runner lends the Sweep in ctx (spec 007 T027); a DELETE is sent once, a 404
// counted as gone.
func (o Options) sweepBacked(ctx context.Context, project string, before []string, term io.Writer) error {
	c, err := live.NewAPIChecker(ctx, o.API, o.Admin)
	if err != nil {
		return err
	}
	ids, err := listAlerts(ctx, c, project)
	if err != nil {
		return err
	}
	ids = slices.DeleteFunc(ids, func(id string) bool { return slices.Contains(before, id) })
	if len(ids) == 0 {
		return nil
	}
	email, err := accountEmail(ctx, c)
	if err != nil {
		return err
	}
	var deleted []string
	var errs []error
	for _, id := range ids {
		gone, err := deleteIfPlanned(ctx, c, project, id, email, term)
		if gone {
			deleted = append(deleted, id)
		}
		errs = append(errs, err)
	}
	if len(deleted) > 0 {
		errs = append(errs, stillListed(ctx, c, project, deleted))
	}
	return errors.Join(errs...)
}

func listAlerts(ctx context.Context, c *live.APIChecker, project string) ([]string, error) {
	a, err := c.Read(ctx, live.APICall{Op: live.APIListAlerts, Project: project})
	switch {
	case err != nil:
		return nil, fmt.Errorf("budget-alert sweep: %w", err)
	case a.Status < 200 || a.Status > 299:
		return nil, fmt.Errorf("budget-alert sweep: the alert listing answered status %d", a.Status)
	}
	return a.Alerts, nil
}

// deleteIfPlanned reads one non-baseline alert and deletes it if it matches the plan (gone true);
// an alert gone before the read needs nothing; any other is reported, kept and an error.
func deleteIfPlanned(ctx context.Context, c *live.APIChecker, project, id, email string, term io.Writer) (gone bool, err error) {
	a, err := c.Read(ctx, live.APICall{Op: live.APIGetAlert, Project: project, ID: id})
	switch {
	case err != nil:
		return false, fmt.Errorf("budget-alert sweep: %w", err)
	case a.Status == http.StatusNotFound:
		return false, nil
	case a.Alert == nil:
		return false, fmt.Errorf("budget-alert sweep: alert %s: the read answered status %d; kept", id, a.Status)
	}
	if diffs := planDiffs(*a.Alert, email); len(diffs) > 0 {
		fmt.Fprintf(term, "LZ-LIVE sweep budget-alert reported %s: not the run's plan (%s); kept\n", id, strings.Join(diffs, "; "))
		return false, fmt.Errorf("budget-alert sweep: alert %s is not in the baseline and does not match the plan (%s); kept for the owner", id, strings.Join(diffs, "; "))
	}
	d, err := c.Do(ctx, live.APICall{Op: live.APIDeleteAlert, Project: project, ID: id})
	switch {
	case err != nil:
		return false, fmt.Errorf("budget-alert sweep: %w", err)
	case (d.Status < 200 || d.Status > 299) && d.Status != http.StatusNotFound:
		return false, fmt.Errorf("budget-alert sweep: deleting alert %s answered status %d", id, d.Status)
	}
	fmt.Fprintf(term, "LZ-LIVE sweep budget-alert deleted %s\n", id)
	return true, nil
}

// stillListed lists the project's alerts again: an error names every deleted id still listed.
func stillListed(ctx context.Context, c *live.APIChecker, project string, deleted []string) error {
	ids, err := listAlerts(ctx, c, project)
	if err != nil {
		return err
	}
	var left []string
	for _, id := range deleted {
		if slices.Contains(ids, id) {
			left = append(left, id)
		}
	}
	if len(left) > 0 {
		return fmt.Errorf("budget-alert sweep: deleted alerts still listed: %s", strings.Join(left, ", "))
	}
	return nil
}
