package live

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// T096: the probe check's S3 client (s3check.go). Its gate admits nine named operations, each with
// its method and sub-resource fixed; every request is signed with the store's SigV4 signer (the
// fake recomputes each signature from what it received); of an answer it keeps the status, a
// well-formed S3 error code, a well-formed version id and only the two listings' bodies; errors
// carry no answer text and no key.

func newTestS3Checker(t *testing.T, f *fakeS3) *S3Checker {
	t.Helper()
	c, err := NewS3Checker(S3Options{Region: "gra", Keys: s3TestKeys(), HTTP: f.client(), Now: func() time.Time { return s3TestNow }})
	if err != nil {
		t.Fatalf("NewS3Checker: %v", err)
	}
	return c
}

// answerWith makes the fake answer every request with status, the version id header (if any) and
// body.
func answerWith(f *fakeS3, status int, version, body string) {
	f.fault = func(rw http.ResponseWriter, r *http.Request) bool {
		if version != "" {
			rw.Header().Set("X-Amz-Version-Id", version)
		}
		rw.WriteHeader(status)
		fmt.Fprint(rw, body)
		return true
	}
}

// TestS3CheckerOperations: each operation goes out with its method, path and sub-resource, signed
// (the fake verified the signature from the wire), the body sent as given; bucket-configuration
// PUTs carry a signed Content-MD5 of the body; only ListObjectVersions and GetBucketVersioning
// return the answer's body.
func TestS3CheckerOperations(t *testing.T) {
	t.Parallel()
	const vid = "3HL4kqtJlcpXroDTDmJ+rmSpXd3dIbrHY"
	cases := []struct {
		call       S3Call
		method     string
		uri        string
		md5, keeps bool
	}{
		// T099: the governance bypass header, signed, on a versioned DeleteObject only.
		{S3Call{Op: S3DeleteObject, Key: "k", VersionID: vid, Bypass: true}, "DELETE", "/" + s3TestBucket + "/k?versionId=3HL4kqtJlcpXroDTDmJ%2BrmSpXd3dIbrHY", false, false},
		{S3Call{Op: S3PutObject, Key: "lz-deny-check/probe.tflock", Body: []byte("v1")}, "PUT", "/" + s3TestBucket + "/lz-deny-check/probe.tflock", false, false},
		// T100: GetObject keeps the object (the storage-iam check classifies the companion's state).
		{S3Call{Op: S3GetObject, Key: "k"}, "GET", "/" + s3TestBucket + "/k", false, true},
		{S3Call{Op: S3DeleteObject, Key: "k"}, "DELETE", "/" + s3TestBucket + "/k", false, false},
		{S3Call{Op: S3DeleteObject, Key: "k", VersionID: vid}, "DELETE", "/" + s3TestBucket + "/k?versionId=3HL4kqtJlcpXroDTDmJ%2BrmSpXd3dIbrHY", false, false},
		{S3Call{Op: S3ListObjectVersions}, "GET", "/" + s3TestBucket + "?versions=", false, true},
		{S3Call{Op: S3PutBucketPolicy, Body: []byte(`{"Statement":[]}`)}, "PUT", "/" + s3TestBucket + "?policy=", true, false},
		{S3Call{Op: S3GetBucketPolicy}, "GET", "/" + s3TestBucket + "?policy=", false, false},
		{S3Call{Op: S3DeleteBucketPolicy}, "DELETE", "/" + s3TestBucket + "?policy=", false, false},
		{S3Call{Op: S3PutBucketVersioning, Body: []byte("<VersioningConfiguration><Status>Suspended</Status></VersioningConfiguration>")}, "PUT", "/" + s3TestBucket + "?versioning=", true, false},
		{S3Call{Op: S3GetBucketVersioning}, "GET", "/" + s3TestBucket + "?versioning=", false, true},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s%s-bypass-%v", tc.call.Op, tc.call.VersionID, tc.call.Bypass), func(t *testing.T) {
			f := newFakeS3(t)
			answerWith(f, http.StatusOK, "v-1", "<Answer>body</Answer>")
			c := newTestS3Checker(t, f)
			call := tc.call
			call.Bucket = s3TestBucket
			got, err := c.Do(context.Background(), call)
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			reqs := f.requests()
			if len(reqs) != 1 {
				t.Fatalf("%d requests, want 1", len(reqs))
			}
			r := reqs[0]
			if r.Method != tc.method || r.RawPath != tc.uri || r.Host != s3TestHost || !r.SignedOK {
				t.Errorf("sent %s %s to %s (signature ok %v), want %s %s to %s, signed", r.Method, r.RawPath, r.Host, r.SignedOK, tc.method, tc.uri, s3TestHost)
			}
			if !bytes.Equal(r.Body, tc.call.Body) {
				t.Errorf("body %q, want %q", r.Body, tc.call.Body)
			}
			sum := md5.Sum(tc.call.Body)
			md := r.Header.Get("Content-Md5")
			signedMD5 := strings.Contains(r.Header.Get("Authorization"), "content-md5")
			if tc.md5 && (md != base64.StdEncoding.EncodeToString(sum[:]) || !signedMD5) {
				t.Errorf("Content-MD5 %q (signed %v), want the body's MD5, signed", md, signedMD5)
			}
			if !tc.md5 && md != "" {
				t.Errorf("Content-MD5 %q on %s", md, tc.call.Op)
			}
			// T099: x-amz-bypass-governance-retention (docs.ovhcloud.com storage-and-backup/
			// object-storage/s3-managing-object-lock.mdx:241-255) only when asked, value true, signed.
			by := r.Header.Values("X-Amz-Bypass-Governance-Retention")
			signedBy := strings.Contains(r.Header.Get("Authorization"), "x-amz-bypass-governance-retention")
			if tc.call.Bypass && (len(by) != 1 || by[0] != "true" || !signedBy) {
				t.Errorf("bypass header %q (signed %v), want exactly true, signed", by, signedBy)
			}
			if !tc.call.Bypass && (len(by) != 0 || signedBy) {
				t.Errorf("bypass header %q (signed %v) on a call that did not ask for it", by, signedBy)
			}
			if got.Status != http.StatusOK || got.Code != "" || got.VersionID != "v-1" {
				t.Errorf("answer %+v, want status 200, no code, version v-1", got)
			}
			if tc.keeps != (string(got.Body) == "<Answer>body</Answer>") || !tc.keeps && got.Body != nil {
				t.Errorf("body %q kept for %s; only GetObject and the two listings keep theirs", got.Body, tc.call.Op)
			}
		})
	}
}

