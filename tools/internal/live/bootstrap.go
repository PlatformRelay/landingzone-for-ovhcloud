package live

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Account bootstrap, phases guard, identify, passphrase, admin and revoke (spec 005 FR-010,
// FR-012, research R13, R19; data-model *Account binding and local files*). The state, publish and
// verify phases (T056/T057) run through BootstrapOptions.Rest.
//
// Credentials: without --fresh-account the run works only with sandbox.env's admin credential.
// With it the operator types the account root's AK/AS/CK at a no-echo prompt; they stay in memory
// (rootkeys.go), sign requests in process, are added to the run's Redactor and are revoked on every
// exit path once entered. Every line the run prints and every phase detail passes that Redactor
// (redact.go, guard G2); files are written only through files.go.
//
// Coordinator decisions of 2026-10-08 (evidence/T043.md): the admin policy covers the account and
// every Public Cloud project (the admin is account-wide by role; least privilege lives in the
// per-project deployer identities); a client this run created is deleted again when the admin
// cannot be completed (only if that fails too is its id reported); --fresh-account never repairs
// or replaces an admin that exists: one that has drifted, or one the run holds no working
// credential for, is refused (CondAdminExists).

// Bootstrap phases, in run order (research R13). `revoke` runs only with FreshAccount.
const (
	PhaseGuard      = "guard"
	PhaseIdentify   = "identify"
	PhasePassphrase = "passphrase"
	PhaseAdmin      = "admin"
	PhaseRevoke     = "revoke"
)

// Phase statuses (research R13).
const (
	StatusRan       = "ran"
	StatusUnchanged = "unchanged"
	StatusBlocked   = "blocked"
	StatusFail      = "fail"
)

// Refusal conditions of the bootstrap.
const (
	CondConfigRoot  = "config-root"  // the credential directory lies inside the checkout (directly or through a symlink)
	CondAdminExists = "admin-exists" // --fresh-account on an account whose admin exists but is drifted or has no working sandbox.env
)

// PhaseResult is one phase's outcome; Detail never holds a secret.
type PhaseResult struct {
	Phase  string
	Status string
	Detail string
}

// Terminal is the operator's terminal. Production reads /dev/tty; tests inject a fake.
type Terminal interface {
	// ReadSecret prints prompt and reads one line with echo off (root AK/AS/CK).
	ReadSecret(prompt string) (string, error)
	// ReadLine prints prompt and reads one line with echo on (project references; not secret).
	ReadLine(prompt string) (string, error)
}

// BootstrapAccount is what the phases after `admin` run with.
type BootstrapAccount struct {
	ID    string     // the account id GET /auth/details returned
	Dir   string     // <ConfigRoot>/accounts/<ID>
	Admin Credential // the sandbox.env credential (never the root keys)
}

// BootstrapOptions configures one `lz-live bootstrap` run.
type BootstrapOptions struct {
	ConfigRoot   string   // ~/.config/ovh-lz (live.env, sandbox.env, accounts/)
	Checkout     string   // the owner's checkout; no credential file may lie inside it
	Endpoint     string   // API endpoint name written to sandbox.env and account.env, e.g. ovh-eu
	Org          string   // the manifest's spec.org
	ProjectRefs  []string // the manifest's project references; account.env holds LZ_PROJECT_ID_<REF> for each
	FreshAccount bool     // --fresh-account: root keys at the prompt, admin created, root credential revoked
	API          API      // OAuth2 token endpoint and API base; root-key requests are signed in process
	Terminal     Terminal
	Stdout       io.Writer // LZ-LIVE lines
	// Guard is the host guard (Host.Check); a refusal ends the run before any credential is read.
	Guard func() error
	// Rest runs the phases after `admin` (state, publish, verify; T056/T057); nil skips them.
	Rest func(ctx context.Context, a BootstrapAccount) error
}

// The admin as expected (research R13, coordinator decision 1): one OAuth2 client with the
// client-credentials flow, bound by exactly one policy granting exactly these actions on the
// account and on every Public Cloud project, with no except, deny, permissions group, condition
// or expiry. The captured lz-sandbox-admin policy (T087) has this shape.
const (
	adminName        = "lz-sandbox-admin"
	adminDescription = "Landing-zone sandbox admin: IAM, me, Public Cloud"
	adminFlow        = "CLIENT_CREDENTIALS"
)

var adminActions = []string{"account:apiovh:iam/*", "account:apiovh:me/*", "publicCloudProject:apiovh:*"}

func adminResources(account string) []string {
	return []string{"urn:v1:eu:resource:account:" + account, "urn:v1:eu:resource:publicCloudProject:*"}
}

// statusAPIError is adminState's API failure that is neither drift nor a missing admin (a 5xx, a
// timeout): reported as fail with no repair advice, never as drift (review r2).
const statusAPIError = "error"

// revokeTimeout bounds the revocation, which runs also after an interrupt.
const revokeTimeout = 30 * time.Second

type bootstrap struct {
	o       BootstrapOptions
	out     io.WriteCloser // o.Stdout through red
	red     *Redactor
	rs      []PhaseResult
	root    *rootClient // nil without --fresh-account
	account string
	dir     string            // accounts/<account>
	binding map[string]string // account.env
	cred    Credential        // sandbox.env's credential for this account, or the admin this run created
	bearer  *bearerClient     // cred's token
	stale   string            // why sandbox.env, a client of this account, does not work (--fresh-account)
}

// Bootstrap runs the phases in order and returns each phase's result. A refusal is a *Refusal
// (exit 3); a blocked phase is a *Blocked (exit 2), a failed one any other error (exit 1). Once
// root keys were entered, `revoke` runs on every exit path and is the last phase reported.
func Bootstrap(ctx context.Context, o BootstrapOptions) ([]PhaseResult, error) {
	b := &bootstrap{o: o, red: NewRedactor()}
	b.out = b.red.Writer(o.Stdout)
	err := b.run(ctx)
	b.out.Close()
	if err != nil {
		// lz-live prints the error to stderr: it passes the run's Redactor too (also the state
		// phase's), keeping the Refusal or Blocked inside for the exit code.
		err = redactedError{err: err, red: b.red}
	}
	return b.rs, err
}

func (b *bootstrap) run(ctx context.Context) (err error) {
	if err := b.o.Guard(); err != nil {
		return err
	}
	b.add(PhaseGuard, StatusRan, "")
	if err := configOutside(b.o.ConfigRoot, b.o.Checkout); err != nil {
		return err
	}
	if b.o.FreshAccount {
		keys, kerr := b.readRootKeys()
		if kerr != nil {
			return b.fail(PhaseIdentify, "root keys not entered")
		}
		b.red.Add(keys.secrets()...)
		b.root = newRootClient(b.o.API, keys)
		// err is the named result: a revocation failure is never lost behind a nil.
		defer func() {
			if rerr := b.revoke(ctx); rerr != nil {
				// A live root credential left behind is a failure (exit 1), whatever the run's
				// own outcome was: the earlier error is kept as text, not as a *Refusal or
				// *Blocked that would win the exit code.
				if err != nil {
					rerr = fmt.Errorf("%w; the run had already stopped: %s", rerr, err.Error())
				}
				err = rerr
			}
		}()
	}
	if err := b.identify(ctx); err != nil {
		return err
	}
	if err := b.passphrase(); err != nil {
		return err
	}
	if err := b.admin(ctx); err != nil {
		return err
	}
	if b.o.Rest == nil {
		return nil
	}
	return b.o.Rest(ctx, BootstrapAccount{ID: b.account, Dir: b.dir, Admin: b.cred})
}

// configOutside refuses a credential directory inside the checkout, also through a symlink (G3).
func configOutside(root, checkout string) error {
	r, err := filepath.EvalSymlinks(root)
	if err != nil {
		return refuse(CondConfigRoot, "%s does not resolve", root)
	}
	c, err := filepath.EvalSymlinks(checkout)
	if err != nil {
		return refuse(CondConfigRoot, "%s does not resolve", checkout)
	}
	if rel, err := filepath.Rel(c, r); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return refuse(CondConfigRoot, "credential directory %s lies inside the checkout %s", root, checkout)
	}
	return nil
}

