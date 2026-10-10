package state

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// T096 against a fake S3 endpoint: a versioned bucket whose two users answer as the probe root's
// policies say — the state user (the state-backend Allow and Deny statements) and the janitor
// (`s3:*` and s3:BypassGovernanceRetention on the bucket). Knobs turn the fake into the worlds the
// live run may meet: the Deny's version pattern inert, bucket policies not implemented, a backend
// action refused, no version ids, failures during the undo. No test calls the OVHcloud API or
// reads ~/.config/ovh-lz/.
//
// T099: the bucket carries Object Lock with a default governance retention, as the probe root
// creates it. A versioned DELETE of a locked version is refused 403 AccessDenied unless it carries
// `x-amz-bypass-governance-retention: true` and its sender holds s3:BypassGovernanceRetention
// (docs.ovhcloud.com storage-and-backup/object-storage/s3-managing-object-lock.mdx:57-59, :241-255,
// :309-326); delete markers carry no retention. Knobs: the lock not in force, the state user able
// to bypass, the janitor unable to.

const (
	stateAK   = "AKSTATEFAKE00001"
	stateSK   = "state-secret-key-0a1b2c3d4e5f6a7b"
	janitorAK = "AKJANITORFAKE001"
	janitorSK = "janitor-secret-key-9f8e7d6c5b4a"
	bucket    = "lzprobe-state-20261010t120000z-ab12"
	echo      = "SERVERTEXT"
)

var secrets = []string{stateSK, janitorSK, stateAK, janitorAK}

func testEnv() map[string]string {
	return map[string]string{"bucket": bucket, "region": "GRA", "state_key_id": stateAK, "state_secret": stateSK,
		"janitor_key_id": janitorAK, "janitor_secret": janitorSK}
}

type version struct {
	key, id string
	marker  bool
}

// seen is one request: who sent it, which operation, which version id, whether it asked for the
// governance bypass.
type seen struct {
	who, op, versionID string
	bypass             bool
}

type fakeBucket struct {
	t          *testing.T
	srv        *httptest.Server
	mu         sync.Mutex
	versions   []version // oldest first
	policy     []byte
	versioning string
	seq        int
	log        []seen

	denyInert             bool           // the state user's versioned DELETE is allowed
	stateMayPutPolicy     bool           // the state user's PutBucketPolicy is allowed
	stateMaySetVersioning bool           // the state user's PutBucketVersioning is allowed
	policyNotImplemented  bool           // every bucket-policy call answers 501 NotImplemented
	noVersionIDs          bool           // PUT answers carry no x-amz-version-id
	sameVersionID         bool           // every PUT answers the same version id
	refuse                map[string]int // "<who> <op>": that status with code AccessDenied
	fail                  map[string]int // "<who> <op>": that status with code InternalError
	hangup                map[string]bool
	truncated, garbage    bool // the janitor's listing is truncated / not XML
	wrongRoot             bool // the listing answer is well-formed XML of another kind (review r1)
	versioningWrongRoot   bool // the versioning answer is well-formed XML of another kind
	keepVersions          bool // DELETE of a version answers 204 and removes nothing
	lockInert             bool // T099: Object Lock not in force (no lock, or retention ignored)
	stateMayBypass        bool // T099: the state user's bypass is honoured (its Deny inert)
	janitorMayNotBypass   bool // T099: the janitor's bypass is refused (its grant inert)
}

func newFakeBucket(t *testing.T) *fakeBucket {
	t.Helper()
	f := &fakeBucket{t: t, versioning: "Enabled", refuse: map[string]int{}, fail: map[string]int{}, hangup: map[string]bool{}}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeBucket) options() Options {
	pool := x509.NewCertPool()
	pool.AddCert(f.srv.Certificate())
	addr := f.srv.Listener.Addr().String()
	return Options{
		HTTP: &http.Client{Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "example.com"},
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		}},
		Now: func() time.Time { return time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC) },
	}
}

func (f *fakeBucket) requests() []seen {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.log)
}

func (f *fakeBucket) state() (vs []version, policy []byte, versioning string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.versions), f.policy, f.versioning
}

func errorAnswer(rw http.ResponseWriter, status int, code string) {
	rw.WriteHeader(status)
	// Error answers echo what a server might: text and the request's credential.
	fmt.Fprintf(rw, "<?xml version=\"1.0\"?><Error><Code>%s</Code><Message>%s %s %s</Message></Error>", code, echo, stateSK, janitorSK)
}