// TestS3CheckerGate: anything outside the nine operations, or an operation with a part it does not
// take, is refused before any request — above all a DELETE of the bucket or of its sub-resources
// other than the policy.
func TestS3CheckerGate(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", 1025)
	refused := map[string]S3Call{
		"DeleteBucket":                {Op: "DeleteBucket"},
		"empty op":                    {Op: ""},
		"lower-case op":               {Op: "deleteobject", Key: "k"},
		"PutBucketAcl":                {Op: "PutBucketAcl", Body: []byte("x")},
		"DeleteBucketLifecycle":       {Op: "DeleteBucketLifecycle"},
		"object op without key":       {Op: S3DeleteObject},
		"bucket op with key":          {Op: S3DeleteBucketPolicy, Key: "k"},
		"list with key":               {Op: S3ListObjectVersions, Key: "k"},
		"version on GetObject":        {Op: S3GetObject, Key: "k", VersionID: "v1"},
		"version on PutObject":        {Op: S3PutObject, Key: "k", VersionID: "v1", Body: []byte("x")},
		"version on bucket op":        {Op: S3DeleteBucketPolicy, VersionID: "v1"},
		"version with space":          {Op: S3DeleteObject, Key: "k", VersionID: "v 1"},
		"version with control":        {Op: S3DeleteObject, Key: "k", VersionID: "v\n1"},
		"version too long":            {Op: S3DeleteObject, Key: "k", VersionID: long},
		"version non-ASCII":           {Op: S3DeleteObject, Key: "k", VersionID: "vä"},
		"body on GetObject":           {Op: S3GetObject, Key: "k", Body: []byte("x")},
		"body on DeleteObject":        {Op: S3DeleteObject, Key: "k", Body: []byte("x")},
		"body on DeleteBucketPolicy":  {Op: S3DeleteBucketPolicy, Body: []byte("x")},
		"body on ListObjectVersions":  {Op: S3ListObjectVersions, Body: []byte("x")},
		"body above the cap":          {Op: S3PutObject, Key: "k", Body: bytes.Repeat([]byte("x"), maxObject+1)},
		"key with dot-dot":            {Op: S3DeleteObject, Key: "a/../b"},
		"key with leading slash":      {Op: S3DeleteObject, Key: "/k"},
		"no bucket":                   {Op: S3ListObjectVersions, Bucket: "-"},
		"bucket with slash":           {Op: S3ListObjectVersions, Bucket: "a/b"},
		"bucket with query":           {Op: S3ListObjectVersions, Bucket: "abc?policy"},
		"put policy without body":     {Op: S3PutBucketPolicy},
		"put versioning without body": {Op: S3PutBucketVersioning},
		// T099: the governance bypass only on a DeleteObject that names a version.
		"bypass without version":        {Op: S3DeleteObject, Key: "k", Bypass: true},
		"bypass on PutObject":           {Op: S3PutObject, Key: "k", Body: []byte("x"), Bypass: true},
		"bypass on GetObject":           {Op: S3GetObject, Key: "k", Bypass: true},
		"bypass on DeleteBucketPolicy":  {Op: S3DeleteBucketPolicy, Bypass: true},
		"bypass on PutBucketVersioning": {Op: S3PutBucketVersioning, Body: []byte("x"), Bypass: true},
		"bypass on ListObjectVersions":  {Op: S3ListObjectVersions, Bypass: true},
	}
	for name, call := range refused {
		t.Run(name, func(t *testing.T) {
			f := newFakeS3(t)
			answerWith(f, http.StatusOK, "", "")
			c := newTestS3Checker(t, f)
			if call.Bucket == "" {
				call.Bucket = s3TestBucket
			}
			if _, err := c.Do(context.Background(), call); err == nil {
				t.Error("no refusal")
			}
			if n := len(f.requests()); n != 0 {
				t.Errorf("%d requests before the refusal, want 0", n)
			}
		})
	}
	// The account bucket's store keeps its own gate: no DELETE (T089), whatever the checker admits.
	f := newFakeS3(t)
	s := newTestS3Store(t, f)
	if _, err := s.request(context.Background(), http.MethodDelete, s3TestBucket, "k", nil); err == nil || len(f.requests()) != 0 {
		t.Errorf("the store sent a DELETE (%v, %d requests)", err, len(f.requests()))
	}
}