func (b *bootstrap) readRootKeys() (RootKeys, error) {
	var v [3]string
	for i, prompt := range []string{"application key (AK): ", "application secret (AS): ", "consumer key (CK): "} {
		s, err := b.o.Terminal.ReadSecret(prompt)
		if err != nil {
			return RootKeys{}, err
		}
		if v[i] = strings.TrimSpace(s); v[i] == "" {
			return RootKeys{}, errors.New("empty")
		}
	}
	return RootKeys{ak: v[0], as: v[1], ck: v[2]}, nil
}

func readSandbox(path string) (Credential, error) {
	v, err := ReadCredentialFile(path)
	if err != nil {
		return Credential{}, err
	}
	return Credential{Endpoint: v["OVH_ENDPOINT"], ClientID: v["OVH_CLIENT_ID"], ClientSecret: v["OVH_CLIENT_SECRET"]}, nil
}

// accountOf binds a credential to its account through GET /auth/details (P26), keeping its token.
func (b *bootstrap) accountOf(ctx context.Context, c Credential) (string, *bearerClient, error) {
	bc, err := newBearer(ctx, b.o.API, c)
	if err != nil {
		return "", nil, err
	}
	var d struct {
		Account string `json:"account"`
	}
	if err := bc.get(ctx, "/auth/details", &d); err != nil {
		return "", nil, err
	}
	if d.Account == "" {
		return "", nil, errors.New("GET /auth/details named no account")
	}
	return d.Account, &bc, nil
}

