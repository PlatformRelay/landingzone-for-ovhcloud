package live

// The account bucket's S3 store (spec 005 FR-010, FR-012; research R13 phases 6–7; T089): the
// bootstrap's publish and verify phases read and write the account-bootstrap envelope through it
// with state.env's S3 keys. A minimal S3 client over net/http, no SDK (evidence/T057.md decision
// request, option A):
//
//   - GET, PUT and HEAD only; any other method is refused before a request is built.
//   - Path-style requests to https://s3.<region>.io.cloud.ovh.net/<bucket>/<key>, the endpoint
//     derived from the Object Storage region and refused unless its host is that pattern (kb
//     storage-and-backup/object-storage/s3-getting-started-with-object-storage.mdx, `endpoint_url`
//     https://s3.<region>.io.cloud.ovh.net, `region = <region_in_lowercase>`, `signature_version =
//     s3v4`; s3-post-object-upload.mdx, path-style https://s3.<region>.io.cloud.ovh.net/<bucket>).
//     That OVHcloud accepts this client's requests is UNVERIFIED until the owner session T044.
//   - AWS Signature Version 4 in the Authorization header, signing host, x-amz-content-sha256
//     (the body's SHA-256) and x-amz-date; the secret key enters only the HMAC. Pinned against the
//     published AWS vectors in testdata/sigv4.
//   - Answers are capped at maxObject, a redirect is never followed, and no error carries the
//     answer's text or a key: an error answer may echo what the request sent.

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// maxObject bounds an object read or written: an envelope is a few KiB.
const maxObject = 1 << 20

// S3Options opens the account bucket's S3 store.
type S3Options struct {
	Region string            // spec.state.region of the manifest (gra)
	Keys   map[string]string // state.env: AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY
	HTTP   *http.Client      // nil: a default client (its redirect policy is always replaced)
	Now    func() time.Time  // nil: time.Now
}

// S3Store is ObjectStore over OVHcloud Object Storage's S3 API.
type S3Store struct {
	host   string // s3.<region>.io.cloud.ovh.net
	signer sigV4
	http   *http.Client
	now    func() time.Time
}

var (
	// objectStorageHost is the only host the store talks to: an Object Storage S3 endpoint of a
	// one-word region (spec.state.region is `^[a-z]+$` in schemas/deployments.schema.json; 3-AZ
	// regions are refused until handled, coordinator decision 2026-10-07).
	objectStorageHost = regexp.MustCompile(`^s3\.[a-z]+\.io\.cloud\.ovh\.net$`)
	// s3Bucket is an S3 bucket name: 3–63 lower-case letters, digits, dots and hyphens.
	s3Bucket = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
)

// ObjectStorageEndpoint derives the S3 endpoint of an Object Storage region (gra →
// https://s3.gra.io.cloud.ovh.net); a region whose host would not match the pattern is refused.
func ObjectStorageEndpoint(region string) (string, error) {
	host := "s3." + strings.ToLower(region) + ".io.cloud.ovh.net"
	if !objectStorageHost.MatchString(host) {
		return "", fmt.Errorf("Object Storage region %q gives no s3.<region>.io.cloud.ovh.net endpoint", region)
	}
	return "https://" + host, nil
}

// NewS3Store opens the store for a region with state.env's keys. A refusal names the variable,
// never a value.
func NewS3Store(o S3Options) (*S3Store, error) {
	endpoint, err := ObjectStorageEndpoint(o.Region)
	if err != nil {
		return nil, err
	}
	for _, k := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"} {
		if o.Keys[k] == "" {
			return nil, fmt.Errorf("S3 store: no %s", k)
		}
	}
	c := http.Client{Timeout: 30 * time.Second}
	if o.HTTP != nil {
		c = *o.HTTP
	}
	// A redirect would re-send the signed request elsewhere.
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	now := o.Now
	if now == nil {
		now = time.Now
	}
	return &S3Store{
		host:   strings.TrimPrefix(endpoint, "https://"),
		signer: sigV4{accessKey: o.Keys["AWS_ACCESS_KEY_ID"], secret: o.Keys["AWS_SECRET_ACCESS_KEY"], region: strings.ToLower(o.Region), service: "s3"},
		http:   &c,
		now:    now,
	}, nil
}