func (f *fakeBucket) serve(rw http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	authz := r.Header.Get("Authorization")
	who := ""
	switch {
	case strings.HasPrefix(authz, "AWS4-HMAC-SHA256 Credential="+stateAK+"/20261010/gra/s3/aws4_request,"):
		who = "state"
	case strings.HasPrefix(authz, "AWS4-HMAC-SHA256 Credential="+janitorAK+"/20261010/gra/s3/aws4_request,"):
		who = "janitor"
	}
	q := r.URL.Query()
	path := strings.TrimPrefix(r.URL.Path, "/")
	b, key, _ := strings.Cut(path, "/")
	op := ""
	switch {
	case key != "" && r.Method == "PUT":
		op = "PutObject"
	case key != "" && r.Method == "GET":
		op = "GetObject"
	case key != "" && r.Method == "DELETE":
		op = "DeleteObject"
	case q.Has("versions") && r.Method == "GET":
		op = "ListObjectVersions"
	case q.Has("policy"):
		op = map[string]string{"PUT": "PutBucketPolicy", "GET": "GetBucketPolicy", "DELETE": "DeleteBucketPolicy"}[r.Method]
	case q.Has("versioning"):
		op = map[string]string{"PUT": "PutBucketVersioning", "GET": "GetBucketVersioning"}[r.Method]
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	bypass := r.Header.Get("X-Amz-Bypass-Governance-Retention") == "true"
	f.log = append(f.log, seen{who: who, op: op, versionID: q.Get("versionId"), bypass: bypass})
	if who == "" || b != bucket || op == "" {
		errorAnswer(rw, 403, "SignatureDoesNotMatch")
		return
	}
	k := who + " " + op
	if f.hangup[k] {
		if conn, _, err := rw.(http.Hijacker).Hijack(); err == nil {
			conn.Close()
		}
		return
	}
	if s := f.refuse[k]; s != 0 {
		errorAnswer(rw, s, "AccessDenied")
		return
	}
	if s := f.fail[k]; s != 0 {
		errorAnswer(rw, s, "InternalError")
		return
	}
	if f.policyNotImplemented && strings.Contains(op, "BucketPolicy") {
		errorAnswer(rw, 501, "NotImplemented")
		return
	}
	if who == "state" {
		denied := false
		switch op {
		case "DeleteObject":
			denied = q.Has("versionId") && !f.denyInert
		case "PutBucketPolicy", "DeleteBucketPolicy":
			denied = !f.stateMayPutPolicy
		case "PutBucketVersioning":
			denied = !f.stateMaySetVersioning
		case "GetBucketPolicy", "GetBucketVersioning":
			denied = true // not in the state user's Allow
		}
		if denied {
			errorAnswer(rw, 403, "AccessDenied")
			return
		}
	}
	switch op {
	case "PutObject":
		f.seq++
		id := "null"
		if f.versioning == "Enabled" {
			id = fmt.Sprintf("v%03d.%s", f.seq, "x+y/z=")
		}
		if f.sameVersionID {
			id = "same"
		}
		if !f.noVersionIDs {
			rw.Header().Set("X-Amz-Version-Id", id)
		}
		f.versions = append(f.versions, version{key: key, id: id})
		rw.WriteHeader(200)
	case "GetObject":
		for i := len(f.versions) - 1; i >= 0; i-- {
			if v := f.versions[i]; v.key == key {
				if v.marker {
					break
				}
				rw.WriteHeader(200)
				fmt.Fprint(rw, "object")
				return
			}
		}
		errorAnswer(rw, 404, "NoSuchKey")
	case "DeleteObject":
		if v := q.Get("versionId"); v != "" {
			// T099: a locked version (not a delete marker) goes only with an honoured bypass.
			locked := !f.lockInert && slices.ContainsFunc(f.versions, func(x version) bool { return x.key == key && x.id == v && !x.marker })
			mayBypass := who == "janitor" && !f.janitorMayNotBypass || who == "state" && f.stateMayBypass
			if locked && !(bypass && mayBypass) {
				errorAnswer(rw, 403, "AccessDenied")
				return
			}
			if !f.keepVersions {
				f.versions = slices.DeleteFunc(f.versions, func(x version) bool { return x.key == key && x.id == v })
			}
		} else {
			f.seq++
			f.versions = append(f.versions, version{key: key, id: fmt.Sprintf("m%03d", f.seq), marker: true})
		}
		rw.WriteHeader(204)
	case "ListObjectVersions":
		if f.wrongRoot {
			rw.WriteHeader(200)
			fmt.Fprint(rw, `<?xml version="1.0"?><Other><IsTruncated>false</IsTruncated></Other>`)
			return
		}
		if f.garbage {
			rw.WriteHeader(200)
			fmt.Fprint(rw, echo+" "+janitorSK+" <<not xml")
			return
		}
		var out bytes.Buffer
		fmt.Fprintf(&out, `<?xml version="1.0" encoding="UTF-8"?><ListVersionsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>%s</Name><IsTruncated>%v</IsTruncated>`, bucket, f.truncated)
		for i, v := range f.versions {
			tag := "Version"
			if v.marker {
				tag = "DeleteMarker"
			}
			var kb, ib bytes.Buffer
			_ = xml.EscapeText(&kb, []byte(v.key))
			_ = xml.EscapeText(&ib, []byte(v.id))
			fmt.Fprintf(&out, "<%s><Key>%s</Key><VersionId>%s</VersionId><IsLatest>%v</IsLatest></%s>", tag, kb.String(), ib.String(), i == len(f.versions)-1, tag)
		}
		out.WriteString("</ListVersionsResult>")
		rw.WriteHeader(200)
		rw.Write(out.Bytes())
	case "PutBucketPolicy":
		f.policy = body
		rw.WriteHeader(204)
	case "GetBucketPolicy":
		if f.policy == nil {
			errorAnswer(rw, 404, "NoSuchBucketPolicy")
			return
		}
		rw.WriteHeader(200)
		rw.Write(f.policy)
	case "DeleteBucketPolicy":
		f.policy = nil
		rw.WriteHeader(204)
	case "PutBucketVersioning":
		var vc struct {
			Status string `xml:"Status"`
		}
		if xml.Unmarshal(body, &vc) != nil || (vc.Status != "Enabled" && vc.Status != "Suspended") {
			errorAnswer(rw, 400, "MalformedXML")
			return
		}
		f.versioning = vc.Status
		rw.WriteHeader(200)
	case "GetBucketVersioning":
		rw.WriteHeader(200)
		if f.versioningWrongRoot {
			fmt.Fprint(rw, `<?xml version="1.0"?><Other><Status>Enabled</Status></Other>`)
			return
		}
		fmt.Fprintf(rw, `<?xml version="1.0"?><VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>%s</Status></VersioningConfiguration>`, f.versioning)
	}
}

func runObserve(t *testing.T, f *fakeBucket, env map[string]string) (Record, []byte, string, error) {
	t.Helper()
	var term bytes.Buffer
	raw, err := Check(f.options()).Observe(context.Background(), env, &term)
	var rec Record
	if raw != nil {
		if jerr := json.Unmarshal(raw, &rec); jerr != nil {
			t.Fatalf("record %s: %v", raw, jerr)
		}
	}
	return rec, raw, term.String(), err
}

func runUndo(t *testing.T, f *fakeBucket, env map[string]string) (string, error) {
	t.Helper()
	var term bytes.Buffer
	err := Check(f.options()).Undo(context.Background(), env, &term)
	return term.String(), err
}

// noLeak: nothing the check records, prints or returns holds a key or the server's text.
func noLeak(t *testing.T, what string, texts ...string) {
	t.Helper()
	for _, s := range texts {
		for _, bad := range append([]string{echo, "Message", "https://", "<Error", "<?xml"}, secrets...) {
			if strings.Contains(s, bad) {
				t.Errorf("%s carries %q:\n%s", what, bad, s)
			}
		}
	}
}

type wantStep struct {
	name, op, expected, observed string
	status                       int
	code                         string
	asExpected                   bool
	credential                   string
}

func wantSteps(t *testing.T, got []Observation, want []wantStep) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d observations %+v, want %d", len(got), got, len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Step != w.name || g.Operation != w.op || g.Credential != w.credential || g.Expected != w.expected || g.Observed != w.observed ||
			g.Status != w.status || g.Code != w.code || g.AsExpected != w.asExpected {
			t.Errorf("observation %d = %+v, want %+v", i, g, w)
		}
	}
}