// identify learns the account (sandbox.env's credential, or the root keys), moves a previous
// account's sandbox.env aside under --fresh-account, and creates or checks account.env (G13).
func (b *bootstrap) identify(ctx context.Context) error {
	sandbox := filepath.Join(b.o.ConfigRoot, "sandbox.env")
	if b.root == nil {
		if _, err := os.Lstat(sandbox); errors.Is(err, fs.ErrNotExist) {
			return b.block(PhaseIdentify, "no sandbox.env: run with --fresh-account")
		}
		cred, err := readSandbox(sandbox)
		if err != nil {
			return err
		}
		b.red.Add(cred.ClientSecret)
		// Compared before the credential is sent anywhere.
		if cred.Endpoint != b.o.Endpoint {
			return refuse(CondEndpoint, "sandbox.env endpoint %q, want %q", cred.Endpoint, b.o.Endpoint)
		}
		account, bearer, err := b.accountOf(ctx, cred)
		switch {
		case errors.Is(err, errRejected):
			return b.block(PhaseIdentify, "the sandbox.env credential is rejected: run with --fresh-account")
		case err != nil:
			return b.fail(PhaseIdentify, err.Error())
		}
		b.account, b.cred, b.bearer = account, cred, bearer
	} else {
		var d struct {
			Account string `json:"account"`
		}
		if err := b.root.call(ctx, http.MethodGet, "/auth/details", nil, &d); err != nil {
			return b.fail(PhaseIdentify, "GET /auth/details with the root keys: "+err.Error())
		}
		if d.Account == "" {
			return b.fail(PhaseIdentify, "GET /auth/details with the root keys named no account")
		}
		b.account = d.Account
	}
	var err error
	if b.dir, err = AccountDir(b.o.ConfigRoot, b.account); err != nil {
		return b.fail(PhaseIdentify, err.Error())
	}
	if b.root != nil {
		if err := b.migrate(ctx, sandbox); err != nil {
			return err
		}
	}

	changed := false
	path := filepath.Join(b.dir, "account.env")
	if _, serr := os.Lstat(path); serr == nil {
		if b.binding, err = ReadCredentialFile(path); err != nil {
			return err
		}
		switch {
		case b.binding["LZ_ACCOUNT_ID"] != b.account:
			return refuse(CondAccount, "account.env account %q, credential %q", b.binding["LZ_ACCOUNT_ID"], b.account)
		case b.binding["OVH_ENDPOINT"] != b.o.Endpoint:
			return refuse(CondEndpoint, "account.env endpoint %q, want %q", b.binding["OVH_ENDPOINT"], b.o.Endpoint)
		case b.binding["LZ_ORG"] != b.o.Org:
			return refuse(CondOrg, "manifest org %q, bound %q", b.o.Org, b.binding["LZ_ORG"])
		}
	} else if errors.Is(serr, fs.ErrNotExist) {
		b.binding = map[string]string{"LZ_ACCOUNT_ID": b.account, "OVH_ENDPOINT": b.o.Endpoint, "LZ_ORG": b.o.Org}
		changed = true
	} else {
		return b.fail(PhaseIdentify, serr.Error())
	}

	var missing []string
	for _, ref := range b.o.ProjectRefs {
		if b.binding["LZ_PROJECT_ID_"+ref] == "" {
			missing = append(missing, "LZ_PROJECT_ID_"+ref)
		}
	}
	if len(missing) > 0 && b.root == nil {
		if changed {
			if err := b.writeBinding(); err != nil {
				return b.fail(PhaseIdentify, err.Error())
			}
		}
		return b.block(PhaseIdentify, "account.env lacks "+strings.Join(missing, ", ")+": fill them in or run with --fresh-account")
	}
	if len(missing) > 0 {
		var projects []string
		if err := b.root.call(ctx, http.MethodGet, "/cloud/project", nil, &projects); err != nil {
			return b.fail(PhaseIdentify, err.Error())
		}
		fmt.Fprintf(b.out, "LZ-LIVE identify bootstrap info the account's projects: %s\n", strings.Join(projects, " "))
		for _, m := range missing {
			v, err := b.o.Terminal.ReadLine(m + " (one of the listed projects): ")
			if err != nil {
				return b.fail(PhaseIdentify, "the "+m+" prompt was aborted")
			}
			if v = strings.TrimSpace(v); !slices.Contains(projects, v) {
				return b.fail(PhaseIdentify, m+": not one of the listed projects")
			}
			b.binding[m] = v
		}
		changed = true
	}
	if !changed {
		b.add(PhaseIdentify, StatusUnchanged, b.account)
		return nil
	}
	if err := b.writeBinding(); err != nil {
		return b.fail(PhaseIdentify, err.Error())
	}
	b.add(PhaseIdentify, StatusRan, b.account)
	return nil
}

