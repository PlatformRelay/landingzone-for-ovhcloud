// Package state is the state-backend probe's check (spec 005 T096): whether OVHcloud enforces the
// Deny statement of the state user's S3 policy (T094: `s3:DeleteObjectVersion*`,
// `s3:PutBucketPolicy*`, `s3:PutBucketVersioning` among others), and whether the OpenTofu S3
// backend's own calls stay allowed under it.
//
// lz-live runs it on tests/live/probes/state-backend after the root's apply (live.ProbeCheck):
//
//   - Observe, with the state user's credential: two PUTs of one object (two versions), a GET, a
//     DELETE of the older version by its versionId, the same with the governance bypass (T099), a
//     bucket-policy PUT, a versioning PUT (Suspended), a DELETE without versionId; then, with the
//     janitor's (T099), a DELETE of the newer version without and with the bypass: the bucket
//     carries Object Lock in governance mode, so the first is refused while the retention holds
//     and the second is allowed. Each answer is an observation: expected, observed (allowed = 2xx,
//     refused = any other status), status, S3 error code. A refusal is as expected only with
//     AccessDenied. An answer other than the expected one is a finding: recorded, listed and
//     printed, never an error — an accepted versioned DELETE is the owner's to judge (ADR-0009).
//     Observe fails only when a step could not run: no answer, or no two distinct version ids to
//     address a version by.
//   - Undo, with the janitor's credential (`s3:*` and s3:BypassGovernanceRetention on the probe
//     bucket only): delete the bucket policy if one is set, enable versioning if it is not
//     Enabled, delete every version and delete marker in the bucket with the governance bypass,
//     then list again; anything left is an error, and lz-live keeps the root for `--cleanup`. It
//     needs nothing from Observe, so it also runs after an interrupted Observe and on a cleanup.
//
// The credentials come from the root's sensitive output deny_check; lz-live has added them to its
// redactor. Nothing here prints or records a key or the server's text: of an answer only the
// status and a well-formed S3 error code (live.S3Checker), and a listing is parsed, never quoted.
package state

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

// Output is the probe root's sensitive output that publishes the check's variables.
const Output = "deny_check"

// Object is the one key the check writes, shaped like the S3 backend's lock file.
const Object = "lz-deny-check/probe.tflock"

// maxVersions bounds what Undo deletes: the check writes three versions at most, so more means
// a bucket the check did not fill.
const maxVersions = 100

// Public are the keys of Output whose values are no secret; lz-live redacts every other value.
var Public = []string{"bucket", "region"}

// envKeys are the keys Output must publish, and the only ones.
var envKeys = []string{"bucket", "janitor_key_id", "janitor_secret", "region", "state_key_id", "state_secret"}

// probeBucket is the state-backend probe's bucket name (lzprobe-state-<run id, lowercased>): Undo
// empties the whole bucket, so it never runs on any other.
var probeBucket = regexp.MustCompile(`^lzprobe-state-[0-9]{8}t[0-9]{6}z-[0-9a-f]{4}$`)

// Options are the S3 client's seams; the zero value is production.
type Options struct {
	HTTP *http.Client     // nil: the S3 client's default
	Now  func() time.Time // nil: time.Now
}

// Observation is one step's answer as the run record keeps it.
type Observation struct {
	Step       string `json:"step"`
	Operation  string `json:"operation"`
	Credential string `json:"credential"`
	Expected   string `json:"expected"`
	Observed   string `json:"observed"`
	Status     int    `json:"status,omitempty"`
	Code       string `json:"code,omitempty"`
	AsExpected bool   `json:"as_expected"`
}

// Record is observations.json.
type Record struct {
	Check        string        `json:"check"`
	Object       string        `json:"object"`
	Observations []Observation `json:"observations"`
	Findings     []string      `json:"findings"`
}

// Observed values.
const (
	allowed  = "allowed"
	refused  = "refused"
	notRun   = "not-run"
	noAnswer = "no-answer"
	// skipped: the step's version was already deleted by an earlier step of the run (review r1).
	skipped = "skipped"
)

// s3 is the one method of live.S3Checker the check uses.
type s3 interface {
	Do(context.Context, live.S3Call) (live.S3Answer, error)
}

