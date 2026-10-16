package live

// S3 calls of a probe check (spec 005 T096): the state-backend probe observes whether OVHcloud
// enforces the state user's Deny statement, which needs a versioned DELETE, a bucket-policy PUT and
// a versioning PUT — calls the account bucket's store (objectstore.go) must never send. So the
// store's gate stays GET, PUT and HEAD, and this checker is a separate client with its own gate: a
// closed set of named S3 operations, each with its method and sub-resource fixed. A DELETE is only
// DeleteObject on an object key (with an optional versionId) or DeleteBucketPolicy on `?policy`;
// a bucket is never deleted. Only a versioned DeleteObject may carry the governance bypass header
// (T099: the probe bucket is locked in governance mode). Spec 003 T036 adds the locked-recovery
// check's two writes: CopyObject of a named version of an object of the same bucket over a key
// (the version restore; the source travels in x-amz-copy-source, no body), and a PutObject with
// `If-None-Match: *` (a conditional write, the S3 lockfile's own exclusion). It shares the store's endpoint rule, name checks, SigV4 signer,
// redirect refusal, size cap and quiet transport errors, and keeps of an answer only the status,
// the S3 error code when it is well-formed, a well-formed x-amz-version-id, a well-formed
// x-amz-server-side-encryption (T100) and, for GetObject (T100: the storage-iam check classifies
// the companion's state object), the two listings its caller parses and the Object Lock
// configuration (spec 006 FR-010), the capped body; no error carries answer text or a key.

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// S3Op is an S3 operation the checker may send.
type S3Op string

// The operations a probe check sends (AWS S3 API names).
const (
	S3PutObject           S3Op = "PutObject"
	S3GetObject           S3Op = "GetObject"
	S3DeleteObject        S3Op = "DeleteObject"
	S3ListObjectVersions  S3Op = "ListObjectVersions"
	S3PutBucketPolicy     S3Op = "PutBucketPolicy"
	S3GetBucketPolicy     S3Op = "GetBucketPolicy"
	S3DeleteBucketPolicy  S3Op = "DeleteBucketPolicy"
	S3PutBucketVersioning S3Op = "PutBucketVersioning"
	S3GetBucketVersioning S3Op = "GetBucketVersioning"
	S3CopyObject          S3Op = "CopyObject" // spec 003 T036
	// S3GetObjectLock reads a bucket's Object Lock configuration (spec 006 FR-010).
	S3GetObjectLock S3Op = "GetObjectLockConfiguration"
)

// S3Call is one operation on one bucket.
type S3Call struct {
	Op        S3Op
	Bucket    string
	Key       string // object operations only
	VersionID string // DeleteObject only
	Body      []byte // PutObject, PutBucketPolicy, PutBucketVersioning only
	// Bypass sends `x-amz-bypass-governance-retention: true` (spec 005 T099): a versioned
	// DeleteObject only, which then deletes a version locked in governance mode if the credential
	// holds s3:BypassGovernanceRetention (docs.ovhcloud.com storage-and-backup/object-storage/
	// s3-managing-object-lock.mdx:57-59, :241-255).
	Bypass bool
	// IfNoneMatch sends `If-None-Match: *` (spec 003 T036): PutObject only, which then writes
	// only when the key has no current object (OpenTofu 1.13.0's S3 lockfile takes its lock this
	// way, internal/backend/remote-state/s3/client.go:313-345). Whether OVHcloud honours it is
	// UNVERIFIED (spec 003 research.md; T028 observes it).
	IfNoneMatch bool
	// CopyFrom and CopyVersion name CopyObject's source (spec 003 T036): a key of the same bucket
	// and its version, both required, sent as x-amz-copy-source
	// `/<bucket>/<key>?versionId=<version>`, URL-encoded (AWS S3 API reference, CopyObject).
	CopyFrom    string
	CopyVersion string
}

// s3OpSpec fixes an operation's request: method, sub-resource (`?versions`, `?policy`,
// `?versioning`; none for an object), whether it names an object key, whether it sends a body,
// and whether its 2xx answer's body is returned.
type s3OpSpec struct {
	method, sub  string
	object, body bool
	keep         bool
}