// enforced is the expected record when OVHcloud enforces the Deny and the Object Lock (T099: the
// state user is also refused the bypass; the janitor is refused a locked version without the
// bypass and allowed it with).
var enforced = []wantStep{
	{"put-v1", "PutObject", "allowed", "allowed", 200, "", true, "state"},
	{"put-v2", "PutObject", "allowed", "allowed", 200, "", true, "state"},
	{"get", "GetObject", "allowed", "allowed", 200, "", true, "state"},
	{"delete-older-version", "DeleteObject", "refused", "refused", 403, "AccessDenied", true, "state"},
	{"delete-older-version-bypass", "DeleteObject", "refused", "refused", 403, "AccessDenied", true, "state"},
	{"put-bucket-policy", "PutBucketPolicy", "refused", "refused", 403, "AccessDenied", true, "state"},
	{"suspend-versioning", "PutBucketVersioning", "refused", "refused", 403, "AccessDenied", true, "state"},
	{"delete-current", "DeleteObject", "allowed", "allowed", 204, "", true, "state"},
	{"janitor-delete-locked", "DeleteObject", "refused", "refused", 403, "AccessDenied", true, "janitor"},
	{"janitor-delete-bypass", "DeleteObject", "allowed", "allowed", 204, "", true, "janitor"},
}