// TestS3CheckerAnswers: any status is an answer (no redirect followed, no retry); of a non-2xx
// answer only a well-formed S3 error code is kept; a malformed version id is dropped; errors (cap,
// truncation, transport, cancellation) name the operation and never the answer's text, a key, the
// URL or the host.
func TestS3CheckerAnswers(t *testing.T) {
	t.Parallel()
	echo := "SERVERTEXT " + s3TestSK + " " + s3TestAK
	errorBody := func(code string) string {
		return "<?xml version=\"1.0\"?><Error><Code>" + code + "</Code><Message>" + echo + "</Message></Error>"
	}
	answers := []struct {
		name, version, body string
		status              int
		code, vid           string
	}{
		{"denied", "", errorBody("AccessDenied"), 403, "AccessDenied", ""},
		{"not-implemented", "", errorBody("NotImplemented"), 501, "NotImplemented", ""},
		{"no-policy", "", errorBody("NoSuchBucketPolicy"), 404, "NoSuchBucketPolicy", ""},
		{"server-error", "", errorBody("InternalError"), 500, "InternalError", ""},
		{"code-with-space", "", errorBody("Access Denied"), 403, "unrecognised", ""},
		{"code-echoing", "", errorBody(s3TestSK), 403, "unrecognised", ""},
		{"code-too-long", "", errorBody(strings.Repeat("A", 65)), 403, "unrecognised", ""},
		{"no-xml", "", echo, 403, "unrecognised", ""},
		{"empty", "", "", 403, "unrecognised", ""},
		{"redirect", "", errorBody("PermanentRedirect"), 301, "PermanentRedirect", ""},
		{"no-content", "v-2", "", 204, "", "v-2"},
		{"version-with-space", "v 2", "", 200, "", ""},
		{"version-too-long", strings.Repeat("v", 1025), "", 200, "", ""},
	}
	for _, a := range answers {
		t.Run(a.name, func(t *testing.T) {
			f := newFakeS3(t)
			answerWith(f, a.status, a.version, a.body)
			if a.status == 301 {
				inner := f.fault
				f.fault = func(rw http.ResponseWriter, r *http.Request) bool {
					rw.Header().Set("Location", "https://elsewhere.example/"+s3TestBucket+"/k")
					return inner(rw, r)
				}
			}
			c := newTestS3Checker(t, f)
			got, err := c.Do(context.Background(), S3Call{Op: S3DeleteObject, Bucket: s3TestBucket, Key: "k", VersionID: "v1"})
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			if got.Status != a.status || got.Code != a.code || got.VersionID != a.vid || got.Body != nil {
				t.Errorf("answer %+v, want status %d, code %q, version %q, no body", got, a.status, a.code, a.vid)
			}
			if n := len(f.requests()); n != 1 {
				t.Errorf("%d requests, want 1", n)
			}
		})
	}
	// A listing's error answer keeps no body either.
	f := newFakeS3(t)
	answerWith(f, 403, "", errorBody("AccessDenied"))
	if got, err := newTestS3Checker(t, f).Do(context.Background(), S3Call{Op: S3ListObjectVersions, Bucket: s3TestBucket}); err != nil || got.Body != nil || got.Code != "AccessDenied" {
		t.Errorf("denied listing: %+v, %v; want code AccessDenied, no body", got, err)
	}
	// T100: x-amz-server-side-encryption (SSE-OMK answers AES256: docs.ovhcloud.com
	// storage-and-backup/object-storage/s3-encrypt-your-objects-with-sse-c.mdx:143-160) is kept when
	// it has the shape of an algorithm name, on any answer; anything else is dropped.
	for header, want := range map[string]string{"AES256": "AES256", "aws:kms": "aws:kms", "": "", "AES 256": "", s3TestSK: "", s3TestAK: "", strings.Repeat("A", 33): ""} {
		f := newFakeS3(t)
		f.fault = func(rw http.ResponseWriter, r *http.Request) bool {
			if header != "" {
				rw.Header().Set("X-Amz-Server-Side-Encryption", header)
			}
			fmt.Fprint(rw, "ciphertext")
			return true
		}
		got, err := newTestS3Checker(t, f).Do(context.Background(), S3Call{Op: S3GetObject, Bucket: s3TestBucket, Key: "k"})
		if err != nil || got.SSE != want || string(got.Body) != "ciphertext" {
			t.Errorf("SSE header %q: answer %+v, %v; want SSE %q and the object", header, got, err, want)
		}
	}

	faults := map[string]func(rw http.ResponseWriter, r *http.Request){
		"oversized": func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(http.StatusForbidden)
			rw.Write(bytes.Repeat([]byte("x"), maxObject+1))
		},
		"truncated": func(rw http.ResponseWriter, r *http.Request) {
			rw.Header().Set("Content-Length", "100")
			rw.WriteHeader(http.StatusOK)
			rw.Write([]byte(echo[:10]))
		},
		"reset": func(rw http.ResponseWriter, r *http.Request) {
			if conn, _, err := rw.(http.Hijacker).Hijack(); err == nil {
				conn.Close()
			}
		},
	}
	for name, fault := range faults {
		t.Run(name, func(t *testing.T) {
			f := newFakeS3(t)
			f.fault = func(rw http.ResponseWriter, r *http.Request) bool { fault(rw, r); return true }
			_, err := newTestS3Checker(t, f).Do(context.Background(), S3Call{Op: S3ListObjectVersions, Bucket: s3TestBucket})
			if err == nil {
				t.Fatal("no error")
			}
			noLeak(t, err.Error(), echo)
			if !strings.Contains(err.Error(), "ListObjectVersions") {
				t.Errorf("error %q does not name the operation", err)
			}
		})
	}
	t.Run("cancelled", func(t *testing.T) {
		f := newFakeS3(t)
		answerWith(f, 200, "", "")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := newTestS3Checker(t, f).Do(ctx, S3Call{Op: S3GetObject, Bucket: s3TestBucket, Key: "k"})
		if err == nil || !strings.Contains(err.Error(), "cancelled") {
			t.Errorf("cancelled context: %v, want an error naming the cancellation", err)
		}
		noLeak(t, fmt.Sprint(err), echo)
	})
}