// Get reads an object; a missing one (404) is an error matching fs.ErrNotExist.
func (s *S3Store) Get(bucket, key string) ([]byte, error) {
	return s.request(http.MethodGet, bucket, key, nil)
}

// Put writes an object of at most maxObject bytes.
func (s *S3Store) Put(bucket, key string, data []byte) error {
	_, err := s.request(http.MethodPut, bucket, key, data)
	return err
}

// PutNew writes an object only if none exists (`If-None-Match: *`, the conditional write OpenTofu's
// S3 backend locks with); an existing object answers 412, a typed error (statusOf). That OVHcloud
// honours the condition is UNVERIFIED until T049.
func (s *S3Store) PutNew(bucket, key string, data []byte) error {
	_, err := s.requestWith(http.MethodPut, bucket, key, data, http.Header{"If-None-Match": {"*"}})
	return err
}

// request is the store's one way out: the method gate, the names, the signature, the size caps.
// Only a 200 answer succeeds; a 404 to GET or HEAD is fs.ErrNotExist.
func (s *S3Store) request(method, bucket, key string, body []byte) ([]byte, error) {
	return s.requestWith(method, bucket, key, body, nil)
}

// requestWith is request with extra, unsigned request headers (a write condition).
func (s *S3Store) requestWith(method, bucket, key string, body []byte, extra http.Header) ([]byte, error) {
	switch method {
	case http.MethodGet, http.MethodPut, http.MethodHead:
	default:
		return nil, fmt.Errorf("S3 store: method %q refused (GET, PUT and HEAD only)", method)
	}
	what := method + " " + bucket + "/" + key
	if !s3Bucket.MatchString(bucket) {
		return nil, fmt.Errorf("S3 store: %q is not a bucket name", bucket)
	}
	if err := checkObjectKey(key); err != nil {
		return nil, fmt.Errorf("S3 store: %w", err)
	}
	if len(body) > maxObject {
		return nil, fmt.Errorf("%s: %d bytes, more than %d", what, len(body), maxObject)
	}
	path := "/" + bucket + "/" + key
	u := &url.URL{Scheme: "https", Host: s.host, Path: path, RawPath: escapePath(path)}
	req, err := http.NewRequest(method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%s: bad request", what)
	}
	if body == nil {
		req.Body, req.ContentLength = http.NoBody, 0
	}
	at := s.now().UTC()
	hash := hexSHA256(body)
	req.Header.Set("X-Amz-Date", at.Format(sigV4Time))
	req.Header.Set("X-Amz-Content-Sha256", hash)
	signed := http.Header{"Host": {s.host}, "X-Amz-Date": {at.Format(sigV4Time)}, "X-Amz-Content-Sha256": {hash}}
	_, _, authz := s.signer.sign(method, path, nil, signed, hash, at)
	req.Header.Set("Authorization", authz)
	for k, v := range extra {
		req.Header[k] = v
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, quiet(what, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxObject+1))
	if err != nil {
		return nil, fmt.Errorf("%s: unreadable answer", what)
	}
	if len(raw) > maxObject {
		return nil, fmt.Errorf("%s: answer larger than %d bytes", what, maxObject)
	}
	switch {
	case resp.StatusCode == http.StatusNotFound && method != http.MethodPut:
		return nil, fmt.Errorf("%s: %w", what, fs.ErrNotExist)
	case resp.StatusCode != http.StatusOK:
		// The same text as before, typed so that a refusal (403) can be told apart (T062).
		return nil, &apiStatus{what: what, code: resp.StatusCode}
	}
	return raw, nil
}