// TestObserveEnforced: the Deny and the Object Lock enforced. The state user writes two versions
// of one object, reads it, is refused the versioned DELETE of the older version (with and without
// the governance bypass), the bucket-policy PUT and the versioning suspension, and deletes the
// object without a version id; then the janitor is refused the newer version without the bypass
// (retention holds even for `s3:*`) and deletes it with the bypass. Every answer is recorded, no
// finding; the rest is still there for Undo, which then empties the bucket with the bypass.
func TestObserveEnforced(t *testing.T) {
	f := newFakeBucket(t)
	rec, raw, term, err := runObserve(t, f, testEnv())
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if rec.Check != "state-backend-deny" || rec.Object != Object || len(rec.Findings) != 0 {
		t.Errorf("record %s: want check state-backend-deny, object %s, no finding", raw, Object)
	}
	wantSteps(t, rec.Observations, enforced)
	reqs := f.requests()
	var got []seen
	for _, r := range reqs {
		got = append(got, seen{who: r.who, op: r.op, versionID: r.versionID, bypass: r.bypass})
	}
	// The versioned DELETEs of the state user name the older version (the first PUT's id), the
	// janitor's the newer one (the second PUT's), so that what the state user may have deleted does
	// not decide the janitor's answers; delete-current names none. Only the two *-bypass steps ask
	// for the bypass.
	want := []seen{
		{"state", "PutObject", "", false}, {"state", "PutObject", "", false}, {"state", "GetObject", "", false},
		{"state", "DeleteObject", "v001.x+y/z=", false}, {"state", "DeleteObject", "v001.x+y/z=", true},
		{"state", "PutBucketPolicy", "", false}, {"state", "PutBucketVersioning", "", false}, {"state", "DeleteObject", "", false},
		{"janitor", "DeleteObject", "v002.x+y/z=", false}, {"janitor", "DeleteObject", "v002.x+y/z=", true},
	}
	if !slices.Equal(got, want) {
		t.Errorf("requests\n%v\nwant\n%v", got, want)
	}
	for _, line := range []string{"LZ-LIVE observe delete-older-version refused 403 AccessDenied", "LZ-LIVE observe put-v1 allowed 200",
		"LZ-LIVE observe janitor-delete-bypass allowed 204"} {
		if !strings.Contains(term, line) {
			t.Errorf("terminal lacks %q:\n%s", line, term)
		}
	}
	if strings.Contains(term, "LZ-LIVE finding") {
		t.Errorf("terminal reports a finding:\n%s", term)
	}
	noLeak(t, "record", string(raw))
	noLeak(t, "terminal", term)
	if vs, _, _ := f.state(); len(vs) != 2 || vs[0].id != "v001.x+y/z=" || !vs[1].marker {
		t.Errorf("after Observe the bucket holds %v, want the older version and a delete marker (Undo removes them)", vs)
	}

	before := len(f.requests())
	uterm, err := runUndo(t, f, testEnv())
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	vs, policy, versioning := f.state()
	if len(vs) != 0 || policy != nil || versioning != "Enabled" {
		t.Errorf("after Undo: versions %v, policy %q, versioning %s; want none, none, Enabled", vs, policy, versioning)
	}
	for _, r := range f.requests()[before:] {
		if r.who != "janitor" {
			t.Errorf("Undo sent %s with the %s credential, want the janitor's only", r.op, r.who)
		}
		// T099: every version Undo deletes may be locked: each DELETE asks for the bypass.
		if r.op == "DeleteObject" && (r.versionID == "" || !r.bypass) {
			t.Errorf("Undo's DeleteObject (version %q, bypass %v): want a version id and the governance bypass", r.versionID, r.bypass)
		}
	}
	if !strings.Contains(uterm, "LZ-LIVE undo removed 2 versions; bucket empty") {
		t.Errorf("undo terminal %q, want the count and the empty bucket", uterm)
	}
	noLeak(t, "undo terminal", uterm)
}