func noLeak(t *testing.T, msg, echo string) {
	t.Helper()
	for _, bad := range []string{"SERVERTEXT", s3TestSK, s3TestAK, echo[:10], "https://", s3TestHost} {
		if strings.Contains(msg, bad) {
			t.Errorf("error %q carries %q", msg, bad)
		}
	}
}

// TestS3CheckerNoSecret: the secret key appears in no request (URL, header, body), the access key
// only in Authorization.
func TestS3CheckerNoSecret(t *testing.T) {
	t.Parallel()
	f := newFakeS3(t)
	answerWith(f, 200, "v-1", "")
	c := newTestS3Checker(t, f)
	for _, call := range []S3Call{
		{Op: S3PutObject, Key: "k", Body: []byte("v1")},
		{Op: S3DeleteObject, Key: "k", VersionID: "v-1"},
		{Op: S3PutBucketPolicy, Body: []byte("{}")},
	} {
		call.Bucket = s3TestBucket
		if _, err := c.Do(context.Background(), call); err != nil {
			t.Fatalf("%s: %v", call.Op, err)
		}
	}
	s3NoSecretIn(t, f, s3TestSK)
	for _, r := range f.requests() {
		h := r.Header.Clone()
		h.Del("Authorization")
		if strings.Contains(fmt.Sprintf("%s %s %v %s", r.RawPath, r.RawQuery, h, r.Body), s3TestAK) {
			t.Errorf("%s %s carries the access key outside Authorization", r.Method, r.Path)
		}
	}
	if _, err := NewS3Checker(S3Options{Region: "gra", Keys: map[string]string{"AWS_ACCESS_KEY_ID": s3TestAK}}); err == nil || strings.Contains(err.Error(), s3TestAK) {
		t.Errorf("checker without a secret key: %v; want a refusal naming the variable only", err)
	}
	if _, err := NewS3Checker(S3Options{Region: "gra11", Keys: s3TestKeys()}); err == nil {
		t.Error("checker for a region with no Object Storage endpoint: no refusal")
	}
}