// migrate handles sandbox.env under --fresh-account: one for this account is the admin to check;
// one for another account moves to accounts/<old>/sandbox.env before anything is written, and no
// file of the old account is read afterwards (R13).
func (b *bootstrap) migrate(ctx context.Context, sandbox string) error {
	if _, err := os.Lstat(sandbox); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	old, err := readSandbox(sandbox)
	if err != nil {
		return err
	}
	b.red.Add(old.ClientSecret)
	// Compared before the credential is sent anywhere.
	if old.Endpoint != b.o.Endpoint {
		return refuse(CondEndpoint, "sandbox.env endpoint %q, want %q", old.Endpoint, b.o.Endpoint)
	}
	acct, bearer, err := b.accountOf(ctx, old)
	if errors.Is(err, errRejected) {
		// A rejected credential cannot say whose it is; the root keys can: a client of this
		// account is this account's admin with a stale secret, left in place for the admin
		// phase, which refuses it (decision 3).
		var ids []string
		if lerr := b.root.call(ctx, http.MethodGet, "/me/api/oauth2/client", nil, &ids); lerr != nil {
			return b.fail(PhaseIdentify, lerr.Error())
		}
		if slices.Contains(ids, old.ClientID) {
			b.stale = "the sandbox.env credential of client " + old.ClientID + " is rejected"
			return nil
		}
	}
	if err != nil {
		return b.fail(PhaseIdentify, "cannot tell which account sandbox.env belongs to ("+err.Error()+"): move it to accounts/<its account>/sandbox.env, then re-run")
	}
	if acct == b.account {
		b.cred, b.bearer = old, bearer
		return nil
	}
	if _, err := AccountDir(b.o.ConfigRoot, acct); err != nil {
		return b.fail(PhaseIdentify, err.Error())
	}
	if err := MoveCredentialFile(b.o.ConfigRoot, "sandbox.env", filepath.Join("accounts", acct, "sandbox.env")); err != nil {
		return b.fail(PhaseIdentify, err.Error())
	}
	fmt.Fprintf(b.out, "LZ-LIVE identify bootstrap info moved the previous sandbox.env to accounts/%s/\n", acct)
	return nil
}