// TestObserveFindings: an answer other than the expected one is a finding — recorded, listed,
// printed — and not an error: the run reports it to the owner (ADR-0009) instead of failing.
func TestObserveFindings(t *testing.T) {
	// T099: the worlds of the lock. With the Deny inert (T010 run 20261010T201153Z-ffbf) the lock
	// alone refuses the state user's versioned DELETE: no finding. Without the lock in force, the
	// versioned DELETEs go through: findings. A bypass honoured for the state user, or refused for
	// the janitor: findings (the latter also leaves Undo unable to empty the bucket).
	allowedState := func(name string) wantStep {
		return wantStep{name, "DeleteObject", "refused", "allowed", 204, "", false, "state"}
	}
	cases := []struct {
		name      string
		knob      func(*fakeBucket)
		steps     map[int]wantStep
		findings  []string
		undoFails bool
	}{
		{"deny-inert-lock-holds", func(f *fakeBucket) { f.denyInert = true }, nil, nil, false},
		// Review r1: a bypass step whose version an earlier step of the run already deleted says
		// nothing about the bypass: skipped (no request, no finding, no error), not a finding.
		{"lock-inert", func(f *fakeBucket) { f.lockInert = true; f.denyInert = true },
			map[int]wantStep{3: allowedState("delete-older-version"), 4: {"delete-older-version-bypass", "DeleteObject", "refused", "skipped", 0, "", false, "state"},
				8: {"janitor-delete-locked", "DeleteObject", "refused", "allowed", 204, "", false, "janitor"},
				9: {"janitor-delete-bypass", "DeleteObject", "allowed", "skipped", 0, "", false, "janitor"}},
			[]string{"delete-older-version", "janitor-delete-locked"}, false},
		{"lock-inert-deny-holds", func(f *fakeBucket) { f.lockInert = true },
			map[int]wantStep{8: {"janitor-delete-locked", "DeleteObject", "refused", "allowed", 204, "", false, "janitor"},
				9: {"janitor-delete-bypass", "DeleteObject", "allowed", "skipped", 0, "", false, "janitor"}},
			[]string{"janitor-delete-locked"}, false},
		{"state-may-bypass", func(f *fakeBucket) { f.denyInert = true; f.stateMayBypass = true },
			map[int]wantStep{4: allowedState("delete-older-version-bypass")}, []string{"delete-older-version-bypass"}, false},
		{"janitor-may-not-bypass", func(f *fakeBucket) { f.janitorMayNotBypass = true },
			map[int]wantStep{9: {"janitor-delete-bypass", "DeleteObject", "allowed", "refused", 403, "AccessDenied", false, "janitor"}},
			[]string{"janitor-delete-bypass"}, true},
		{"policy-not-implemented", func(f *fakeBucket) { f.policyNotImplemented = true },
			map[int]wantStep{5: {"put-bucket-policy", "PutBucketPolicy", "refused", "refused", 501, "NotImplemented", false, "state"}}, []string{"put-bucket-policy"}, false},
		{"policy-accepted", func(f *fakeBucket) { f.stateMayPutPolicy = true },
			map[int]wantStep{5: {"put-bucket-policy", "PutBucketPolicy", "refused", "allowed", 204, "", false, "state"}}, []string{"put-bucket-policy"}, false},
		{"versioning-suspended", func(f *fakeBucket) { f.stateMaySetVersioning = true },
			map[int]wantStep{6: {"suspend-versioning", "PutBucketVersioning", "refused", "allowed", 200, "", false, "state"}}, []string{"suspend-versioning"}, false},
		{"versioning-refused-otherwise", func(f *fakeBucket) { f.fail["state PutBucketVersioning"] = 500 },
			map[int]wantStep{6: {"suspend-versioning", "PutBucketVersioning", "refused", "refused", 500, "InternalError", false, "state"}}, []string{"suspend-versioning"}, false},
		{"get-refused", func(f *fakeBucket) { f.refuse["state GetObject"] = 403 },
			map[int]wantStep{2: {"get", "GetObject", "allowed", "refused", 403, "AccessDenied", false, "state"}}, []string{"get"}, false},
		// A refused DeleteObject with no version id refuses the versioned ones too in this world.
		{"delete-current-refused", func(f *fakeBucket) { f.refuse["state DeleteObject"] = 403; f.denyInert = true },
			map[int]wantStep{7: {"delete-current", "DeleteObject", "allowed", "refused", 403, "AccessDenied", false, "state"}}, []string{"delete-current"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeBucket(t)
			c.knob(f)
			rec, raw, term, err := runObserve(t, f, testEnv())
			if err != nil {
				t.Fatalf("Observe: %v (a finding is no error)", err)
			}
			want := slices.Clone(enforced)
			for i, s := range c.steps {
				want[i] = s
			}
			wantSteps(t, rec.Observations, want)
			skipped := 0
			for _, w := range want {
				if w.observed == "skipped" {
					skipped++
					if !strings.Contains(term, "LZ-LIVE observe "+w.name+" skipped") {
						t.Errorf("terminal does not report %s skipped:\n%s", w.name, term)
					}
				}
			}
			if n := len(f.requests()); n != len(want)-skipped {
				t.Errorf("%d requests, want %d (a skipped step sends none)", n, len(want)-skipped)
			}
			if !slices.Equal(rec.Findings, append([]string{}, c.findings...)) {
				t.Errorf("findings %v, want %v", rec.Findings, c.findings)
			}
			for _, fd := range c.findings {
				if !strings.Contains(term, "LZ-LIVE finding "+fd+":") {
					t.Errorf("terminal does not report the finding %s:\n%s", fd, term)
				}
			}
			if len(c.findings) == 0 && strings.Contains(term, "LZ-LIVE finding") {
				t.Errorf("terminal reports a finding:\n%s", term)
			}
			noLeak(t, "record", string(raw))
			noLeak(t, "terminal", term)
			// What an accepted call changed: versioning suspended; a policy that grants nothing.
			_, policy, versioning := f.state()
			if c.name == "versioning-suspended" && versioning != "Suspended" {
				t.Errorf("versioning %s after an accepted suspension, want Suspended", versioning)
			}
			if c.name == "policy-accepted" && (!strings.Contains(string(policy), `"Effect":"Deny"`) || strings.Contains(string(policy), "Allow") ||
				!strings.Contains(string(policy), `"Resource":"arn:aws:s3:::`+bucket+`"`)) {
				t.Errorf("bucket policy %s: want a Deny on the probe bucket that grants nothing", policy)
			}
			// Whatever the state user changed, Undo restores: no policy, versioning Enabled, empty —
			// unless the janitor's bypass is refused: then a locked version stays and Undo fails.
			_, uerr := runUndo(t, f, testEnv())
			if c.undoFails {
				if uerr == nil {
					t.Fatal("Undo passed although a locked version could not be deleted")
				}
				return
			}
			if uerr != nil {
				t.Fatalf("Undo: %v", uerr)
			}
			if vs, policy, versioning := f.state(); len(vs) != 0 || policy != nil || versioning != "Enabled" {
				t.Errorf("after Undo: versions %v, policy %q, versioning %s", vs, policy, versioning)
			}
		})
	}
}