// s3Ops is the checker's gate.
var s3Ops = map[S3Op]s3OpSpec{
	S3PutObject:           {method: http.MethodPut, object: true, body: true},
	S3GetObject:           {method: http.MethodGet, object: true, keep: true},
	S3DeleteObject:        {method: http.MethodDelete, object: true},
	S3ListObjectVersions:  {method: http.MethodGet, sub: "versions", keep: true},
	S3PutBucketPolicy:     {method: http.MethodPut, sub: "policy", body: true},
	S3GetBucketPolicy:     {method: http.MethodGet, sub: "policy"},
	S3DeleteBucketPolicy:  {method: http.MethodDelete, sub: "policy"},
	S3PutBucketVersioning: {method: http.MethodPut, sub: "versioning", body: true},
	S3GetBucketVersioning: {method: http.MethodGet, sub: "versioning", keep: true},
	S3CopyObject:          {method: http.MethodPut, object: true},
	S3GetObjectLock:       {method: http.MethodGet, sub: "object-lock", keep: true},
}

var (
	// s3VersionID is a version id the checker sends or keeps: printable ASCII without a space.
	s3VersionID = regexp.MustCompile(`^[\x21-\x7e]+$`)
	// s3ErrorCode is an S3 error code as AWS documents them (AccessDenied, NoSuchBucketPolicy).
	s3ErrorCode = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9.]{0,63}$`)
	// s3SSE is a server-side encryption algorithm name as S3 answers it (AES256, aws:kms).
	s3SSE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9:_-]{0,31}$`)
)

// S3Answer is what the checker keeps of an answer.
type S3Answer struct {
	Status    int
	Code      string // a non-2xx answer's S3 error code, or "unrecognised"
	VersionID string // x-amz-version-id, when well-formed
	Body      []byte // a 2xx answer to GetObject, the listings, GetBucketVersioning or the Object Lock read
	SSE       string // x-amz-server-side-encryption, when shaped like an algorithm name (T100)
}

// S3Checker sends a probe check's S3 calls.
type S3Checker struct {
	store *S3Store
}

// NewS3Checker opens the checker for a region with one credential (AWS_ACCESS_KEY_ID,
// AWS_SECRET_ACCESS_KEY); refusals as NewS3Store's.
func NewS3Checker(o S3Options) (*S3Checker, error) {
	s, err := NewS3Store(o)
	if err != nil {
		return nil, err
	}
	return &S3Checker{store: s}, nil
}

