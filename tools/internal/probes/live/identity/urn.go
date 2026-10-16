package identity

// The iam-urn check (spec 003 T033, T034): which URN OVHcloud checks an `account:apiovh:iam/*`
// action against, the project's or the account's (FR-006 C006.2, C006.5; FR-008 C008.1; D109:
// `account:apiovh:iam/resource/edit` granted on a project URN did not authorise tagging that
// project, so the account URN is the likely target, UNVERIFIED until T027). lz-live runs it on
// tests/live/probes/iam-urn, whose sensitive output urn_check publishes run_id, project_urn,
// account_urn, bucket = lzprobe-iam-urn-<lower(run id)> (the only resource the root owns, the
// write's target) and bucket_urn (data-model.md *Probe checks*). The check, not the root, creates
// and owns the identities and policies lzprobe-iam-urn-<run id>-project|account.
//
//   - Observe lists the bucket as an IAM resource with the sandbox admin (GET /iam/resource by
//     name): the write's target is owned only when a listed resource named `bucket` has the URN
//     bucket_urn. It creates, per identity, an OAuth2 client and a policy that allows exactly the
//     probed read URNRead, the probed write URNWrite and the identity's control on exactly its URN
//     (project_urn or account_urn), and takes one token per client. Per identity it repeats the
//     control Interval apart until it is allowed or Settle is used up; only then it sends the read
//     (GET /iam/policy/{its own policy}) and, when the target is owned, the write (PUT
//     /iam/resource/{bucket_urn} with the listed tags plus URNTagKey = the run id). JudgeURN
//     decides each action from both identities' answers; a blocked result fails Observe.
//   - Undo and Sweep list the clients and policies of the two exact names and the bucket's IAM
//     resource, delete each and the run's tag key, and list again; anything left fails them. A
//     thing already gone (absent, or a delete answering 404) is undone, so a second Undo, or an
//     Undo after a Sweep, passes. Sweep, which runs after the destroy, also deletes the bucket
//     itself when a lost create answer left it outside the state (ProbeCheck.Sweep: every
//     resource the root or the check may have created); Undo leaves it to the destroy.
//
// Of the API the check keeps statuses, times and the actions a refusal names; an answer's actions
// are dropped when they hold the token or secret of any identity of the run. No record, terminal
// line or error carries a secret, a token or answer text.

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

// URNAnswer is one identity's answer to a control or a probed call.
type URNAnswer struct {
	Status  int
	Actions []string // the IAM actions a refusal named (live.APIChecker)
	Err     error
}

// The check's fixed names, actions and region.
const (
	URNOutput = "urn_check"
	// URNRead is the probed read: GET /iam/policy/{policyId} (kb api/v2/iam.json, iamActions),
	// sent on the identity's own policy.
	URNRead = "account:apiovh:iam/policy/get"
	// URNWrite is the floor's write: PUT /iam/resource/{resourceURN} (kb api/v2/iam.json).
	URNWrite = "account:apiovh:iam/resource/edit"
	// URNTagKey is the only tag the write adds, on the probe bucket only. A key may hold `:`
	// (kb api/v2/iam.json iam.resource.AddTag: '^[a-zA-Z0-9_.:/=+@-]{1,128}').
	URNTagKey = "lzprobe-iam-urn:run-id"
	// URNRegion is the probe bucket's region: the root creates it there and Sweep looks there.
	URNRegion = "GRA"

	// The controls: GET /cloud/project/{serviceName} (kb api/v1/cloud.json) for the project URN,
	// GET /me (observed by spec 005) for the account URN.
	urnCtlProject = "publicCloudProject:apiovh:get"
	urnCtlAccount = "account:apiovh:me/get"
)

// URNName is the client and policy name of a run's identity ("project" or "account").
func URNName(runID, who string) string { return "lzprobe-iam-urn-" + runID + "-" + who }

// URNBucket is the run's probe bucket (bucket names are lower case, P21).
func URNBucket(runID string) string { return "lzprobe-iam-urn-" + strings.ToLower(runID) }

func uOK(a URNAnswer) bool { return a.Err == nil && a.Status >= 200 && a.Status <= 299 }