// versionSteps: the four steps that address a version by its id (T099: two of them the janitor's),
// each observed as given.
func versionSteps(observed string) map[string]string {
	m := map[string]string{}
	for _, s := range []string{"delete-older-version", "delete-older-version-bypass", "janitor-delete-locked", "janitor-delete-bypass"} {
		m[s] = observed
	}
	return m
}

func with(a, b map[string]string) map[string]string {
	maps.Copy(a, b)
	return a
}

// TestObserveIncomplete: a step that cannot run (no version to delete, no answer) is recorded
// not-run or no-answer and makes Observe fail, naming the step; the other steps still run and the
// record is still returned.
func TestObserveIncomplete(t *testing.T) {
	cases := []struct {
		name    string
		knob    func(*fakeBucket)
		want    map[string]string // step → observed
		errHas  string
		minReqs int
	}{
		{"no-version-ids", func(f *fakeBucket) { f.noVersionIDs = true }, versionSteps("not-run"), "delete-older-version", 6},
		{"bucket-not-versioned", func(f *fakeBucket) { f.versioning = "Suspended" }, versionSteps("not-run"), "delete-older-version", 6},
		{"one-version-id", func(f *fakeBucket) { f.sameVersionID = true }, versionSteps("not-run"), "delete-older-version", 6},
		{"put-refused", func(f *fakeBucket) { f.refuse["state PutObject"] = 403 },
			with(versionSteps("not-run"), map[string]string{"put-v1": "refused", "put-v2": "refused"}), "delete-older-version", 6},
		{"hang-up", func(f *fakeBucket) { f.hangup["state PutBucketPolicy"] = true },
			map[string]string{"put-bucket-policy": "no-answer"}, "put-bucket-policy", 10},
		// T099: the janitor's steps run with their own credential and fail on their own.
		{"janitor-hang-up", func(f *fakeBucket) { f.hangup["janitor DeleteObject"] = true },
			map[string]string{"janitor-delete-locked": "no-answer", "janitor-delete-bypass": "no-answer"}, "janitor-delete-bypass", 10},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeBucket(t)
			c.knob(f)
			rec, raw, term, err := runObserve(t, f, testEnv())
			if err == nil || !strings.Contains(err.Error(), c.errHas) {
				t.Fatalf("Observe: %v, want an error naming %s", err, c.errHas)
			}
			if len(rec.Observations) != len(enforced) {
				t.Fatalf("record %s: want all %d steps recorded", raw, len(enforced))
			}
			for _, o := range rec.Observations {
				if w, ok := c.want[o.Step]; ok && (o.Observed != w || o.AsExpected) {
					t.Errorf("%s observed %s (as expected %v), want %s, not as expected", o.Step, o.Observed, o.AsExpected, w)
				}
			}
			if n := len(f.requests()); n < c.minReqs {
				t.Errorf("%d requests: the remaining steps did not run", n)
			}
			noLeak(t, "record", string(raw))
			noLeak(t, "terminal", term)
			noLeak(t, "error", err.Error())
		})
	}
}