// Do sends one call. Only a refused call or a missing or unusable answer is an error; any status
// is an answer.
func (c *S3Checker) Do(ctx context.Context, call S3Call) (S3Answer, error) {
	spec, ok := s3Ops[call.Op]
	if !ok {
		return S3Answer{}, fmt.Errorf("S3 check: operation %q refused", call.Op)
	}
	if !s3Bucket.MatchString(call.Bucket) {
		return S3Answer{}, fmt.Errorf("S3 check: %q is not a bucket name", call.Bucket)
	}
	what := string(call.Op) + " " + call.Bucket
	path := "/" + call.Bucket
	switch {
	case spec.object:
		if err := checkObjectKey(call.Key); err != nil {
			return S3Answer{}, fmt.Errorf("S3 check %s: %w", call.Op, err)
		}
		path += "/" + call.Key
		what += "/" + call.Key
	case call.Key != "":
		return S3Answer{}, fmt.Errorf("S3 check: %s takes no object key", call.Op)
	}
	q := url.Values{}
	if spec.sub != "" {
		q.Set(spec.sub, "")
	}
	if call.VersionID != "" {
		if call.Op != S3DeleteObject || !validVersionID(call.VersionID) {
			return S3Answer{}, fmt.Errorf("S3 check: %s with a version id refused", call.Op)
		}
		q.Set("versionId", call.VersionID)
	}
	if call.Bypass && (call.Op != S3DeleteObject || call.VersionID == "") {
		return S3Answer{}, fmt.Errorf("S3 check: %s with the governance bypass refused (a versioned DeleteObject only)", call.Op)
	}
	if call.IfNoneMatch && call.Op != S3PutObject {
		return S3Answer{}, fmt.Errorf("S3 check: %s with If-None-Match refused (PutObject only)", call.Op)
	}
	var copySource string
	switch {
	case call.Op == S3CopyObject:
		if err := checkObjectKey(call.CopyFrom); err != nil || !validVersionID(call.CopyVersion) {
			return S3Answer{}, fmt.Errorf("S3 check: CopyObject needs a source key and a well-formed source version")
		}
		copySource = escapePath("/"+call.Bucket+"/"+call.CopyFrom) + "?versionId=" + url.QueryEscape(call.CopyVersion)
	case call.CopyFrom != "" || call.CopyVersion != "":
		return S3Answer{}, fmt.Errorf("S3 check: %s takes no copy source", call.Op)
	}
	switch {
	case spec.body && spec.sub != "" && len(call.Body) == 0:
		return S3Answer{}, fmt.Errorf("S3 check: %s without a body", call.Op)
	case !spec.body && call.Body != nil:
		return S3Answer{}, fmt.Errorf("S3 check: %s takes no body", call.Op)
	case len(call.Body) > maxObject:
		return S3Answer{}, fmt.Errorf("%s: %d bytes, more than %d", what, len(call.Body), maxObject)
	}
	s := c.store
	u := &url.URL{Scheme: "https", Host: s.host, Path: path, RawPath: escapePath(path), RawQuery: canonicalQuery(q)}
	req, err := http.NewRequestWithContext(ctx, spec.method, u.String(), bytes.NewReader(call.Body))
	if err != nil {
		return S3Answer{}, fmt.Errorf("%s: bad request", what)
	}
	if call.Body == nil {
		req.Body, req.ContentLength = http.NoBody, 0
	}
	at := s.now().UTC()
	hash := hexSHA256(call.Body)
	signed := http.Header{"Host": {s.host}, "X-Amz-Date": {at.Format(sigV4Time)}, "X-Amz-Content-Sha256": {hash}}
	// AWS requires Content-MD5 on PutBucketVersioning and accepts it on PutBucketPolicy; whether
	// OVHcloud requires it is UNVERIFIED. Signed like every header sent.
	if spec.body && spec.sub != "" {
		sum := md5.Sum(call.Body)
		signed.Set("Content-Md5", base64.StdEncoding.EncodeToString(sum[:]))
	}
	if call.Bypass {
		signed.Set("X-Amz-Bypass-Governance-Retention", "true")
	}
	if call.IfNoneMatch {
		signed.Set("If-None-Match", "*")
	}
	if copySource != "" {
		signed.Set("X-Amz-Copy-Source", copySource)
	}
	for k, v := range signed {
		if k != "Host" {
			req.Header[k] = v
		}
	}
	_, _, authz := s.signer.sign(spec.method, path, q, signed, hash, at)
	req.Header.Set("Authorization", authz)
	resp, err := s.http.Do(req)
	if err != nil {
		return S3Answer{}, quiet(what, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxObject+1))
	if err != nil {
		return S3Answer{}, fmt.Errorf("%s: unreadable answer", what)
	}
	if len(raw) > maxObject {
		return S3Answer{}, fmt.Errorf("%s: answer larger than %d bytes", what, maxObject)
	}
	a := S3Answer{Status: resp.StatusCode}
	if v := resp.Header.Get("X-Amz-Version-Id"); validVersionID(v) {
		a.VersionID = v
	}
	if v := resp.Header.Get("X-Amz-Server-Side-Encryption"); s3SSE.MatchString(v) && !strings.Contains(v, s.signer.accessKey) && !strings.Contains(v, s.signer.secret) {
		a.SSE = v
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		a.Code = s3Code(raw, s.signer.accessKey, s.signer.secret)
		return a, nil
	}
	if spec.keep {
		a.Body = raw
	}
	return a, nil
}