// Check is the probe check lz-live runs on the state-backend root.
func Check(o Options) *live.ProbeCheck {
	return &live.ProbeCheck{
		Output: Output,
		Public: Public,
		Validate: func(env map[string]string) error {
			_, _, err := o.open(env)
			return err
		},
		Observe: func(ctx context.Context, env map[string]string, term io.Writer) ([]byte, error) {
			st, jan, err := o.open(env)
			if err != nil {
				return nil, err
			}
			rec, err := observe(ctx, map[string]s3{"state": st, "janitor": jan}, env["bucket"], term)
			raw, jerr := json.Marshal(rec)
			return raw, errors.Join(err, jerr)
		},
		Undo: func(ctx context.Context, env map[string]string, term io.Writer) error {
			_, jan, err := o.open(env)
			if err != nil {
				return err
			}
			return undo(ctx, jan, env["bucket"], term)
		},
	}
}

// open checks the published variables and opens the state user's and the janitor's clients.
// Refusals name keys, never values.
func (o Options) open(env map[string]string) (state, janitor s3, err error) {
	if got := slices.Sorted(maps.Keys(env)); !slices.Equal(got, envKeys) {
		return nil, nil, fmt.Errorf("output %s publishes the keys %s, want %s", Output, strings.Join(got, ", "), strings.Join(envKeys, ", "))
	}
	for _, k := range envKeys {
		if env[k] == "" {
			return nil, nil, fmt.Errorf("output %s publishes an empty %s", Output, k)
		}
	}
	if !probeBucket.MatchString(env["bucket"]) {
		return nil, nil, fmt.Errorf("output %s names bucket %q, not a state-backend probe bucket", Output, env["bucket"])
	}
	client := func(who string) (s3, error) {
		c, err := live.NewS3Checker(live.S3Options{Region: env["region"], HTTP: o.HTTP, Now: o.Now,
			Keys: map[string]string{"AWS_ACCESS_KEY_ID": env[who+"_key_id"], "AWS_SECRET_ACCESS_KEY": env[who+"_secret"]}})
		if err != nil {
			return nil, fmt.Errorf("%s credential: %w", who, err)
		}
		return c, nil
	}
	if state, err = client("state"); err != nil {
		return nil, nil, err
	}
	if janitor, err = client("janitor"); err != nil {
		return nil, nil, err
	}
	return state, janitor, nil
}

// step is one observation to make: by whom (state or janitor), and which of the two written
// versions its call addresses by id (0: none).
type step struct {
	name     string
	who      string
	call     live.S3Call
	version  int
	expected string
}