func (b *bootstrap) writeBinding() error {
	return WriteCredentialFile(b.o.ConfigRoot, filepath.Join("accounts", b.account, "account.env"), b.binding)
}

// passphrase creates state-passphrase.env from 32 random bytes if absent; never overwritten (G11).
func (b *bootstrap) passphrase() error {
	path := filepath.Join(b.dir, "state-passphrase.env")
	if _, err := os.Lstat(path); err == nil {
		v, err := ReadCredentialFile(path)
		if err != nil {
			return err
		}
		if v["TF_VAR_state_passphrase"] == "" {
			return b.fail(PhasePassphrase, "state-passphrase.env holds no TF_VAR_state_passphrase; it is never overwritten: fix it by hand")
		}
		b.red.Add(v["TF_VAR_state_passphrase"])
		b.add(PhasePassphrase, StatusUnchanged, "")
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return b.fail(PhasePassphrase, err.Error())
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return b.fail(PhasePassphrase, "no randomness")
	}
	v := hex.EncodeToString(raw)
	b.red.Add(v)
	if err := WriteCredentialFile(b.o.ConfigRoot, filepath.Join("accounts", b.account, "state-passphrase.env"), map[string]string{"TF_VAR_state_passphrase": v}); err != nil {
		return b.fail(PhasePassphrase, err.Error())
	}
	b.add(PhasePassphrase, StatusRan, "")
	return nil
}

type policyAction struct {
	Action string `json:"action"`
}

type policyURN struct {
	URN string `json:"urn"`
}

// policy is the part of an IAM policy (kb/api/v2/iam.json iam.policy.Response) the admin check reads.
type policy struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Identities  []string    `json:"identities"`
	Resources   []policyURN `json:"resources"`
	Permissions struct {
		Allow  []policyAction `json:"allow"`
		Except []policyAction `json:"except"`
		Deny   []policyAction `json:"deny"`
	} `json:"permissions"`
	PermissionsGroups []policyURN `json:"permissionsGroups"`
	Conditions        any         `json:"conditions"`
	ExpiredAt         string      `json:"expiredAt"`
}