// uOutage: no answer, or an answer other than 2xx, 401 or 403.
func uOutage(a URNAnswer) bool {
	return a.Err != nil || !(a.Status >= 200 && a.Status <= 299 || a.Status == http.StatusUnauthorized || a.Status == http.StatusForbidden)
}

// uDenied: a 403 that names the action (an attributable denial); a 403 naming nothing is not.
func uDenied(action string, a URNAnswer) bool {
	return a.Err == nil && a.Status == http.StatusForbidden && slices.Contains(a.Actions, action)
}

// JudgeURN, once per probed action: "project", "account", "either" (both allowed) or "blocked"; a
// control that never succeeded within Settle makes it "blocked" whatever the probed calls
// answered. A URN is claimed only from one allowed call and one denial naming the action.
func JudgeURN(action string, projectControl, project, accountControl, account URNAnswer) string {
	if !uOK(projectControl) || !uOK(accountControl) {
		return blocked
	}
	switch {
	case uOK(project) && uOK(account):
		return "either"
	case uOK(project) && uDenied(action, account):
		return "project"
	case uDenied(action, project) && uOK(account):
		return "account"
	}
	return blocked
}

// urnReason is a blocked result's reason, in the order urn_test.go's header fixes: a control
// outage (it wins), a control refusal, a probed call's outage, two denials, anything else.
func urnReason(action string, pc, p, ac, a URNAnswer) string {
	switch {
	case !uOK(pc) && uOutage(pc) || !uOK(ac) && uOutage(ac):
		return "outage"
	case !uOK(pc) || !uOK(ac):
		return "propagation"
	case uOutage(p) || uOutage(a):
		return "outage"
	case uDenied(action, p) && uDenied(action, a):
		return "refused"
	}
	return "unattributable"
}

var (
	urnProjectRE = regexp.MustCompile(`^urn:v1:eu:resource:publicCloudProject:([0-9a-f]{32})$`)
	urnAccountRE = regexp.MustCompile(`^urn:v1:eu:resource:account:[a-z0-9-]{1,64}$`)
	urnPublic    = []string{"account_urn", "bucket", "bucket_urn", "project_urn", "run_id"}
)

// validateURN refuses published variables that are not the iam-urn root's of one run: exactly the
// five keys; a run id; the project and account URNs of their types; the bucket of the run; and a
// bucket_urn that is a resource URN without a wildcard, other than the project's and the
// account's, whose last `:` or `/` segment is the bucket. The bucket URN's form is UNVERIFIED, so
// its type and project are not checked here: Observe writes only to a resource the admin's
// listing shows under the bucket's name with exactly that URN.
func validateURN(env map[string]string) error {
	if got := slices.Sorted(maps.Keys(env)); !slices.Equal(got, urnPublic) {
		return fmt.Errorf("output %s publishes the keys %s, want %s", URNOutput, strings.Join(got, ", "), strings.Join(urnPublic, ", "))
	}
	for _, k := range urnPublic {
		if env[k] == "" {
			return fmt.Errorf("output %s publishes an empty %s", URNOutput, k)
		}
	}
	b, bu := env["bucket"], env["bucket_urn"]
	switch {
	case !live.RunIDPattern.MatchString(env["run_id"]):
		return fmt.Errorf("output %s: run_id is not a run id", URNOutput)
	case !urnProjectRE.MatchString(env["project_urn"]):
		return fmt.Errorf("output %s: project_urn is not a public cloud project URN", URNOutput)
	case !urnAccountRE.MatchString(env["account_urn"]):
		return fmt.Errorf("output %s: account_urn is not an account URN", URNOutput)
	case b != URNBucket(env["run_id"]):
		return fmt.Errorf("output %s: bucket is not lzprobe-iam-urn-<lower(run id)>", URNOutput)
	case !live.ValidResourceURN(bu) || strings.Contains(bu, "*") || bu == env["project_urn"] || bu == env["account_urn"] ||
		!strings.HasSuffix(bu, "/"+b) && !strings.HasSuffix(bu, ":"+b):
		return fmt.Errorf("output %s: bucket_urn is not a resource URN naming the bucket", URNOutput)
	}
	return nil
}