// observe makes the ten observations in order; every step runs (unless the context ended or the
// step needs version ids it does not have), so one failure does not hide the others.
func observe(ctx context.Context, clients map[string]s3, bucket string, term io.Writer) (Record, error) {
	rec := Record{Check: "state-backend-deny", Object: Object, Observations: []Observation{}, Findings: []string{}}
	obj := func(op live.S3Op, body []byte) live.S3Call {
		return live.S3Call{Op: op, Bucket: bucket, Key: Object, Body: body}
	}
	// A bucket policy that grants nothing: if the PUT is accepted, it denies everyone CORS changes
	// until Undo deletes it.
	policy := []byte(`{"Version":"2012-10-17","Statement":[{"Sid":"LzProbeHarmless","Effect":"Deny","Principal":"*","Action":"s3:PutBucketCORS","Resource":"arn:aws:s3:::` + bucket + `"}]}`)
	suspend := []byte(`<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Suspended</Status></VersioningConfiguration>`)
	var versions []string
	deleted := map[int]bool{} // versions (1, 2) an allowed versioned DELETE of this run removed
	var errs []error
	bypass := obj(live.S3DeleteObject, nil)
	bypass.Bypass = true
	for _, s := range []step{
		{"put-v1", "state", obj(live.S3PutObject, []byte("lz-deny-check v1\n")), 0, allowed},
		{"put-v2", "state", obj(live.S3PutObject, []byte("lz-deny-check v2\n")), 0, allowed},
		{"get", "state", obj(live.S3GetObject, nil), 0, allowed},
		{"delete-older-version", "state", obj(live.S3DeleteObject, nil), 1, refused},
		// T099: the state user may not bypass the governance retention (its Deny on
		// s3:BypassGovernanceRetention, and no grant of it).
		{"delete-older-version-bypass", "state", bypass, 1, refused},
		{"put-bucket-policy", "state", live.S3Call{Op: live.S3PutBucketPolicy, Bucket: bucket, Body: policy}, 0, refused},
		{"suspend-versioning", "state", live.S3Call{Op: live.S3PutBucketVersioning, Bucket: bucket, Body: suspend}, 0, refused},
		{"delete-current", "state", obj(live.S3DeleteObject, nil), 0, allowed},
		// T099: the retention holds even for the janitor's `s3:*` unless it asks for the bypass,
		// which it may (s3:BypassGovernanceRetention). The newer version, so that what the state
		// user may have deleted does not decide these answers.
		{"janitor-delete-locked", "janitor", obj(live.S3DeleteObject, nil), 2, refused},
		{"janitor-delete-bypass", "janitor", bypass, 2, allowed},
	} {
		o := Observation{Step: s.name, Operation: string(s.call.Op), Credential: s.who, Expected: s.expected, Observed: notRun}
		switch {
		case ctx.Err() != nil:
			errs = append(errs, fmt.Errorf("%s: not run: %w", s.name, ctx.Err()))
		case s.version > 0 && (len(versions) != 2 || versions[0] == versions[1] || slices.Contains(versions, "null")):
			errs = append(errs, fmt.Errorf("%s: not run: the two PUTs answered no two distinct version ids (is the bucket versioned?)", s.name))
		case s.version > 0 && deleted[s.version]:
			// Review r1: an earlier step of this run deleted the version (the lock was not in force,
			// itself a finding); a DELETE of it now would say nothing about the bypass.
			o.Observed = skipped
		default:
			if s.version > 0 {
				s.call.VersionID = versions[s.version-1]
			}
			a, err := clients[s.who].Do(ctx, s.call)
			if err != nil {
				o.Observed = noAnswer
				errs = append(errs, fmt.Errorf("%s: %w", s.name, err))
				break
			}
			o.Status, o.Code, o.Observed = a.Status, a.Code, refused
			if a.Status >= 200 && a.Status <= 299 {
				o.Observed = allowed
				if s.call.Op == live.S3PutObject && a.VersionID != "" {
					versions = append(versions, a.VersionID)
				}
				if s.version > 0 {
					deleted[s.version] = true
				}
			}
			o.AsExpected = o.Observed == s.expected && (s.expected != refused || a.Code == "AccessDenied")
		}
		rec.Observations = append(rec.Observations, o)
		fmt.Fprintf(term, "LZ-LIVE observe %s %s %d %s\n", o.Step, o.Observed, o.Status, o.Code)
		if (o.Observed == allowed || o.Observed == refused) && !o.AsExpected {
			rec.Findings = append(rec.Findings, o.Step)
			fmt.Fprintf(term, "LZ-LIVE finding %s: expected %s, observed %s %d %s\n", o.Step, o.Expected, o.Observed, o.Status, o.Code)
		}
	}
	return rec, errors.Join(errs...)
}

// listing is what Undo reads of ListObjectVersions.
type listing struct {
	XMLName   xml.Name
	Truncated bool    `xml:"IsTruncated"`
	Versions  []entry `xml:"Version"`
	Markers   []entry `xml:"DeleteMarker"`
}

type entry struct {
	Key       string `xml:"Key"`
	VersionID string `xml:"VersionId"`
}