// adminState checks the admin with its own credential (R13 admin: "credential works" and
// "present as expected"). It returns the policy id when there is one bound to the client, and
// unchanged, blocked (client or policy missing or unreadable), fail (drift, naming it) or
// statusAPIError (the API failed).
func (b *bootstrap) adminState(ctx context.Context) (policyID, status, detail string) {
	var cl struct {
		ClientID string `json:"clientId"`
		Identity string `json:"identity"`
		Flow     string `json:"flow"`
	}
	if err := b.bearer.get(ctx, "/me/api/oauth2/client/"+url.PathEscape(b.cred.ClientID), &cl); err != nil {
		if c := statusOf(err); c == http.StatusNotFound || c == http.StatusForbidden {
			return "", StatusBlocked, "admin client missing or without its policy (" + err.Error() + ")"
		}
		return "", statusAPIError, err.Error()
	}
	ps, err := getAll[policy](ctx, *b.bearer, "/iam/policy?identity="+url.QueryEscape(cl.Identity))
	if err != nil {
		if statusOf(err) == http.StatusForbidden {
			return "", StatusBlocked, "admin policy missing or narrowed (" + err.Error() + ")"
		}
		return "", statusAPIError, err.Error()
	}
	ps = slices.DeleteFunc(ps, func(p policy) bool { return !slices.Contains(p.Identities, cl.Identity) })
	if len(ps) == 0 {
		return "", StatusBlocked, "admin policy missing"
	}
	// The recorded policy first, so the drift names the others as extra.
	slices.SortStableFunc(ps, func(x, y policy) int {
		switch {
		case x.ID == b.binding["LZ_ADMIN_POLICY_ID"]:
			return -1
		case y.ID == b.binding["LZ_ADMIN_POLICY_ID"]:
			return 1
		}
		return 0
	})
	p := ps[0]
	var diffs []string
	if cl.Flow != adminFlow {
		diffs = append(diffs, "client flow "+cl.Flow)
	}
	for _, x := range ps[1:] {
		var acts []string
		for _, a := range x.Permissions.Allow {
			acts = append(acts, a.Action)
		}
		diffs = append(diffs, fmt.Sprintf("extra policy %s (%s) allowing %s", x.Name, x.ID, strings.Join(acts, ", ")))
	}
	var acts, res []string
	for _, a := range p.Permissions.Allow {
		acts = append(acts, a.Action)
	}
	for _, r := range p.Resources {
		res = append(res, r.URN)
	}
	diffs = append(diffs, setDiff("action", acts, adminActions)...)
	diffs = append(diffs, setDiff("resource", res, adminResources(b.account))...)
	for _, a := range p.Permissions.Except {
		diffs = append(diffs, "except "+a.Action)
	}
	for _, a := range p.Permissions.Deny {
		diffs = append(diffs, "deny "+a.Action)
	}
	for _, g := range p.PermissionsGroups {
		diffs = append(diffs, "permissions group "+g.URN)
	}
	if p.Conditions != nil {
		diffs = append(diffs, "the policy has conditions")
	}
	if p.ExpiredAt != "" {
		diffs = append(diffs, "the policy expires "+p.ExpiredAt)
	}
	if len(p.Identities) != 1 {
		diffs = append(diffs, fmt.Sprintf("the policy binds %d identities", len(p.Identities)))
	}
	if len(diffs) > 0 {
		return p.ID, StatusFail, "admin drifted: " + strings.Join(diffs, "; ")
	}
	return p.ID, StatusUnchanged, ""
}

// setDiff names what got has beyond want (+) and what it lacks (-).
func setDiff(kind string, got, want []string) []string {
	var out []string
	for _, g := range got {
		if !slices.Contains(want, g) {
			out = append(out, "+"+kind+" "+g)
		}
	}
	for _, w := range want {
		if !slices.Contains(got, w) {
			out = append(out, "-"+kind+" "+w)
		}
	}
	slices.Sort(out)
	return out
}

// recordAdmin records the admin ids in account.env for the leftover exemption (R12).
func (b *bootstrap) recordAdmin(clientID, policyID string) error {
	if b.binding["LZ_ADMIN_CLIENT_ID"] == clientID && b.binding["LZ_ADMIN_POLICY_ID"] == policyID {
		return nil
	}
	b.binding["LZ_ADMIN_CLIENT_ID"] = clientID
	b.binding["LZ_ADMIN_POLICY_ID"] = policyID
	return b.writeBinding()
}