// validVersionID: 1 to 1024 printable ASCII characters, no space.
func validVersionID(v string) bool { return len(v) <= 1024 && s3VersionID.MatchString(v) }

// s3Code is the error code of an S3 error answer (`<Error><Code>…</Code>…</Error>`) when it has
// the shape of one and holds neither key (an answer may echo what the request sent), else
// "unrecognised": nothing else of the answer is kept.
func s3Code(raw []byte, keys ...string) string {
	var e struct {
		XMLName xml.Name
		Code    string `xml:"Code"`
	}
	if xml.Unmarshal(raw, &e) != nil || e.XMLName.Local != "Error" || !s3ErrorCode.MatchString(e.Code) || slices.ContainsFunc(keys, func(k string) bool { return strings.Contains(e.Code, k) }) {
		return "unrecognised"
	}
	return e.Code
}

// ObjectLock is a bucket's Object Lock configuration as GetObjectLockConfiguration answers it
// (spec 006 FR-010): whether it is enabled, and the default retention's mode and period.
type ObjectLock struct {
	Enabled     bool
	Mode        string
	Days, Years int
}

// objectLockNotFound is the S3 error code of a bucket without an Object Lock configuration (AWS's;
// UNVERIFIED on OVHcloud until the next owner chain run, premise P5).
const objectLockNotFound = "ObjectLockConfigurationNotFoundError"

// ObjectLock reads bucket's Object Lock configuration (GetObjectLockConfiguration: the
// `ObjectLockConfiguration` document with `ObjectLockEnabled` and `Rule.DefaultRetention`'s `Mode`,
// `Days`, `Years`; docs.ovhcloud.com storage-and-backup/object-storage/
// s3-managing-object-lock.mdx:167-190). Only a 404 answering objectLockNotFound is an absent
// configuration (the zero ObjectLock, no error); any other non-2xx answer, or a 2xx answer that is
// not that document, is an error naming the bucket, the status and the S3 code, nothing else.
func (c *S3Checker) ObjectLock(ctx context.Context, bucket string) (ObjectLock, error) {
	a, err := c.Do(ctx, S3Call{Op: S3GetObjectLock, Bucket: bucket})
	if err != nil {
		return ObjectLock{}, err
	}
	what := string(S3GetObjectLock) + " " + bucket
	switch {
	case a.Status == http.StatusNotFound && a.Code == objectLockNotFound:
		return ObjectLock{}, nil
	case a.Status < 200 || a.Status > 299:
		return ObjectLock{}, fmt.Errorf("%s: answered %d %s", what, a.Status, a.Code)
	}
	var doc struct {
		XMLName xml.Name
		Enabled string `xml:"ObjectLockEnabled"`
		Rule    *struct {
			Retention struct {
				Mode  string `xml:"Mode"`
				Days  int    `xml:"Days"`
				Years int    `xml:"Years"`
			} `xml:"DefaultRetention"`
		} `xml:"Rule"`
	}
	if xml.Unmarshal(a.Body, &doc) != nil || doc.XMLName.Local != "ObjectLockConfiguration" {
		return ObjectLock{}, errors.New(what + ": the answer is not an Object Lock configuration")
	}
	l := ObjectLock{Enabled: doc.Enabled == "Enabled"}
	if doc.Rule != nil {
		l.Mode, l.Days, l.Years = doc.Rule.Retention.Mode, doc.Rule.Retention.Days, doc.Rule.Retention.Years
	}
	return l, nil
}

// judgeObjectLock judges a state bucket's Object Lock read for `state-bucket-object-lock`: a read
// error is `blocked`, whatever came with it; an enabled lock whose default retention is governance
// mode with a positive period in days or years, and neither negative, is `pass`; anything else
// (disabled, absent, compliance or no mode, no period) is `fail`.
func judgeObjectLock(l ObjectLock, err error) string {
	switch {
	case err != nil:
		return outcomeBlocked
	case l.Enabled && strings.EqualFold(l.Mode, "GOVERNANCE") && l.Days >= 0 && l.Years >= 0 && (l.Days > 0 || l.Years > 0):
		return "pass"
	}
	return "fail"
}
