// Package identity holds the identity probes' checks (spec 003 T031–T034): what an OVHcloud OAuth2
// identity may still do, observed by lz-live after the probe root's apply.
package identity

// The token-residual check (spec 003 T031, T032): how long an access token already issued to an
// OAuth2 client stays usable after the client is deleted (FR-007, C007.5, C007.6; UNVERIFIED until
// T026). lz-live runs it on tests/live/probes/token-residual, whose sensitive output
// residual_check publishes run_id, project_id and read_path (data-model.md *Probe checks*). The
// check, not the root, creates and deletes the client and policy lzprobe-token-residual-<run id>.
//
//   - Observe creates, with the sandbox admin, the client (flow CLIENT_CREDENTIALS) and a policy
//     that allows the client's identity exactly publicCloudProject:apiovh:get on exactly the
//     project's URN, the IAM action of GET /cloud/project/{serviceName} (kb api/v1/cloud.json). It
//     takes ONE token with the client's credentials and reads read_path with it until the read is
//     allowed (the same-route control, retried within Settle). Then it deletes the client and
//     repeats the read with the same token, at once and then every Interval, until the first
//     refusal at or after the token's expiry, a read allowed after the expiry, Confirm reads after
//     a refusal before the expiry, the run's deadline, or the first poll after the expiry (so no
//     poll is later than the expiry plus one interval). ResidualWindow judges what was observed.
//     A blocked result fails Observe; both windows are observations, not failures.
//   - Undo and Sweep list the clients and policies of the exact name with the sandbox admin,
//     delete each and list again; anything left fails them. A client or policy already gone
//     (absent from the listing, or a delete answering 404) is undone, so a second Undo, or an
//     Undo after a Sweep, passes.
//
// Nothing here prints or records a secret, a token or the server's text: of the API the check
// keeps statuses and times (live.APIChecker keeps a created client's secret in memory for the one
// token request), and its errors name the run's client name and statuses only.