// IamURN is the probe check lz-live runs on the iam-urn root.
func IamURN(o Options) *live.ProbeCheck {
	return &live.ProbeCheck{
		Output:   URNOutput,
		Public:   slices.Clone(urnPublic),
		Validate: validateURN,
		Observe: func(ctx context.Context, env map[string]string, term io.Writer) ([]byte, error) {
			if err := validateURN(env); err != nil {
				return nil, err
			}
			rec, err := o.observeURN(ctx, env, term)
			raw, jerr := json.Marshal(rec)
			return raw, errors.Join(err, jerr)
		},
		Undo: func(ctx context.Context, env map[string]string, term io.Writer) error {
			if err := validateURN(env); err != nil {
				return err
			}
			return o.cleanupURN(ctx, env["run_id"], "", term, "undo")
		},
		Sweep: o.sweepURN,
	}
}

// URNAttempt is one control or probed call as observations.json keeps it.
type URNAttempt struct {
	Identity   string    `json:"identity"` // "project" or "account"
	Call       string    `json:"call"`     // "control" or "probe"
	Action     string    `json:"action"`
	At         time.Time `json:"at"`          // sent
	AnsweredAt time.Time `json:"answered_at"` // answered, or given up
	Status     int       `json:"status"`      // 0: no answer
	Actions    []string  `json:"actions,omitempty"`
	Outage     bool      `json:"outage"`
	// PolicyAge: seconds from the identity's policy's createdAt (OVHcloud's clock) to the send
	// (this host's clock): the send is the edge that cannot overstate it while the two clocks
	// agree; a host clock ahead of OVHcloud's overstates it by the skew (review r1).
	PolicyAge int64 `json:"policy_age_seconds"`
}

// URNResult is one probed action's result.
type URNResult struct {
	Action   string `json:"action"`
	Observed string `json:"observed"`         // "project", "account", "either", "blocked"
	Reason   string `json:"reason,omitempty"` // blocked: "propagation", "no owned target", "outage", "refused", "unattributable"
}

// URNRecord is the iam-urn check's observations.json.
type URNRecord struct {
	Check string `json:"check"`
	RunID string `json:"run_id"`
	// BucketListing: how the admin's listing of the bucket as an IAM resource went ("answered
	// 200", "answered 403", "gave no usable answer"); BucketURNs: the URNs it showed under the
	// bucket's name (T027 learns the form from it; UNVERIFIED until then).
	BucketListing string       `json:"bucket_listing"`
	BucketURNs    []string     `json:"bucket_urns_listed"`
	Setup         string       `json:"setup,omitempty"` // what could not be set up; then no results
	Attempts      []URNAttempt `json:"attempts"`
	Results       []URNResult  `json:"results"`
}

// urnIdent is one probe identity during Observe.
type urnIdent struct {
	who, urn, ctl string
	ctlCall       live.APICall
	policyID      string
	created       time.Time
	probe         *live.APIChecker
	ctlAns        URNAnswer
	read, write   URNAnswer
}