// TestUndo: with the janitor's credential, Undo deletes a bucket policy if one is set, enables
// versioning if it is not, deletes every version and delete marker in the bucket and lists again;
// anything it cannot do, or a bucket not empty afterwards, is an error that names no key and no
// server text.
func TestUndo(t *testing.T) {
	t.Run("nothing-written", func(t *testing.T) {
		f := newFakeBucket(t)
		if _, err := runUndo(t, f, testEnv()); err != nil {
			t.Fatalf("Undo of an empty bucket: %v", err)
		}
		var ops []string
		for _, r := range f.requests() {
			ops = append(ops, r.who+" "+r.op)
		}
		want := []string{"janitor GetBucketPolicy", "janitor GetBucketVersioning", "janitor ListObjectVersions", "janitor ListObjectVersions"}
		if !slices.Equal(ops, want) {
			t.Errorf("requests %v, want %v (nothing to delete or restore)", ops, want)
		}
	})
	t.Run("policy-not-implemented", func(t *testing.T) {
		f := newFakeBucket(t)
		f.policyNotImplemented = true
		if _, err := runUndo(t, f, testEnv()); err != nil {
			t.Fatalf("Undo where bucket policies are not implemented: %v", err)
		}
	})
	t.Run("other-keys-too", func(t *testing.T) {
		f := newFakeBucket(t)
		f.versions = []version{{key: "a/b", id: "1"}, {key: Object, id: "2"}, {key: "a/b", id: "3", marker: true}}
		if _, err := runUndo(t, f, testEnv()); err != nil {
			t.Fatalf("Undo: %v", err)
		}
		if vs, _, _ := f.state(); len(vs) != 0 {
			t.Errorf("after Undo %v remain; the probe bucket must be empty", vs)
		}
		// T099: locked versions (the fake locks every version) go only with the bypass, on each.
		n := 0
		for _, r := range f.requests() {
			if r.op == "DeleteObject" {
				n++
				if !r.bypass || r.who != "janitor" {
					t.Errorf("DeleteObject of version %q by %s, bypass %v: want the janitor with the bypass", r.versionID, r.who, r.bypass)
				}
			}
		}
		if n != 3 {
			t.Errorf("%d DeleteObject, want 3", n)
		}
	})
	failing := map[string]func(*fakeBucket){
		// T099: the janitor's bypass refused: the locked version stays, the DELETE is named.
		"delete-locked-bypass-refused": func(f *fakeBucket) { f.janitorMayNotBypass = true },
		"delete-fails":                 func(f *fakeBucket) { f.fail["janitor DeleteObject"] = 500 },
		"delete-refused":               func(f *fakeBucket) { f.refuse["janitor DeleteObject"] = 403 },
		"versions-remain":              func(f *fakeBucket) { f.keepVersions = true },
		"listing-truncated":            func(f *fakeBucket) { f.truncated = true },
		"listing-garbage":              func(f *fakeBucket) { f.garbage = true },
		"listing-wrong-root":           func(f *fakeBucket) { f.wrongRoot = true },
		"versioning-wrong-root":        func(f *fakeBucket) { f.versioningWrongRoot = true },
		// Review r1: a listed entry without a version id would only add a delete marker.
		"entry-without-version": func(f *fakeBucket) { f.versions = append(f.versions, version{key: Object, id: ""}) },
		// Review r1: listed keys are the server's text; no error may carry them.
		"listed-key-refused": func(f *fakeBucket) { f.versions = append(f.versions, version{key: echo + "/../k", id: "v9"}) },
		"listed-key-hang-up": func(f *fakeBucket) {
			f.versions = []version{{key: echo + "-key", id: "v9"}}
			f.hangup["janitor DeleteObject"] = true
		},
		"listing-refused":        func(f *fakeBucket) { f.refuse["janitor ListObjectVersions"] = 403 },
		"listing-hang-up":        func(f *fakeBucket) { f.hangup["janitor ListObjectVersions"] = true },
		"policy-delete-fails":    func(f *fakeBucket) { f.policy = []byte("{}"); f.fail["janitor DeleteBucketPolicy"] = 500 },
		"policy-read-fails":      func(f *fakeBucket) { f.fail["janitor GetBucketPolicy"] = 500 },
		"versioning-read-fails":  func(f *fakeBucket) { f.fail["janitor GetBucketVersioning"] = 500 },
		"versioning-not-enabled": func(f *fakeBucket) { f.versioning = "Suspended"; f.fail["janitor PutBucketVersioning"] = 500 },
	}
	for name, knob := range failing {
		t.Run(name, func(t *testing.T) {
			f := newFakeBucket(t)
			f.versions = []version{{key: Object, id: "v1"}, {key: Object, id: "m2", marker: true}}
			knob(f)
			term, err := runUndo(t, f, testEnv())
			if err == nil {
				t.Fatal("no error")
			}
			// A refused delete stops the undo there, named, before the final listing.
			if strings.HasPrefix(name, "delete-") && !strings.Contains(err.Error(), "DeleteObject of a version answered") {
				t.Errorf("error %q, want the refused DeleteObject named", err)
			}
			noLeak(t, "error", err.Error())
			noLeak(t, "terminal", term)
			if name == "entry-without-version" {
				for _, r := range f.requests() {
					if r.op == "DeleteObject" {
						t.Fatal("Undo deleted before refusing a listed entry without a version id")
					}
				}
			}
		})
	}
	t.Run("restores", func(t *testing.T) {
		f := newFakeBucket(t)
		f.policy, f.versioning = []byte(`{"Statement":[]}`), "Suspended"
		if _, err := runUndo(t, f, testEnv()); err != nil {
			t.Fatalf("Undo: %v", err)
		}
		if _, policy, versioning := f.state(); policy != nil || versioning != "Enabled" {
			t.Errorf("after Undo: policy %q, versioning %s; want none, Enabled", policy, versioning)
		}
	})
	t.Run("bounded", func(t *testing.T) {
		f := newFakeBucket(t)
		for i := range maxVersions + 1 {
			f.versions = append(f.versions, version{key: Object, id: fmt.Sprint(i)})
		}
		if _, err := runUndo(t, f, testEnv()); err == nil {
			t.Error("Undo deleted more than the bound")
		}
		for _, r := range f.requests() {
			if r.op == "DeleteObject" {
				t.Fatal("Undo deleted before checking the bound")
			}
		}
	})
}