// list reads the bucket's versions and delete markers (one page; a truncated one is an error).
func list(ctx context.Context, jan s3, bucket string) ([]entry, error) {
	a, err := jan.Do(ctx, live.S3Call{Op: live.S3ListObjectVersions, Bucket: bucket})
	if err != nil {
		return nil, err
	}
	if a.Status != http.StatusOK {
		return nil, fmt.Errorf("ListObjectVersions answered %d %s", a.Status, a.Code)
	}
	var l listing
	if xml.Unmarshal(a.Body, &l) != nil || l.XMLName.Local != "ListVersionsResult" {
		return nil, errors.New("ListObjectVersions: the answer is not a version listing")
	}
	if l.Truncated {
		return nil, errors.New("ListObjectVersions: the listing is truncated")
	}
	entries := append(l.Versions, l.Markers...)
	// Without its version id a DELETE would only add a delete marker.
	for _, e := range entries {
		if e.VersionID == "" {
			return nil, errors.New("ListObjectVersions: a listed entry has no version id")
		}
	}
	return entries, nil
}

// undo restores the bucket the check may have changed and empties it.
func undo(ctx context.Context, jan s3, bucket string, term io.Writer) error {
	a, err := jan.Do(ctx, live.S3Call{Op: live.S3GetBucketPolicy, Bucket: bucket})
	switch {
	case err != nil:
		return fmt.Errorf("undo: %w", err)
	case a.Status == http.StatusOK:
		d, err := jan.Do(ctx, live.S3Call{Op: live.S3DeleteBucketPolicy, Bucket: bucket})
		if err != nil {
			return fmt.Errorf("undo: %w", err)
		}
		if d.Status < 200 || d.Status > 299 {
			return fmt.Errorf("undo: DeleteBucketPolicy answered %d %s", d.Status, d.Code)
		}
	case a.Status == http.StatusNotFound, a.Status == http.StatusNotImplemented:
		// No bucket policy, or none possible.
	default:
		return fmt.Errorf("undo: GetBucketPolicy answered %d %s", a.Status, a.Code)
	}

	a, err = jan.Do(ctx, live.S3Call{Op: live.S3GetBucketVersioning, Bucket: bucket})
	if err != nil {
		return fmt.Errorf("undo: %w", err)
	}
	if a.Status != http.StatusOK {
		return fmt.Errorf("undo: GetBucketVersioning answered %d %s", a.Status, a.Code)
	}
	var vc struct {
		XMLName xml.Name
		Status  string `xml:"Status"`
	}
	if xml.Unmarshal(a.Body, &vc) != nil || vc.XMLName.Local != "VersioningConfiguration" {
		return errors.New("undo: GetBucketVersioning: the answer is not a versioning configuration")
	}
	if vc.Status != "Enabled" {
		body := []byte(`<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Enabled</Status></VersioningConfiguration>`)
		p, err := jan.Do(ctx, live.S3Call{Op: live.S3PutBucketVersioning, Bucket: bucket, Body: body})
		if err != nil {
			return fmt.Errorf("undo: %w", err)
		}
		if p.Status < 200 || p.Status > 299 {
			return fmt.Errorf("undo: PutBucketVersioning answered %d %s", p.Status, p.Code)
		}
	}

	entries, err := list(ctx, jan, bucket)
	if err != nil {
		return fmt.Errorf("undo: %w", err)
	}
	if len(entries) > maxVersions {
		return fmt.Errorf("undo: %d versions in the bucket, more than the check writes (%d); none deleted", len(entries), maxVersions)
	}
	for i, e := range entries {
		// T099: the probe bucket locks every version (governance): each DELETE asks for the bypass.
		d, err := jan.Do(ctx, live.S3Call{Op: live.S3DeleteObject, Bucket: bucket, Key: e.Key, VersionID: e.VersionID, Bypass: true})
		if err != nil {
			// The key is the server's text, and the client's error names it: only the position.
			return fmt.Errorf("undo: DeleteObject of listed version %d of %d: refused or no answer", i+1, len(entries))
		}
		if d.Status < 200 || d.Status > 299 {
			return fmt.Errorf("undo: DeleteObject of a version answered %d %s", d.Status, d.Code)
		}
	}
	left, err := list(ctx, jan, bucket)
	if err != nil {
		return fmt.Errorf("undo: %w", err)
	}
	if len(left) > 0 {
		return fmt.Errorf("undo: %d versions remain in the bucket after deleting %d", len(left), len(entries))
	}
	fmt.Fprintf(term, "LZ-LIVE undo removed %d versions; bucket empty\n", len(entries))
	return nil
}