func (b *bootstrap) admin(ctx context.Context) error {
	if b.stale != "" {
		// This account's client, named in sandbox.env, exists whatever its name (decision 3).
		return b.refuseAdmin(b.stale)
	}
	detail := ""
	if b.cred.ClientID != "" {
		policyID, status, d := b.adminState(ctx)
		switch {
		case status == statusAPIError:
			return b.fail(PhaseAdmin, "the admin check failed: "+d)
		case status == StatusUnchanged:
			if err := b.recordAdmin(b.cred.ClientID, policyID); err != nil {
				return b.fail(PhaseAdmin, err.Error())
			}
			b.add(PhaseAdmin, StatusUnchanged, "")
			return nil
		case b.root == nil && status == StatusBlocked:
			return b.block(PhaseAdmin, d+": run with --fresh-account")
		case b.root == nil:
			return b.fail(PhaseAdmin, d+": fix the admin by hand, or delete it and re-run with --fresh-account")
		case status == StatusFail:
			// Decision 3: --fresh-account does not repair or replace an existing admin.
			return b.refuseAdmin(d)
		}
		detail = d
	}
	// --fresh-account without a working admin for this account: any client named like the admin
	// is one this run would duplicate (decision 3; also the orphan of a failed compensation).
	existing, err := b.adminClients(ctx)
	if err != nil {
		return b.fail(PhaseAdmin, err.Error())
	}
	if len(existing) > 0 {
		why := "admin client " + strings.Join(existing, ", ") + " exists without a working sandbox.env"
		if detail != "" {
			why += " (" + detail + ")"
		}
		return b.refuseAdmin(why)
	}
	return b.createAdmin(ctx)
}

// adminClients lists the account's OAuth2 clients named like the admin, with the root keys.
func (b *bootstrap) adminClients(ctx context.Context) ([]string, error) {
	var ids []string
	if err := b.root.call(ctx, http.MethodGet, "/me/api/oauth2/client", nil, &ids); err != nil {
		return nil, err
	}
	var out []string
	for _, id := range ids {
		var cl struct {
			Name string `json:"name"`
		}
		if err := b.root.call(ctx, http.MethodGet, "/me/api/oauth2/client/"+url.PathEscape(id), nil, &cl); err != nil {
			return nil, err
		}
		if cl.Name == adminName {
			out = append(out, id)
		}
	}
	return out, nil
}

// createAdmin creates the admin client and its policy with the root keys (P23) and writes
// sandbox.env once. When the admin cannot be completed, the client it created is deleted again
// (decision 2); only if that fails too does the failure name the client's id for the owner.
func (b *bootstrap) createAdmin(ctx context.Context) error {
	var sec struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}
	req := map[string]any{"callbackUrls": []string{}, "description": adminDescription, "flow": adminFlow, "name": adminName}
	if err := b.root.call(ctx, http.MethodPost, "/me/api/oauth2/client", req, &sec); err != nil {
		return b.fail(PhaseAdmin, err.Error())
	}
	b.red.Add(sec.ClientSecret)
	undo := func(cause string) error {
		// Like the revocation, the compensation outlives an interrupt.
		uctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), revokeTimeout)
		defer cancel()
		if err := b.root.call(uctx, http.MethodDelete, "/me/api/oauth2/client/"+url.PathEscape(sec.ClientID), nil, nil); err != nil {
			return b.fail(PhaseAdmin, cause+"; the client "+sec.ClientID+" this run created was not deleted ("+err.Error()+
				"): delete it (DELETE /me/api/oauth2/client/"+sec.ClientID+") before the next --fresh-account run")
		}
		return b.fail(PhaseAdmin, cause+"; the client this run created was deleted again")
	}
	if sec.ClientID == "" || sec.ClientSecret == "" {
		if sec.ClientID == "" {
			return b.fail(PhaseAdmin, "POST /me/api/oauth2/client named no client")
		}
		return undo("POST /me/api/oauth2/client returned no secret")
	}
	var cl struct {
		Identity string `json:"identity"`
	}
	if err := b.root.call(ctx, http.MethodGet, "/me/api/oauth2/client/"+url.PathEscape(sec.ClientID), nil, &cl); err != nil || cl.Identity == "" {
		return undo(fmt.Sprintf("the created client's identity is unknown (%v)", err))
	}
	var res []policyURN
	for _, u := range adminResources(b.account) {
		res = append(res, policyURN{URN: u})
	}
	var allow []policyAction
	for _, a := range adminActions {
		allow = append(allow, policyAction{Action: a})
	}
	preq := map[string]any{"name": adminName, "description": adminDescription, "identities": []string{cl.Identity},
		"resources": res, "permissions": map[string]any{"allow": allow}}
	var p struct {
		ID string `json:"id"`
	}
	if err := b.root.call(ctx, http.MethodPost, "/iam/policy", preq, &p); err != nil {
		return undo(err.Error())
	}
	cred := Credential{Endpoint: b.o.Endpoint, ClientID: sec.ClientID, ClientSecret: sec.ClientSecret}
	if err := WriteCredentialFile(b.o.ConfigRoot, "sandbox.env", map[string]string{"OVH_ENDPOINT": cred.Endpoint, "OVH_CLIENT_ID": cred.ClientID, "OVH_CLIENT_SECRET": cred.ClientSecret}); err != nil {
		return undo("sandbox.env not written (" + err.Error() + "); policy " + p.ID + " stays bound to the deleted client's identity")
	}
	b.cred = cred
	if err := b.recordAdmin(sec.ClientID, p.ID); err != nil {
		return b.fail(PhaseAdmin, err.Error())
	}
	b.add(PhaseAdmin, StatusRan, sec.ClientID)
	return nil
}