// checkObjectKey refuses a key the store would not address as written: empty, a leading or
// doubled slash, a `.` or `..` segment, or a control character.
func checkObjectKey(key string) error {
	if key == "" {
		return fmt.Errorf("empty object key")
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("object key %q has an empty, . or .. segment", key)
		}
	}
	for _, r := range key {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("object key with a control character")
		}
	}
	return nil
}

// ---------------------------------------------------------------- AWS Signature Version 4

// sigV4Time is the x-amz-date format.
const sigV4Time = "20060102T150405Z"

// sigV4 signs requests for one key pair, region and service (AWS SigV4, header form; the
// canonical request, string to sign and signing key as the AWS S3 API Reference
// sig-v4-header-based-auth describes them). It implements only what a GET, PUT or HEAD of one
// object needs, and S3's rule that the path is encoded once and never normalised.
type sigV4 struct {
	accessKey, secret, region, service string
}

// sign returns the canonical request, the string to sign and the Authorization value of a
// request whose every header in header is signed (names folded to lower case; a value's
// surrounding white space trimmed and inner runs collapsed; repeated values joined by commas).
// path is the decoded path; query is signed sorted by encoded name, then value.
func (k sigV4) sign(method, path string, query url.Values, header http.Header, payloadHash string, t time.Time) (creq, sts, authz string) {
	t = t.UTC()
	names := make([]string, 0, len(header))
	values := map[string][]string{}
	for name, vs := range header {
		n := strings.ToLower(name)
		if _, ok := values[n]; !ok {
			names = append(names, n)
		}
		for _, v := range vs {
			values[n] = append(values[n], strings.Join(strings.Fields(v), " "))
		}
	}
	sort.Strings(names)
	var canonHeaders strings.Builder
	for _, n := range names {
		canonHeaders.WriteString(n + ":" + strings.Join(values[n], ",") + "\n")
	}
	signedHeaders := strings.Join(names, ";")
	creq = strings.Join([]string{method, escapePath(path), canonicalQuery(query), canonHeaders.String(), signedHeaders, payloadHash}, "\n")
	scope := t.Format("20060102") + "/" + k.region + "/" + k.service + "/aws4_request"
	sts = strings.Join([]string{"AWS4-HMAC-SHA256", t.Format(sigV4Time), scope, hexSHA256([]byte(creq))}, "\n")
	key := hmacSHA256([]byte("AWS4"+k.secret), t.Format("20060102"))
	for _, part := range []string{k.region, k.service, "aws4_request"} {
		key = hmacSHA256(key, part)
	}
	sig := hex.EncodeToString(hmacSHA256(key, sts))
	authz = "AWS4-HMAC-SHA256 Credential=" + k.accessKey + "/" + scope + ", SignedHeaders=" + signedHeaders + ", Signature=" + sig
	return creq, sts, authz
}

// canonicalQuery is the query's names and values URI-encoded, sorted by name, then value.
func canonicalQuery(q url.Values) string {
	pairs := make([][2]string, 0, len(q))
	for name, vs := range q {
		for _, v := range vs {
			pairs = append(pairs, [2]string{uriEncode(name, true), uriEncode(v, true)})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i][0] != pairs[j][0] {
			return pairs[i][0] < pairs[j][0]
		}
		return pairs[i][1] < pairs[j][1]
	})
	out := make([]string, len(pairs))
	for i, p := range pairs {
		out[i] = p[0] + "=" + p[1]
	}
	return strings.Join(out, "&")
}

// escapePath encodes every byte of path except the unreserved characters and '/', once.
func escapePath(path string) string { return uriEncode(path, false) }

// uriEncode is SigV4's URI encoding: unreserved characters (A–Z a–z 0–9 - . _ ~) kept, every other
// byte %XX in upper-case hex; '/' kept unless slash is set.
func uriEncode(s string, slash bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z', '0' <= c && c <= '9', c == '-', c == '.', c == '_', c == '~':
			b.WriteByte(c)
		case c == '/' && !slash:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

func hexSHA256(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
