package live

// S3 calls of a probe check (spec 005 T096): the state-backend probe observes whether OVHcloud
// enforces the state user's Deny statement, which needs a versioned DELETE, a bucket-policy PUT and
// a versioning PUT — calls the account bucket's store (objectstore.go) must never send. So the
// store's gate stays GET, PUT and HEAD, and this checker is a separate client with its own gate: a
// closed set of named S3 operations, each with its method and sub-resource fixed. A DELETE is only
// DeleteObject on an object key (with an optional versionId) or DeleteBucketPolicy on `?policy`;
// a bucket is never deleted. It shares the store's endpoint rule, name checks, SigV4 signer,
// redirect refusal, size cap and quiet transport errors, and keeps of an answer only the status,
// the S3 error code when it is well-formed, a well-formed x-amz-version-id and, for the two
// listings its caller parses, the capped body; no error carries answer text or a key.

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/xml"
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
)

// S3Call is one operation on one bucket.
type S3Call struct {
	Op        S3Op
	Bucket    string
	Key       string // object operations only
	VersionID string // DeleteObject only
	Body      []byte // PutObject, PutBucketPolicy, PutBucketVersioning only
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
	S3GetObject:           {method: http.MethodGet, object: true},
	S3DeleteObject:        {method: http.MethodDelete, object: true},
	S3ListObjectVersions:  {method: http.MethodGet, sub: "versions", keep: true},
	S3PutBucketPolicy:     {method: http.MethodPut, sub: "policy", body: true},
	S3GetBucketPolicy:     {method: http.MethodGet, sub: "policy"},
	S3DeleteBucketPolicy:  {method: http.MethodDelete, sub: "policy"},
	S3PutBucketVersioning: {method: http.MethodPut, sub: "versioning", body: true},
	S3GetBucketVersioning: {method: http.MethodGet, sub: "versioning", keep: true},
}

var (
	// s3VersionID is a version id the checker sends or keeps: printable ASCII without a space.
	s3VersionID = regexp.MustCompile(`^[\x21-\x7e]+$`)
	// s3ErrorCode is an S3 error code as AWS documents them (AccessDenied, NoSuchBucketPolicy).
	s3ErrorCode = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9.]{0,63}$`)
)

// S3Answer is what the checker keeps of an answer.
type S3Answer struct {
	Status    int
	Code      string // a non-2xx answer's S3 error code, or "unrecognised"
	VersionID string // x-amz-version-id, when well-formed
	Body      []byte // a 2xx answer to ListObjectVersions or GetBucketVersioning
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