import (
	"context"
	"encoding/json"
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

// Options are the identity checks' seams; lz-live fills them from the run.
type Options struct {
	API   live.API
	Admin live.Credential // creates, lists and deletes the disposable clients and policies
	// Bind: nil binds nothing. The token-residual check never binds its client: a bind takes a
	// token of its own, and the check's claim is about exactly one token.
	Bind     func(context.Context, live.Credential) error
	Now      func() time.Time
	Sleep    func(context.Context, time.Duration) error // nil: a timer that honours ctx
	Interval time.Duration                              // between polls and control retries; zero: 30 s
	Settle   time.Duration                              // the bound on control retries (both checks); zero: 5 min
}

// Poll is one granted read with the issued token.
type Poll struct {
	At     time.Time
	Status int // 0: no answer
	Err    error
}

// Residual is the judged window of a token whose client was deleted.
type Residual struct {
	Observed string // "refused-before-expiry", "valid-until-expiry", "blocked"
	// Reason (blocked): "expiry", "control", "outages", "deadline", "flapping"; Observe's record
	// adds "setup" (client, policy or token not obtained) and "delete" (the client delete failed).
	Reason    string
	Lower     time.Duration // delete to the last allowed poll ("valid ≥ Lower")
	Upper     time.Duration // delete to the first refusal; zero when no refusal bounds the window
	ExpiresIn time.Duration // the validated expires_in of the token answer
}

// The check's fixed names and bounds.
const (
	ResidualOutput = "residual_check"
	// ResidualAction is the one action the client's policy allows: GET /cloud/project/{serviceName}
	// (kb api/v1/cloud.json, iamActions).
	ResidualAction = "publicCloudProject:apiovh:get"
	// MaxExpiresIn is the longest token lifetime the check accepts.
	MaxExpiresIn = 24 * time.Hour
	// Confirm is how many reads follow a refusal before the expiry: a read allowed again among
	// them is "blocked (flapping)", never a window (T031 left the choice to T032).
	Confirm = 3

	refusedBefore = "refused-before-expiry"
	validUntil    = "valid-until-expiry"
	blocked       = "blocked"
)

// ResidualName is the client and policy name of a run.
func ResidualName(runID string) string { return "lzprobe-token-residual-" + runID }

func allowed(p Poll) bool { return p.Err == nil && p.Status >= 200 && p.Status <= 299 }

func refused(p Poll) bool {
	return p.Err == nil && (p.Status == http.StatusUnauthorized || p.Status == http.StatusForbidden)
}

// outage: no answer, or an answer other than 2xx, 401 or 403.
func outage(p Poll) bool { return !allowed(p) && !refused(p) }

// ResidualWindow judges only observed polls: an outage bounds nothing, and no validity is
// inferred across a tail of outages or past a deadline stop. In order:
//   - expires_in must be above zero and at most 24 h, else "blocked (expiry)" with no bounds;
//   - a control that is not allowed is "blocked (control)";
//   - a read allowed after the expiry is "blocked (expiry)": the token outlived its stated
//     lifetime. A read allowed exactly at the expiry instant is within it (expires_in is whole
//     seconds, so at the instant either answer is consistent; a refusal there is likewise
//     valid-until-expiry);
//   - no refusal: "blocked", "deadline" when the deadline stopped the polls, else "outages";
//   - a read allowed again after the first refusal is "blocked (flapping)", Lower up to that read:
//     the refusal did not end the window;
//   - the first refusal before the expiry is refused-before-expiry; at or after it,
//     valid-until-expiry when the poll just before it was allowed, else "blocked (outages)".
//
// A deadline stop after a refusal does not change the judgement: the refusal was observed.
func ResidualWindow(issued, deleted time.Time, expiresIn time.Duration, control Poll, polls []Poll, deadlineHit bool) Residual {
	if expiresIn <= 0 || expiresIn > MaxExpiresIn {
		return Residual{Observed: blocked, Reason: "expiry"}
	}
	r := Residual{ExpiresIn: expiresIn}
	if !allowed(control) {
		r.Observed, r.Reason = blocked, "control"
		return r
	}
	expiry := issued.Add(expiresIn)
	lastAllowed := func(ps []Poll) time.Duration {
		for i := len(ps) - 1; i >= 0; i-- {
			if allowed(ps[i]) {
				return ps[i].At.Sub(deleted)
			}
		}
		return 0
	}
	if slices.ContainsFunc(polls, func(p Poll) bool { return allowed(p) && p.At.After(expiry) }) {
		r.Observed, r.Reason = blocked, "expiry"
		return r
	}
	first := slices.IndexFunc(polls, refused)
	if first < 0 {
		r.Observed, r.Reason, r.Lower = blocked, "outages", lastAllowed(polls)
		if deadlineHit {
			r.Reason = "deadline"
		}
		return r
	}
	if slices.ContainsFunc(polls[first+1:], allowed) {
		r.Observed, r.Reason, r.Lower = blocked, "flapping", lastAllowed(polls)
		return r
	}
	r.Lower = lastAllowed(polls[:first])
	upper := polls[first].At.Sub(deleted)
	switch {
	case polls[first].At.Before(expiry):
		r.Observed, r.Upper = refusedBefore, upper
	case first > 0 && allowed(polls[first-1]):
		r.Observed, r.Upper = validUntil, upper
	default:
		r.Observed, r.Reason = blocked, "outages"
	}
	return r
}

// ResidualPoll is one read as observations.json keeps it.
type ResidualPoll struct {
	At         time.Time `json:"at"` // sent
	AnsweredAt time.Time `json:"answered_at"`
	Status     int       `json:"status"` // 0: no answer
	Outage     bool      `json:"outage"`
}

// ResidualRecord is the token-residual check's observations.json.
type ResidualRecord struct {
	Check  string `json:"check"`
	RunID  string `json:"run_id"`
	Client string `json:"client"` // the client and policy name
	// RequestedAt and IssuedAt: when the token request was sent and its answer arrived; the
	// token's real expiry lies between them plus expires_in.
	RequestedAt *time.Time `json:"requested_at"`
	IssuedAt    *time.Time `json:"issued_at"`
	ExpiresIn   *int64     `json:"expires_in"` // seconds, as answered; null when not an integer
	// DeleteRequestedAt and DeletedAt: when the client delete was sent and acknowledged.
	DeleteRequestedAt *time.Time     `json:"delete_requested_at"`
	DeletedAt         *time.Time     `json:"deleted_at"`
	Control           []ResidualPoll `json:"control"`
	Polls             []ResidualPoll `json:"polls"`
	Observed          string         `json:"observed"`
	Reason            string         `json:"reason,omitempty"`
	Lower             int64          `json:"lower_seconds"`
	Upper             int64          `json:"upper_seconds"`
}

var (
	residualPublic = []string{"project_id", "read_path", "run_id"}
	projectShape   = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

// TokenResidual is the probe check lz-live runs on the token-residual root.
func TokenResidual(o Options) *live.ProbeCheck {
	return &live.ProbeCheck{
		Output:   ResidualOutput,
		Public:   slices.Clone(residualPublic),
		Validate: validateResidual,
		Observe: func(ctx context.Context, env map[string]string, term io.Writer) ([]byte, error) {
			if err := validateResidual(env); err != nil {
				return nil, err
			}
			rec, err := o.observe(ctx, env, term)
			raw, jerr := json.Marshal(rec)
			return raw, errors.Join(err, jerr)
		},
		Undo: func(ctx context.Context, env map[string]string, term io.Writer) error {
			if err := validateResidual(env); err != nil {
				return err
			}
			return o.cleanup(ctx, env["run_id"], term, "undo")
		},
		Sweep: o.sweep,
	}
}

// validateResidual refuses published variables that are not the token-residual root's of one
// run: exactly the three keys, none empty, a run id, a project id, and read_path the project's
// own path. The policy allows only the project read, so read_path is exactly
// /cloud/project/<project_id> (T031's tests admit no other; a path below it would be refused).
func validateResidual(env map[string]string) error {
	if got := slices.Sorted(maps.Keys(env)); !slices.Equal(got, residualPublic) {
		return fmt.Errorf("output %s publishes the keys %s, want %s", ResidualOutput, strings.Join(got, ", "), strings.Join(residualPublic, ", "))
	}
	for _, k := range residualPublic {
		if env[k] == "" {
			return fmt.Errorf("output %s publishes an empty %s", ResidualOutput, k)
		}
	}
	switch {
	case !live.RunIDPattern.MatchString(env["run_id"]):
		return fmt.Errorf("output %s: run_id is not a run id", ResidualOutput)
	case !projectShape.MatchString(env["project_id"]):
		return fmt.Errorf("output %s: project_id is not a project id", ResidualOutput)
	case env["read_path"] != "/cloud/project/"+env["project_id"]:
		return fmt.Errorf("output %s: read_path is not /cloud/project/<project_id>", ResidualOutput)
	}
	return nil
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now().UTC()
	}
	return time.Now().UTC()
}

func (o Options) sleep(ctx context.Context, d time.Duration) error {
	if o.Sleep != nil {
		return o.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (o Options) interval() time.Duration {
	if o.Interval > 0 {
		return o.Interval
	}
	return 30 * time.Second
}

func (o Options) settle() time.Duration {
	if o.Settle > 0 {
		return o.Settle
	}
	return 5 * time.Minute
}

func ok2xx(status int) bool { return status >= 200 && status <= 299 }

// answered says how a call went, without any answer text.
func answered(a live.APIAnswer, err error) string {
	if err != nil {
		return "gave no usable answer"
	}
	return fmt.Sprintf("answered %d", a.Status)
}

func recPoll(p Poll, answered time.Time) ResidualPoll {
	return ResidualPoll{At: p.At, AnsweredAt: answered, Status: p.Status, Outage: outage(p)}
}

// observe runs the probe; the record is complete whatever fails.
func (o Options) observe(ctx context.Context, env map[string]string, term io.Writer) (ResidualRecord, error) {
	name := ResidualName(env["run_id"])
	rec := ResidualRecord{Check: "token-residual", RunID: env["run_id"], Client: name, Control: []ResidualPoll{}, Polls: []ResidualPoll{}, Observed: blocked}
	stop := func(reason string, err error) (ResidualRecord, error) {
		rec.Reason = reason
		fmt.Fprintf(term, "LZ-LIVE observe token-residual blocked (%s)\n", reason)
		return rec, fmt.Errorf("token-residual: blocked (%s): %w", reason, err)
	}
	admin, err := live.NewAPIChecker(ctx, o.API, o.Admin)
	if err != nil {
		return stop("setup", fmt.Errorf("admin identity: %w", err))
	}
	c, err := admin.Do(ctx, live.APICall{Op: live.APICreateClient, Name: name})
	if err != nil || !ok2xx(c.Status) || len(c.Clients) != 1 {
		return stop("setup", fmt.Errorf("the create of client %s %s", name, answered(c, err)))
	}
	id, secret := c.Clients[0].ID, c.Secret
	g, err := admin.Do(ctx, live.APICall{Op: live.APIGetClient, ID: id})
	if err != nil || !ok2xx(g.Status) || len(g.Clients) != 1 || g.Clients[0].Identity == "" {
		return stop("setup", fmt.Errorf("the identity of client %s is unknown: its read %s", name, answered(g, err)))
	}
	identity := g.Clients[0].Identity
	// The project's URN in the identity's region (urn:v1:<region>:…; the shape is gated by
	// live.APIChecker), as tools/internal/stacks writes it.
	region := strings.Split(identity, ":")[2]
	body, _ := json.Marshal(map[string]any{
		"name": name, "description": name, "identities": []string{identity},
		"resources":   []map[string]string{{"urn": "urn:v1:" + region + ":resource:publicCloudProject:" + env["project_id"]}},
		"permissions": map[string]any{"allow": []map[string]string{{"action": ResidualAction}}},
	})
	p, err := admin.Do(ctx, live.APICall{Op: live.APICreatePolicy, Body: body})
	if err != nil || !ok2xx(p.Status) {
		return stop("setup", fmt.Errorf("the create of policy %s %s", name, answered(p, err)))
	}
	fmt.Fprintf(term, "LZ-LIVE observe token-residual created client and policy %s\n", name)
	requested := o.now()
	rec.RequestedAt = &requested
	probe, exp, err := live.NewAPICheckerExpiry(ctx, o.API, live.Credential{Endpoint: o.Admin.Endpoint, ClientID: id, ClientSecret: secret})
	if err != nil {
		return stop("setup", fmt.Errorf("the client's token: %w", err))
	}
	issued := o.now()
	rec.IssuedAt = &issued
	var expiresIn time.Duration
	if exp.Given {
		s := exp.Seconds
		rec.ExpiresIn = &s
		if s > 0 && s <= int64(MaxExpiresIn/time.Second) {
			expiresIn = time.Duration(s) * time.Second
		}
	}
	if expiresIn == 0 {
		return stop("expiry", errors.New("the token answer's expires_in is not an integer of 1 s to 24 h; nothing deleted"))
	}
	fmt.Fprintf(term, "LZ-LIVE observe token-residual token issued, expires_in %d s\n", exp.Seconds)

	// read sends the granted read: At is when it was sent, answered when its answer came back.
	read := func() (Poll, time.Time) {
		at := o.now()
		a, err := probe.Do(ctx, live.APICall{Op: live.APIGetProject, Project: env["project_id"]})
		if err != nil {
			return Poll{At: at, Err: err}, o.now()
		}
		return Poll{At: at, Status: a.Status}, o.now()
	}
	// until sleeps to the absolute instant t (a read's latency is not added to the interval).
	until := func(t time.Time) error { return o.sleep(ctx, max(0, t.Sub(o.now()))) }
	interval, settle := o.interval(), o.settle()
	var control Poll
	for {
		var answeredAt time.Time
		control, answeredAt = read()
		rec.Control = append(rec.Control, recPoll(control, answeredAt))
		fmt.Fprintf(term, "LZ-LIVE observe token-residual control %d\n", control.Status)
		if allowed(control) {
			break
		}
		// Retry while the next control, sent one interval after this one, stays within Settle.
		next := control.At.Add(interval)
		if next.Sub(rec.Control[0].At) > settle {
			return stop("control", fmt.Errorf("the read was not allowed within %s; client %s not deleted", settle, name))
		}
		if err := until(next); err != nil {
			return stop("deadline", fmt.Errorf("the run ended before the control was allowed; client %s not deleted", name))
		}
		// A read slower than the interval leaves the schedule behind: never send past Settle
		// (review r2).
		if o.now().Sub(rec.Control[0].At) > settle {
			return stop("control", fmt.Errorf("the read was not allowed within %s; client %s not deleted", settle, name))
		}
	}

	deleteSent := o.now()
	rec.DeleteRequestedAt = &deleteSent
	d, err := admin.Do(ctx, live.APICall{Op: live.APIDeleteClient, ID: id})
	if err != nil || !ok2xx(d.Status) {
		return stop("delete", fmt.Errorf("the delete of client %s %s: no window", name, answered(d, err)))
	}
	deleted := o.now()
	rec.DeletedAt = &deleted
	fmt.Fprintf(term, "LZ-LIVE observe token-residual client deleted; polling every %s\n", interval)
	expiry := issued.Add(expiresIn)
	var polls []Poll
	var answers []time.Time
	deadlineHit, first := false, -1
	for {
		pl, answeredAt := read()
		polls, answers = append(polls, pl), append(answers, answeredAt)
		rec.Polls = append(rec.Polls, recPoll(pl, answeredAt))
		fmt.Fprintf(term, "LZ-LIVE observe token-residual poll +%ds %d\n", int64(pl.At.Sub(deleted)/time.Second), pl.Status)
		n := len(polls) - 1
		if first < 0 && refused(pl) {
			first = n
		}
		if allowed(pl) && pl.At.After(expiry) || // outlived its expiry
			first == n && !pl.At.Before(expiry) || // refused at or after the expiry
			first >= 0 && n > first && (allowed(pl) || n-first >= Confirm) || // flapping, or confirmed
			pl.At.After(expiry) { // a next poll would be later than the expiry plus one interval
			break
		}
		if err := until(pl.At.Add(interval)); err != nil {
			deadlineHit = true
			break
		}
		// A read slower than the interval leaves the schedule behind: never send past the expiry
		// plus one interval (review r2).
		if o.now().After(expiry.Add(interval)) {
			break
		}
	}
	// The token's real expiry lies between requested+expires_in and issued+expires_in (the server
	// issued it while the request was in flight). ResidualWindow compares against the later edge,
	// so an allowed read after it outlived the token for certain; a refusal is "before the
	// expiry" only if its answer came back before the earlier edge. One that may have been
	// processed at or after the real expiry is judged as a refusal at the expiry (review r1).
	//
	// Upper, when there is one, runs from the delete's request to the refusal's answer, the edges
	// that cannot understate it; Lower from the delete's acknowledgement to the allowed read's
	// send, the edges that cannot overstate it (review r2).
	w := ResidualWindow(issued, deleted, expiresIn, control, polls, deadlineHit)
	f := slices.IndexFunc(polls, refused)
	if w.Observed == refusedBefore && !answers[f].Before(requested.Add(expiresIn)) {
		polls[f].At = expiry
		w = ResidualWindow(issued, deleted, expiresIn, control, polls, deadlineHit)
	}
	if w.Observed == refusedBefore || w.Observed == validUntil {
		w.Upper = answers[f].Sub(deleteSent)
	}
	rec.Observed, rec.Reason = w.Observed, w.Reason
	rec.Lower, rec.Upper = int64(w.Lower/time.Second), int64(w.Upper/time.Second)
	if w.Observed == blocked {
		fmt.Fprintf(term, "LZ-LIVE observe token-residual blocked (%s), valid ≥ %d s\n", w.Reason, rec.Lower)
		return rec, fmt.Errorf("token-residual: blocked (%s): C007.5 not shown", w.Reason)
	}
	fmt.Fprintf(term, "LZ-LIVE observe token-residual %s: valid ≥ %d s, refused by %d s after the delete (expires_in %d s)\n", w.Observed, rec.Lower, rec.Upper, exp.Seconds)
	return rec, nil
}

// sweep is the check's Sweep (data-model.md *Probe checks*): it deletes, by listing the exact name
// lzprobe-token-residual-<run id> with the sandbox admin, the client and policy the check may have
// created, whether or not an output or a record exists.
func (o Options) sweep(ctx context.Context, runID, projectID string, term io.Writer) error {
	if !live.RunIDPattern.MatchString(runID) {
		return errors.New("sweep token-residual: the run id is not a run id")
	}
	if !projectShape.MatchString(projectID) {
		return errors.New("sweep token-residual: the project is not a project id")
	}
	return o.cleanup(ctx, runID, term, "sweep")
}

// cleanup lists the clients and policies of the run's exact name, deletes each, and lists again.
// A failed listing or delete does not stop the others; the second listing decides what is left.
// A delete answering 404 is gone already.
func (o Options) cleanup(ctx context.Context, runID string, term io.Writer, verb string) error {
	name := ResidualName(runID)
	admin, err := live.NewAPIChecker(ctx, o.API, o.Admin)
	if err != nil {
		return fmt.Errorf("%s token-residual %s: admin identity: %w", verb, name, err)
	}
	list := func() (clients, policies []string, errs []error) {
		a, err := admin.Do(ctx, live.APICall{Op: live.APIListClients, Name: name})
		if err != nil || !ok2xx(a.Status) {
			errs = append(errs, fmt.Errorf("the client listing %s", answered(a, err)))
		} else {
			for _, c := range a.Clients {
				clients = append(clients, c.ID)
			}
		}
		p, err := admin.Do(ctx, live.APICall{Op: live.APIListPolicies, Name: name})
		if err != nil || !ok2xx(p.Status) {
			errs = append(errs, fmt.Errorf("the policy listing %s", answered(p, err)))
		} else {
			for _, q := range p.Policies {
				policies = append(policies, q.ID)
			}
		}
		return clients, policies, errs
	}
	del := func(kind string, call live.APICall) error {
		a, err := admin.Do(ctx, call)
		if err != nil || !ok2xx(a.Status) && a.Status != http.StatusNotFound {
			return fmt.Errorf("the delete of a %s %s", kind, answered(a, err))
		}
		return nil
	}
	clients, policies, errs := list()
	for _, id := range clients {
		if err := del("client", live.APICall{Op: live.APIDeleteClient, ID: id}); err != nil {
			errs = append(errs, err)
		}
	}
	for _, id := range policies {
		if err := del("policy", live.APICall{Op: live.APIDeletePolicy, ID: id}); err != nil {
			errs = append(errs, err)
		}
	}
	leftClients, leftPolicies, lerrs := list()
	errs = append(errs, lerrs...)
	if len(leftClients) > 0 {
		errs = append(errs, fmt.Errorf("%d client(s) still listed", len(leftClients)))
	}
	if len(leftPolicies) > 0 {
		errs = append(errs, fmt.Errorf("%d policy(ies) still listed", len(leftPolicies)))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("%s token-residual %s: %w", verb, name, err)
	}
	fmt.Fprintf(term, "LZ-LIVE %s token-residual removed %d clients and %d policies named %s\n", verb, len(clients), len(policies), name)
	return nil
}