// TestEnv: the published variables are checked before any request; a refusal names the key,
// never a value.
func TestEnv(t *testing.T) {
	if err := Check(Options{}).Validate(testEnv()); err != nil {
		t.Fatalf("Validate of the published variables: %v", err)
	}
	bad := map[string]map[string]string{}
	for _, k := range slices.Sorted(maps.Keys(testEnv())) {
		missing := testEnv()
		delete(missing, k)
		bad["missing "+k] = missing
		empty := testEnv()
		empty[k] = ""
		bad["empty "+k] = empty
	}
	extra := testEnv()
	extra["AWS_SESSION_TOKEN"] = "token-value"
	bad["extra key"] = extra
	region := testEnv()
	region["region"] = "GRA11"
	bad["region without endpoint"] = region
	name := testEnv()
	name["bucket"] = "lz-bkt-state"
	bad["not a probe bucket"] = name
	for what, env := range bad {
		t.Run(what, func(t *testing.T) {
			f := newFakeBucket(t)
			_, _, _, oerr := runObserve(t, f, env)
			_, uerr := runUndo(t, f, env)
			// Review r1: lz-live asks Validate first, so that a refusal keeps no root.
			verr := Check(f.options()).Validate(env)
			for _, err := range []error{oerr, uerr, verr} {
				if err == nil {
					t.Fatal("no refusal")
				}
				noLeak(t, "error", err.Error())
				if strings.Contains(err.Error(), "token-value") {
					t.Errorf("error %q carries a value", err)
				}
			}
			if n := len(f.requests()); n != 0 {
				t.Errorf("%d requests before the refusal", n)
			}
		})
	}
}

// TestObserveCancelled: a cancelled context stops the steps; what ran is recorded.
func TestObserveCancelled(t *testing.T) {
	f := newFakeBucket(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw, err := Check(f.options()).Observe(ctx, testEnv(), io.Discard)
	if err == nil {
		t.Fatal("a cancelled check passed")
	}
	if n := len(f.requests()); n != 0 {
		t.Errorf("%d requests after the cancellation", n)
	}
	if raw == nil {
		t.Error("no record of a cancelled check")
	}
}
