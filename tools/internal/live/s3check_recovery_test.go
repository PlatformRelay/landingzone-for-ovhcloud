package live

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Spec 003 T036: the locked-recovery check's two writes through the checker's gate — CopyObject of a
// named version of an object of the same bucket (x-amz-copy-source, URL-encoded, signed, no body)
// and a PutObject with `If-None-Match: *`, signed. Every other combination is refused before any
// request.

// TestS3CheckerCopyAndCondition: what goes out on the wire.
func TestS3CheckerCopyAndCondition(t *testing.T) {
	t.Parallel()
	const vid = "v001.Qx+y/z="
	t.Run("copy", func(t *testing.T) {
		f := newFakeS3(t)
		answerWith(f, http.StatusOK, "v-2", "<CopyObjectResult/>")
		c := newTestS3Checker(t, f)
		got, err := c.Do(context.Background(), S3Call{Op: S3CopyObject, Bucket: s3TestBucket, Key: "p/restore", CopyFrom: "p/restore", CopyVersion: vid})
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		reqs := f.requests()
		if len(reqs) != 1 {
			t.Fatalf("%d requests, want 1", len(reqs))
		}
		r := reqs[0]
		if r.Method != http.MethodPut || r.RawPath != "/"+s3TestBucket+"/p/restore" || !r.SignedOK || len(r.Body) != 0 {
			t.Errorf("sent %s %s (signed %v, %d body bytes), want a signed PUT of /%s/p/restore without a body", r.Method, r.RawPath, r.SignedOK, len(r.Body), s3TestBucket)
		}
		src := r.Header.Values("X-Amz-Copy-Source")
		if len(src) != 1 || !strings.Contains(r.Header.Get("Authorization"), "x-amz-copy-source") {
			t.Fatalf("x-amz-copy-source %q (signed: %v), want exactly one, signed", src, strings.Contains(r.Header.Get("Authorization"), "x-amz-copy-source"))
		}
		path, query, _ := strings.Cut(src[0], "?")
		q, qerr := url.ParseQuery(query)
		if path != "/"+s3TestBucket+"/p/restore" || qerr != nil || q.Get("versionId") != vid || len(q) != 1 {
			t.Errorf("x-amz-copy-source %q, want /%s/p/restore?versionId=<the version, escaped>", src[0], s3TestBucket)
		}
		if r.Header.Get("If-None-Match") != "" {
			t.Errorf("a copy sent If-None-Match")
		}
		if got.Status != http.StatusOK || got.VersionID != "v-2" || got.Body != nil {
			t.Errorf("answer %+v, want 200, version v-2, no body kept", got)
		}
	})

	t.Run("conditional-put", func(t *testing.T) {
		f := newFakeS3(t)
		answerWith(f, http.StatusPreconditionFailed, "", "<Error><Code>PreconditionFailed</Code></Error>")
		c := newTestS3Checker(t, f)
		got, err := c.Do(context.Background(), S3Call{Op: S3PutObject, Bucket: s3TestBucket, Key: "p/state.tflock", Body: []byte(`{"ID":"x"}`), IfNoneMatch: true})
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		r := f.requests()[0]
		if inm := r.Header.Values("If-None-Match"); len(inm) != 1 || inm[0] != "*" || !strings.Contains(r.Header.Get("Authorization"), "if-none-match") || !r.SignedOK {
			t.Errorf("If-None-Match %q (signature ok %v), want exactly *, signed", inm, r.SignedOK)
		}
		if r.Header.Get("X-Amz-Copy-Source") != "" {
			t.Errorf("a conditional PUT sent a copy source")
		}
		if got.Status != http.StatusPreconditionFailed || got.Code != "PreconditionFailed" {
			t.Errorf("answer %+v, want 412 PreconditionFailed", got)
		}
	})

	t.Run("plain-put-unconditional", func(t *testing.T) {
		f := newFakeS3(t)
		answerWith(f, http.StatusOK, "", "")
		c := newTestS3Checker(t, f)
		if _, err := c.Do(context.Background(), S3Call{Op: S3PutObject, Bucket: s3TestBucket, Key: "k", Body: []byte("x")}); err != nil {
			t.Fatal(err)
		}
		if r := f.requests()[0]; r.Header.Get("If-None-Match") != "" || r.Header.Get("X-Amz-Copy-Source") != "" {
			t.Errorf("a plain PUT sent If-None-Match %q or a copy source %q", r.Header.Get("If-None-Match"), r.Header.Get("X-Amz-Copy-Source"))
		}
	})
}

// TestS3CheckerCopyGate: a copy without a well-formed source, a copy source or a condition on
// another operation, is refused before any request.
func TestS3CheckerCopyGate(t *testing.T) {
	t.Parallel()
	refused := map[string]S3Call{
		"copy without source key":       {Op: S3CopyObject, Key: "k", CopyVersion: "v1"},
		"copy without source version":   {Op: S3CopyObject, Key: "k", CopyFrom: "k"},
		"copy source with dot-dot":      {Op: S3CopyObject, Key: "k", CopyFrom: "a/../b", CopyVersion: "v1"},
		"copy source version spaced":    {Op: S3CopyObject, Key: "k", CopyFrom: "k", CopyVersion: "v 1"},
		"copy with a body":              {Op: S3CopyObject, Key: "k", CopyFrom: "k", CopyVersion: "v1", Body: []byte("x")},
		"copy without key":              {Op: S3CopyObject, CopyFrom: "k", CopyVersion: "v1"},
		"copy with query version":       {Op: S3CopyObject, Key: "k", CopyFrom: "k", CopyVersion: "v1", VersionID: "v1"},
		"copy with bypass":              {Op: S3CopyObject, Key: "k", CopyFrom: "k", CopyVersion: "v1", Bypass: true},
		"copy conditional":              {Op: S3CopyObject, Key: "k", CopyFrom: "k", CopyVersion: "v1", IfNoneMatch: true},
		"copy source on PutObject":      {Op: S3PutObject, Key: "k", Body: []byte("x"), CopyFrom: "k", CopyVersion: "v1"},
		"copy version on GetObject":     {Op: S3GetObject, Key: "k", CopyVersion: "v1"},
		"condition on GetObject":        {Op: S3GetObject, Key: "k", IfNoneMatch: true},
		"condition on DeleteObject":     {Op: S3DeleteObject, Key: "k", IfNoneMatch: true},
		"condition on PutBucketPolicy":  {Op: S3PutBucketPolicy, Body: []byte("x"), IfNoneMatch: true},
		"copy source on DeleteObject":   {Op: S3DeleteObject, Key: "k", CopyFrom: "k"},
		"copy source on bucket listing": {Op: S3ListObjectVersions, CopyFrom: "k", CopyVersion: "v1"},
	}
	for name, call := range refused {
		t.Run(name, func(t *testing.T) {
			f := newFakeS3(t)
			answerWith(f, http.StatusOK, "", "")
			c := newTestS3Checker(t, f)
			call.Bucket = s3TestBucket
			if _, err := c.Do(context.Background(), call); err == nil {
				t.Error("no refusal")
			}
			if n := len(f.requests()); n != 0 {
				t.Errorf("%d requests before the refusal, want 0", n)
			}
		})
	}
}