// Spec 006 T021 (FR-010; data-model *Fixed signatures*; premise P5): the state bucket's Object
// Lock read and its judge. The answers take the AWS GetObjectLockConfiguration shape (the
// configuration OVHcloud documents for `get-object-lock-configuration`, docs.ovhcloud.com
// storage-and-backup/object-storage/s3-managing-object-lock.mdx:167-190; the wire XML and the
// not-found code are AWS's, UNVERIFIED on OVHcloud until the next owner chain run). Governance
// mode with a default retention passes; disabled, absent, compliance mode or no retention fails; a
// read error is `blocked`, never `pass`.

// objectLockXML is a GetObjectLockConfiguration answer: ObjectLockEnabled ("" leaves the element
// out) and the rule's inner XML ("" leaves the rule out).
func objectLockXML(enabled, rule string) string {
	s := `<?xml version="1.0" encoding="UTF-8"?><ObjectLockConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`
	if enabled != "" {
		s += "<ObjectLockEnabled>" + enabled + "</ObjectLockEnabled>"
	}
	if rule != "" {
		s += "<Rule>" + rule + "</Rule>"
	}
	return s + "</ObjectLockConfiguration>"
}

// TestS3CheckerObjectLock: S3Checker.ObjectLock against a fake S3 answer, then judgeObjectLock.
// Every row sends one signed GET of the bucket's `object-lock` sub-resource and nothing else; no
// error carries answer text.
func TestS3CheckerObjectLock(t *testing.T) {
	t.Parallel()
	echo := "SERVERTEXT " + s3TestSK
	errorBody := func(code string) string {
		return "<?xml version=\"1.0\"?><Error><Code>" + code + "</Code><Message>" + echo + "</Message></Error>"
	}
	gov60 := "<DefaultRetention><Mode>GOVERNANCE</Mode><Days>60</Days></DefaultRetention>"
	cases := []struct {
		name    string
		status  int
		body    string
		drop    bool       // close the connection unanswered
		outcome string     // judgeObjectLock's
		parsed  ObjectLock // checked on pass rows
	}{
		// Case 1: governance mode with a default retention, in days or in years.
		{name: "governance-days", status: 200, body: objectLockXML("Enabled", gov60), outcome: "pass", parsed: ObjectLock{Enabled: true, Mode: "GOVERNANCE", Days: 60}},
		{name: "governance-years", status: 200, body: objectLockXML("Enabled", "<DefaultRetention><Mode>GOVERNANCE</Mode><Years>1</Years></DefaultRetention>"), outcome: "pass", parsed: ObjectLock{Enabled: true, Mode: "GOVERNANCE", Years: 1}},
		// Case 2: disabled, absent, compliance mode, no retention.
		{name: "disabled", status: 200, body: objectLockXML("Disabled", gov60), outcome: "fail"},
		{name: "enabled-element-absent", status: 200, body: objectLockXML("", gov60), outcome: "fail"},
		{name: "absent", status: 404, body: errorBody("ObjectLockConfigurationNotFoundError"), outcome: "fail"},
		{name: "compliance", status: 200, body: objectLockXML("Enabled", "<DefaultRetention><Mode>COMPLIANCE</Mode><Days>60</Days></DefaultRetention>"), outcome: "fail"},
		{name: "no-rule", status: 200, body: objectLockXML("Enabled", ""), outcome: "fail"},
		{name: "rule-without-period", status: 200, body: objectLockXML("Enabled", "<DefaultRetention><Mode>GOVERNANCE</Mode></DefaultRetention>"), outcome: "fail"},
		{name: "rule-without-mode", status: 200, body: objectLockXML("Enabled", "<DefaultRetention><Days>60</Days></DefaultRetention>"), outcome: "fail"},
		// Case 3: a read error.
		{name: "denied", status: 403, body: errorBody("AccessDenied"), outcome: "blocked"},
		{name: "server-error", status: 500, body: errorBody("InternalError"), outcome: "blocked"},
		// Only ObjectLockConfigurationNotFoundError says the configuration is absent; any other
		// 404 is a read error, not a missing lock (review r1).
		{name: "no-such-bucket", status: 404, body: errorBody("NoSuchBucket"), outcome: "blocked"},
		{name: "404-unrecognised", status: 404, body: "<html>not found " + echo + "</html>", outcome: "blocked"},
		{name: "undecodable", status: 200, body: "<html>gateway " + echo + "</html>", outcome: "blocked"},
		{name: "dropped", drop: true, outcome: "blocked"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFakeS3(t)
			if tc.drop {
				f.fault = func(rw http.ResponseWriter, r *http.Request) bool {
					if conn, _, err := rw.(http.Hijacker).Hijack(); err == nil {
						conn.Close()
					}
					return true
				}
			} else {
				answerWith(f, tc.status, "", tc.body)
			}
			c := newTestS3Checker(t, f)
			l, err := c.ObjectLock(context.Background(), s3TestBucket)
			reqs := f.requests()
			if len(reqs) == 0 {
				t.Errorf("no request sent (err %v): the lock is read from the bucket", err)
			}
			for _, r := range reqs {
				path, query, _ := strings.Cut(r.RawPath, "?")
				q, qerr := url.ParseQuery(query)
				if r.Method != http.MethodGet || path != "/"+s3TestBucket || qerr != nil || len(q) != 1 || !q.Has("object-lock") || q.Get("object-lock") != "" || !r.SignedOK || len(r.Body) > 0 {
					t.Errorf("sent %s %s (signature ok %v, %d body bytes), want one signed GET /%s?object-lock", r.Method, r.RawPath, r.SignedOK, len(r.Body), s3TestBucket)
				}
			}
			// A dropped GET may be retried once by the transport; every other row sends one.
			if want := map[bool]int{false: 1, true: 2}[tc.drop]; len(reqs) > want {
				t.Errorf("%d requests, want at most %d", len(reqs), want)
			}
			if got := judgeObjectLock(l, err); got != tc.outcome {
				t.Errorf("judgeObjectLock(%+v, %v) = %q, want %q", l, err, got, tc.outcome)
			}
			if tc.outcome == "pass" && (err != nil || !l.Enabled || !strings.EqualFold(l.Mode, tc.parsed.Mode) || l.Days != tc.parsed.Days || l.Years != tc.parsed.Years) {
				t.Errorf("ObjectLock = %+v, %v; want %+v, no error (the AWS shape read as answered)", l, err, tc.parsed)
			}
			if err != nil && (strings.Contains(err.Error(), "SERVERTEXT") || strings.Contains(err.Error(), s3TestSK)) {
				t.Errorf("error %q carries answer text", err)
			}
		})
	}
}