// revoke deletes the root keys' own credential (R13): GET /auth/currentCredential, then
// DELETE /me/api/credential/{id}. It runs on every exit path once the keys were entered, also
// after an interrupt (its own deadline, not the cancelled run's).
func (b *bootstrap) revoke(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), revokeTimeout)
	defer cancel()
	var cur struct {
		CredentialID int64 `json:"credentialId"`
	}
	err := b.root.call(ctx, http.MethodGet, "/auth/currentCredential", nil, &cur)
	if err == nil && cur.CredentialID == 0 {
		err = errors.New("GET /auth/currentCredential named no credential")
	}
	if err == nil {
		err = b.root.call(ctx, http.MethodDelete, fmt.Sprintf("/me/api/credential/%d", cur.CredentialID), nil, nil)
	}
	if err != nil {
		return b.fail(PhaseRevoke, "the root credential was not revoked ("+err.Error()+"): delete it in the Control Panel (API keys) now")
	}
	b.add(PhaseRevoke, StatusRan, "")
	return nil
}

// add records a phase result and prints its LZ-LIVE line (contracts/checks.md), both redacted.
func (b *bootstrap) add(phase, status, detail string) {
	detail = b.red.Redact(detail)
	b.rs = append(b.rs, PhaseResult{Phase: phase, Status: status, Detail: detail})
	fmt.Fprintln(b.out, strings.TrimSpace("LZ-LIVE "+phase+" bootstrap "+status+" "+detail))
}

func (b *bootstrap) block(phase, detail string) error {
	b.add(phase, StatusBlocked, detail)
	return &Blocked{Phase: phase, Detail: b.red.Redact(detail)}
}

func (b *bootstrap) fail(phase, detail string) error {
	b.add(phase, StatusFail, detail)
	return fmt.Errorf("%s: %s", phase, b.red.Redact(detail))
}

// refuseAdmin is decision 3's refusal: nothing created, changed or repaired; the owner deletes
// or fixes the admin named, then re-runs.
func (b *bootstrap) refuseAdmin(detail string) error {
	detail = b.red.Redact(detail + "; --fresh-account does not repair or replace an existing admin: delete or fix it, then re-run")
	b.add(PhaseAdmin, StatusFail, "refused ("+CondAdminExists+"): "+detail)
	return refuse(CondAdminExists, "%s", detail)
}