// observeURN runs the probe; the record is complete whatever fails.
func (o Options) observeURN(ctx context.Context, env map[string]string, term io.Writer) (URNRecord, error) {
	run, bucket, bu := env["run_id"], env["bucket"], env["bucket_urn"]
	rec := URNRecord{Check: "iam-urn", RunID: run, BucketURNs: []string{}, Attempts: []URNAttempt{}, Results: []URNResult{}}
	setup := func(err error) (URNRecord, error) {
		rec.Setup = err.Error()
		fmt.Fprintf(term, "LZ-LIVE observe iam-urn blocked (setup)\n")
		return rec, fmt.Errorf("iam-urn: blocked (setup): %w", err)
	}
	admin, err := live.NewAPIChecker(ctx, o.API, o.Admin)
	if err != nil {
		return setup(fmt.Errorf("admin identity: %w", err))
	}
	checkers := []*live.APIChecker{admin}

	// The write's target: a listed resource named bucket whose URN is bucket_urn.
	var tags map[string]string
	l, err := admin.Do(ctx, live.APICall{Op: live.APIListResources, Name: bucket})
	rec.BucketListing = answered(l, err)
	if err == nil && ok2xx(l.Status) {
		for _, r := range l.Resources {
			// A listed URN is server text: kept only when it holds no secret of the run (review
			// r1); at this point the admin is the run's only identity.
			if !admin.Carries(r.URN) {
				rec.BucketURNs = append(rec.BucketURNs, r.URN)
			}
			// A resource whose tags could not all be read is no target: writing back what was
			// read would remove the others (review r1).
			if r.URN == bu && r.Name == bucket && !r.Partial {
				tags = maps.Clone(r.Tags)
			}
		}
	}
	owned := tags != nil
	fmt.Fprintf(term, "LZ-LIVE observe iam-urn bucket listing %s, owned target %v\n", rec.BucketListing, owned)

	project := urnProjectRE.FindStringSubmatch(env["project_urn"])[1]
	ids := []*urnIdent{
		{who: "project", urn: env["project_urn"], ctl: urnCtlProject, ctlCall: live.APICall{Op: live.APIGetProject, Project: project}},
		{who: "account", urn: env["account_urn"], ctl: urnCtlAccount, ctlCall: live.APICall{Op: live.APIGetMe}},
	}
	for _, id := range ids {
		name := URNName(run, id.who)
		c, err := admin.Do(ctx, live.APICall{Op: live.APICreateClient, Name: name})
		if err != nil || !ok2xx(c.Status) || len(c.Clients) != 1 {
			return setup(fmt.Errorf("the create of client %s %s", name, answered(c, err)))
		}
		clientID, secret := c.Clients[0].ID, c.Secret
		g, err := admin.Do(ctx, live.APICall{Op: live.APIGetClient, ID: clientID})
		if err != nil || !ok2xx(g.Status) || len(g.Clients) != 1 || g.Clients[0].Identity == "" {
			return setup(fmt.Errorf("the identity of client %s is unknown: its read %s", name, answered(g, err)))
		}
		body, _ := json.Marshal(map[string]any{
			"name": name, "description": name, "identities": []string{g.Clients[0].Identity},
			"resources":   []map[string]string{{"urn": id.urn}},
			"permissions": map[string]any{"allow": []map[string]string{{"action": URNRead}, {"action": URNWrite}, {"action": id.ctl}}},
		})
		p, err := admin.Do(ctx, live.APICall{Op: live.APICreatePolicy, Body: body})
		answeredAt := o.now()
		if err != nil || !ok2xx(p.Status) || p.ID == "" {
			return setup(fmt.Errorf("the create of policy %s %s", name, answered(p, err)))
		}
		// The policy's age runs from its createdAt; without one, from the create's answer, which
		// cannot overstate it.
		id.policyID, id.created = p.ID, answeredAt
		if at, ok := p.CreatedAt[p.ID]; ok {
			id.created = at
		}
		fmt.Fprintf(term, "LZ-LIVE observe iam-urn created client and policy %s\n", name)
		probe, err := live.NewAPIChecker(ctx, o.API, live.Credential{Endpoint: o.Admin.Endpoint, ClientID: clientID, ClientSecret: secret})
		if err != nil {
			return setup(fmt.Errorf("the token of client %s: %w", name, err))
		}
		id.probe = probe
		checkers = append(checkers, probe)
	}

	// call sends one control or probed call as the identity and records it; the actions an answer
	// names are dropped when they hold any identity's token or secret.
	call := func(id *urnIdent, kind, action string, c live.APICall) (URNAnswer, time.Time) {
		at := o.now()
		a, err := id.probe.Do(ctx, c)
		answeredAt := o.now()
		ans := URNAnswer{Status: a.Status, Err: err}
		if err == nil {
			for _, act := range a.Actions {
				if !slices.ContainsFunc(checkers, func(k *live.APIChecker) bool { return k.Carries(act) }) {
					ans.Actions = append(ans.Actions, act)
				}
			}
		}
		rec.Attempts = append(rec.Attempts, URNAttempt{Identity: id.who, Call: kind, Action: action, At: at, AnsweredAt: answeredAt,
			Status: ans.Status, Actions: ans.Actions, Outage: uOutage(ans), PolicyAge: int64(at.Sub(id.created) / time.Second)})
		fmt.Fprintf(term, "LZ-LIVE observe iam-urn %s %s %s %d\n", id.who, kind, action, ans.Status)
		return ans, at
	}
	interval, settle := o.interval(), o.settle()
	for _, id := range ids {
		var first time.Time
		for {
			var at time.Time
			id.ctlAns, at = call(id, "control", id.ctl, id.ctlCall)
			if first.IsZero() {
				first = at
			}
			if uOK(id.ctlAns) {
				break
			}
			// Retry while the next control, sent one interval after this one, stays within Settle;
			// a control slower than the interval never sends past it either.
			next := at.Add(interval)
			if next.Sub(first) > settle {
				break
			}
			if err := o.sleep(ctx, max(0, next.Sub(o.now()))); err != nil || o.now().Sub(first) > settle {
				break
			}
		}
		if !uOK(id.ctlAns) {
			continue
		}
		id.read, _ = call(id, "probe", URNRead, live.APICall{Op: live.APIGetPolicy, ID: id.policyID})
		if owned {
			t := maps.Clone(tags)
			t[URNTagKey] = run
			id.write, _ = call(id, "probe", URNWrite, live.APICall{Op: live.APIPutResourceTags, URN: bu, Tags: t})
		}
	}

	p, a := ids[0], ids[1]
	var blockedErrs []error
	for _, action := range []string{URNRead, URNWrite} {
		pa, aa := p.read, a.read
		if action == URNWrite {
			pa, aa = p.write, a.write
		}
		r := URNResult{Action: action, Observed: JudgeURN(action, p.ctlAns, pa, a.ctlAns, aa)}
		if r.Observed == blocked {
			r.Reason = urnReason(action, p.ctlAns, pa, a.ctlAns, aa)
			if action == URNWrite && !owned && uOK(p.ctlAns) && uOK(a.ctlAns) {
				r.Reason = "no owned target"
			}
			blockedErrs = append(blockedErrs, fmt.Errorf("%s blocked (%s)", action, r.Reason))
		}
		rec.Results = append(rec.Results, r)
		fmt.Fprintf(term, "LZ-LIVE observe iam-urn %s: %s %s\n", action, r.Observed, r.Reason)
	}
	if err := errors.Join(blockedErrs...); err != nil {
		return rec, fmt.Errorf("iam-urn: %w", err)
	}
	return rec, nil
}