// TestJudgeObjectLock: the judge alone. Only an enabled lock in governance mode with a default
// retention period passes; a read error is `blocked` whatever configuration came with it.
func TestJudgeObjectLock(t *testing.T) {
	t.Parallel()
	readErr := errors.New("GetObjectLockConfiguration lz-bkt-state: request failed")
	cases := []struct {
		name string
		l    ObjectLock
		err  error
		want string
	}{
		{"governance-days", ObjectLock{Enabled: true, Mode: "GOVERNANCE", Days: 1}, nil, "pass"},
		{"governance-years", ObjectLock{Enabled: true, Mode: "GOVERNANCE", Years: 1}, nil, "pass"},
		{"disabled", ObjectLock{Mode: "GOVERNANCE", Days: 1}, nil, "fail"},
		{"absent", ObjectLock{}, nil, "fail"},
		{"compliance", ObjectLock{Enabled: true, Mode: "COMPLIANCE", Days: 1}, nil, "fail"},
		{"no-mode", ObjectLock{Enabled: true, Days: 1}, nil, "fail"},
		{"no-retention", ObjectLock{Enabled: true, Mode: "GOVERNANCE"}, nil, "fail"},
		{"negative-retention", ObjectLock{Enabled: true, Mode: "GOVERNANCE", Days: -1}, nil, "fail"},
		{"negative-retention-years", ObjectLock{Enabled: true, Mode: "GOVERNANCE", Years: -1}, nil, "fail"},
		{"read-error", ObjectLock{}, readErr, "blocked"},
		{"read-error-with-governance", ObjectLock{Enabled: true, Mode: "GOVERNANCE", Days: 1}, readErr, "blocked"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := judgeObjectLock(tc.l, tc.err); got != tc.want {
				t.Errorf("judgeObjectLock(%+v, %v) = %q, want %q", tc.l, tc.err, got, tc.want)
			}
		})
	}
}

// TestJudgeObjectLockMixedPeriod (T026 review r1): a negative period fails even next to a positive
// one in the other unit (AWS answers one of Days or Years; a positive one does not excuse the other).
func TestJudgeObjectLockMixedPeriod(t *testing.T) {
	t.Parallel()
	for _, l := range []ObjectLock{
		{Enabled: true, Mode: "GOVERNANCE", Days: -1, Years: 1},
		{Enabled: true, Mode: "GOVERNANCE", Days: 1, Years: -1},
	} {
		if got := judgeObjectLock(l, nil); got != "fail" {
			t.Errorf("judgeObjectLock(%+v, nil) = %q, want fail", l, got)
		}
	}
	if got := judgeObjectLock(ObjectLock{Enabled: true, Mode: "GOVERNANCE", Days: 1, Years: 1}, nil); got != "pass" {
		t.Errorf("both periods positive: %q, want pass", got)
	}
}