// sweepURN is the check's Sweep (data-model.md *Probe checks*): it deletes, by listing the exact
// names lzprobe-iam-urn-<run id>-project|account with the sandbox admin, the clients and policies
// the check may have created, removes the tag key lzprobe-iam-urn:run-id from the probe bucket
// lzprobe-iam-urn-<lower(run id)>, and deletes that bucket in URNRegion when it is still there,
// whether or not an output or a record exists.
func (o Options) sweepURN(ctx context.Context, runID, projectID string, term io.Writer) error {
	if !live.RunIDPattern.MatchString(runID) {
		return errors.New("sweep iam-urn: the run id is not a run id")
	}
	if !projectShape.MatchString(projectID) {
		return errors.New("sweep iam-urn: the project is not a project id")
	}
	return o.cleanupURN(ctx, runID, projectID, term, "sweep")
}

// urnFound is what a cleanup's listing found of a run.
type urnFound struct {
	clients, policies map[string][]string // name → ids
	tagged, buckets   []string
}

// cleanupURN lists the clients and policies of the run's two exact names, the probe bucket's IAM
// resource (for the run's tag key) and, when project is set (Sweep), the probe bucket itself;
// deletes each; and lists again. A failed listing or delete does not stop the others; the second
// listing decides what is left. A delete answering 404 is gone already. Only a resource named the
// bucket whose URN ends in it loses the tag key.
func (o Options) cleanupURN(ctx context.Context, runID, project string, term io.Writer, verb string) error {
	bucket := URNBucket(runID)
	names := []string{URNName(runID, "project"), URNName(runID, "account")}
	admin, err := live.NewAPIChecker(ctx, o.API, o.Admin)
	if err != nil {
		return fmt.Errorf("%s iam-urn %s: admin identity: %w", verb, runID, err)
	}
	list := func() (f urnFound, errs []error) {
		f.clients, f.policies = map[string][]string{}, map[string][]string{}
		for _, name := range names {
			a, err := admin.Do(ctx, live.APICall{Op: live.APIListClients, Name: name})
			if err != nil || !ok2xx(a.Status) {
				errs = append(errs, fmt.Errorf("the client listing %s", answered(a, err)))
			} else {
				for _, c := range a.Clients {
					f.clients[name] = append(f.clients[name], c.ID)
				}
			}
			p, err := admin.Do(ctx, live.APICall{Op: live.APIListPolicies, Name: name})
			if err != nil || !ok2xx(p.Status) {
				errs = append(errs, fmt.Errorf("the policy listing %s", answered(p, err)))
			} else {
				for _, q := range p.Policies {
					f.policies[name] = append(f.policies[name], q.ID)
				}
			}
		}
		r, err := admin.Do(ctx, live.APICall{Op: live.APIListResources, Name: bucket})
		if err != nil || !ok2xx(r.Status) {
			errs = append(errs, fmt.Errorf("the resource listing %s", answered(r, err)))
		} else {
			for _, x := range r.Resources {
				_, has := x.Tags[URNTagKey]
				if has && x.Name == bucket && (strings.HasSuffix(x.URN, "/"+bucket) || strings.HasSuffix(x.URN, ":"+bucket)) {
					f.tagged = append(f.tagged, x.URN)
				}
			}
		}
		if project != "" {
			b, err := admin.Do(ctx, live.APICall{Op: live.APIListBuckets, Project: project, Region: URNRegion})
			switch {
			case err == nil && b.Status == http.StatusNotFound && b.NoBucketService:
			case err != nil || !ok2xx(b.Status):
				errs = append(errs, fmt.Errorf("the bucket listing %s", answered(b, err)))
			case slices.Contains(b.Names, bucket):
				f.buckets = []string{bucket}
			}
		}
		return f, errs
	}
	del := func(what string, call live.APICall) error {
		a, err := admin.Do(ctx, call)
		if err != nil || !ok2xx(a.Status) && a.Status != http.StatusNotFound {
			return fmt.Errorf("the delete of %s %s", what, answered(a, err))
		}
		return nil
	}
	f, errs := list()
	n := 0
	for _, name := range names {
		for _, id := range f.clients[name] {
			n++
			if err := del("client "+name, live.APICall{Op: live.APIDeleteClient, ID: id}); err != nil {
				errs = append(errs, err)
			}
		}
		for _, id := range f.policies[name] {
			n++
			if err := del("policy "+name, live.APICall{Op: live.APIDeletePolicy, ID: id}); err != nil {
				errs = append(errs, err)
			}
		}
	}
	for _, u := range f.tagged {
		n++
		if err := del("tag key "+URNTagKey+" of "+bucket, live.APICall{Op: live.APIDeleteResourceTag, URN: u, Key: URNTagKey}); err != nil {
			errs = append(errs, err)
		}
	}
	for _, b := range f.buckets {
		n++
		if err := del("bucket "+b, live.APICall{Op: live.APIDeleteBucket, Project: project, Region: URNRegion, Name: b}); err != nil {
			errs = append(errs, err)
		}
	}
	left, lerrs := list()
	errs = append(errs, lerrs...)
	for _, name := range names {
		if len(left.clients[name]) > 0 {
			errs = append(errs, fmt.Errorf("client %s still listed", name))
		}
		if len(left.policies[name]) > 0 {
			errs = append(errs, fmt.Errorf("policy %s still listed", name))
		}
	}
	if len(left.tagged) > 0 {
		errs = append(errs, fmt.Errorf("tag key %s still on %s", URNTagKey, bucket))
	}
	if len(left.buckets) > 0 {
		errs = append(errs, fmt.Errorf("bucket %s still listed", bucket))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("%s iam-urn %s: %w", verb, runID, err)
	}
	fmt.Fprintf(term, "LZ-LIVE %s iam-urn removed %d clients, policies, tag keys and buckets of run %s\n", verb, n, runID)
	return nil
}
