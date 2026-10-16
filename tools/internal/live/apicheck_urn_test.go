package live

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Spec 003 T034: the operations the iam-urn check sends (kb api/v2/iam.json: GET
// /iam/policy/{policyId} `account:apiovh:iam/policy/get`; GET /iam/resource with resourceName;
// PUT /iam/resource/{resourceURN} `account:apiovh:iam/resource/edit` with iam.resource.Resource;
// DELETE /iam/resource/{resourceURN}/tag/{key}; tag keys and values per iam.resource.AddTag), a
// created policy's createdAt, and Carries, which lets a check filter one identity's answer
// against every other identity's token and secret.

const (
	apiTestBucket    = "lzprobe-iam-urn-20261016t121500z-5e6f"
	apiTestBucketURN = "urn:v1:eu:resource:storageS3:" + apiTestProject + "/" + apiTestBucket
	apiTestTagKey    = "lzprobe-iam-urn:run-id"
)

func TestAPICheckerURNOps(t *testing.T) {
	t.Parallel()
	tags := map[string]string{"lz:tenant": "lzprobe", apiTestTagKey: "20261016T121500Z-5e6f"}
	escURN := "urn:v1:eu:resource:storageS3:" + apiTestProject + "%2F" + apiTestBucket

	t.Run("operations", func(t *testing.T) {
		for _, tc := range []struct {
			call                APICall
			method, path, query string
			answer              string
		}{
			{APICall{Op: APIGetPolicy, ID: apiTestPolicy}, "GET", "/v2/iam/policy/" + apiTestPolicy, "", `{"id":"` + apiTestPolicy + `"}`},
			{APICall{Op: APIListResources, Name: apiTestBucket}, "GET", "/v2/iam/resource", "resourceName=" + apiTestBucket, `[]`},
			{APICall{Op: APIPutResourceTags, URN: apiTestBucketURN, Tags: tags}, "PUT", "/v2/iam/resource/" + escURN, "", `{}`},
			{APICall{Op: APIDeleteResourceTag, URN: apiTestBucketURN, Key: apiTestTagKey}, "DELETE", "/v2/iam/resource/" + escURN + "/tag/" + apiTestTagKey, "", `null`},
		} {
			t.Run(string(tc.call.Op), func(t *testing.T) {
				f := newFakeCheckAPI(t)
				f.answer = func(rw http.ResponseWriter, r *http.Request) { fmt.Fprint(rw, tc.answer) }
				got, err := newTestAPIChecker(t, f).Do(context.Background(), tc.call)
				if err != nil || got.Status != http.StatusOK {
					t.Fatalf("Do = %+v, %v", got, err)
				}
				reqs := f.requests()
				if len(reqs) != 1 {
					t.Fatalf("%d requests, want 1", len(reqs))
				}
				r := reqs[0]
				if r.Method != tc.method || r.Path != tc.path || r.Query != tc.query || r.Auth != "Bearer "+apiTestToken {
					t.Errorf("sent %s %s?%s (auth %q), want %s %s?%s with the bearer token", r.Method, r.Path, r.Query, r.Auth, tc.method, tc.path, tc.query)
				}
				if tc.call.Op == APIPutResourceTags {
					var body struct {
						Tags map[string]string `json:"tags"`
					}
					if json.Unmarshal(r.Body, &body) != nil || !maps.Equal(body.Tags, tags) || r.ContentType != "application/json" {
						t.Errorf("body %s (%s), want exactly {\"tags\": %v} as JSON", r.Body, r.ContentType, tags)
					}
				} else if len(r.Body) != 0 {
					t.Errorf("body %q on %s", r.Body, tc.call.Op)
				}
			})
		}
	})

	t.Run("gate", func(t *testing.T) {
		for name, call := range map[string]APICall{
			"get policy not a uuid":        {Op: APIGetPolicy, ID: "../me"},
			"get policy with a name":       {Op: APIGetPolicy, ID: apiTestPolicy, Name: "x"},
			"list without name":            {Op: APIListResources},
			"list with a query in name":    {Op: APIListResources, Name: apiTestBucket + "&resourceType=x"},
			"list with urn":                {Op: APIListResources, Name: apiTestBucket, URN: apiTestBucketURN},
			"put without tags":             {Op: APIPutResourceTags, URN: apiTestBucketURN},
			"put without urn":              {Op: APIPutResourceTags, Tags: tags},
			"put a wildcard urn":           {Op: APIPutResourceTags, URN: "urn:v1:eu:resource:storageS3:*", Tags: tags},
			"put an identity urn":          {Op: APIPutResourceTags, URN: "urn:v1:eu:identity:credential:xx1111-ovh/" + apiTestBucket, Tags: tags},
			"put a urn with a query":       {Op: APIPutResourceTags, URN: apiTestBucketURN + "?x=1", Tags: tags},
			"put a urn with dot segments":  {Op: APIPutResourceTags, URN: "urn:v1:eu:resource:storageS3:../../me", Tags: tags},
			"put a tag key with a space":   {Op: APIPutResourceTags, URN: apiTestBucketURN, Tags: map[string]string{"a b": "c"}},
			"put a tag value with a space": {Op: APIPutResourceTags, URN: apiTestBucketURN, Tags: map[string]string{"a": "b c"}},
			"put a long tag key":           {Op: APIPutResourceTags, URN: apiTestBucketURN, Tags: map[string]string{strings.Repeat("k", 129): ""}},
			"put with a key":               {Op: APIPutResourceTags, URN: apiTestBucketURN, Tags: tags, Key: apiTestTagKey},
			"delete tag without key":       {Op: APIDeleteResourceTag, URN: apiTestBucketURN},
			"delete tag a slash key":       {Op: APIDeleteResourceTag, URN: apiTestBucketURN, Key: "a/../b"},
			"delete tag with tags":         {Op: APIDeleteResourceTag, URN: apiTestBucketURN, Key: apiTestTagKey, Tags: tags},
			"tags on another op":           {Op: APIGetMe, Tags: tags},
			"urn on another op":            {Op: APIGetMe, URN: apiTestBucketURN},
		} {
			t.Run(name, func(t *testing.T) {
				f := newFakeCheckAPI(t)
				if _, err := newTestAPIChecker(t, f).Do(context.Background(), call); err == nil {
					t.Error("no refusal")
				}
				if n := len(f.requests()); n != 0 {
					t.Errorf("%d requests before the refusal, want 0", n)
				}
			})
		}
	})

	t.Run("list keeps the exact name, every page", func(t *testing.T) {
		f := newFakeCheckAPI(t)
		f.answer = func(rw http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Pagination-Cursor") == "" {
				rw.Header().Set("X-Pagination-Cursor-Next", "p2")
				fmt.Fprintf(rw, `[{"urn":%q,"name":%q,"tags":{"lz:tenant":"lzprobe"}},{"urn":"urn:v1:eu:resource:storageS3:x/other","name":"other","tags":{"SERVER TEXT":"x y"}}]`, apiTestBucketURN, apiTestBucket)
				return
			}
			fmt.Fprintf(rw, `[{"urn":"urn:v1:eu:resource:storageS3:y/%s","name":%q,"tags":null}]`, apiTestBucket, apiTestBucket)
		}
		got, err := newTestAPIChecker(t, f).Do(context.Background(), APICall{Op: APIListResources, Name: apiTestBucket})
		if err != nil || got.Status != http.StatusOK {
			t.Fatalf("Do = %+v, %v", got, err)
		}
		want := []APIResource{
			{URN: apiTestBucketURN, Name: apiTestBucket, Tags: map[string]string{"lz:tenant": "lzprobe"}},
			{URN: "urn:v1:eu:resource:storageS3:y/" + apiTestBucket, Name: apiTestBucket, Tags: map[string]string{}},
		}
		if len(got.Resources) != len(want) {
			t.Fatalf("resources %+v, want %+v", got.Resources, want)
		}
		for i := range want {
			if got.Resources[i].URN != want[i].URN || got.Resources[i].Name != want[i].Name || !maps.Equal(got.Resources[i].Tags, want[i].Tags) || got.Resources[i].Tags == nil {
				t.Errorf("resource %d = %+v, want %+v", i, got.Resources[i], want[i])
			}
		}
		if n := len(f.requests()); n != 2 {
			t.Errorf("%d requests, want both pages", n)
		}
	})

	t.Run("answers of another shape", func(t *testing.T) {
		for name, answer := range map[string]string{
			"not an array":            `{"SERVERTEXT":1}`,
			"null":                    `null`,
			"a null entry":            `[null]`,
			"the name with a bad urn": `[{"urn":"SERVERTEXT *","name":"` + apiTestBucket + `","tags":{}}]`,
		} {
			t.Run(name, func(t *testing.T) {
				f := newFakeCheckAPI(t)
				f.answer = func(rw http.ResponseWriter, r *http.Request) { fmt.Fprint(rw, answer) }
				got, err := newTestAPIChecker(t, f).Do(context.Background(), APICall{Op: APIListResources, Name: apiTestBucket})
				if err == nil {
					t.Fatalf("Do = %+v, want an error", got)
				}
				apiNoLeak(t, err.Error())
			})
		}
	})

	t.Run("tags of another shape are dropped and flagged", func(t *testing.T) {
		// Review r1: a tag the checker cannot hold fails neither the listing (Undo and Sweep need
		// it) nor reaches the caller; the resource is marked partial, so no write puts back less.
		for name, tag := range map[string]string{"key": `"SERVER TEXT":"x"`, "value": `"k":"SERVER TEXT"`, "value of the run key": `"lzprobe-iam-urn:run-id":"SERVER TEXT"`} {
			t.Run(name, func(t *testing.T) {
				f := newFakeCheckAPI(t)
				f.answer = func(rw http.ResponseWriter, r *http.Request) {
					body := fmt.Sprintf(`[{"urn":%q,"name":%q,"tags":{%s,%q:"run"}}]`, apiTestBucketURN, apiTestBucket, tag, apiTestTagKey)
					if name == "value of the run key" {
						body = fmt.Sprintf(`[{"urn":%q,"name":%q,"tags":{%s}}]`, apiTestBucketURN, apiTestBucket, tag)
					}
					fmt.Fprint(rw, body)
				}
				got, err := newTestAPIChecker(t, f).Do(context.Background(), APICall{Op: APIListResources, Name: apiTestBucket})
				if err != nil || len(got.Resources) != 1 {
					t.Fatalf("Do = %+v, %v; want the resource", got, err)
				}
				r := got.Resources[0]
				want := map[string]string{apiTestTagKey: "run"}
				switch name {
				case "value":
					want["k"] = ""
				case "value of the run key":
					want = map[string]string{apiTestTagKey: ""}
				}
				if !r.Partial || !maps.Equal(r.Tags, want) {
					t.Errorf("resource %+v, want partial with tags %v (a key of AddTag's shape kept, its odd value blanked)", r, want)
				}
			})
		}
		f := newFakeCheckAPI(t)
		f.answer = func(rw http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(rw, `[{"urn":%q,"name":%q,"tags":{"k":"v"}}]`, apiTestBucketURN, apiTestBucket)
		}
		if got, err := newTestAPIChecker(t, f).Do(context.Background(), APICall{Op: APIListResources, Name: apiTestBucket}); err != nil || len(got.Resources) != 1 || got.Resources[0].Partial {
			t.Errorf("Do = %+v, %v; want a complete resource", got, err)
		}
	})

	t.Run("refusal keeps status and actions", func(t *testing.T) {
		f := newFakeCheckAPI(t)
		f.answer = func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(http.StatusForbidden)
			fmt.Fprintf(rw, `{"message":"SERVERTEXT %s","details":{"unauthorizedActionsByIAM":"account:apiovh:iam/resource/edit"}}`, apiTestToken)
		}
		for _, call := range []APICall{
			{Op: APIGetPolicy, ID: apiTestPolicy},
			{Op: APIListResources, Name: apiTestBucket},
			{Op: APIPutResourceTags, URN: apiTestBucketURN, Tags: tags},
			{Op: APIDeleteResourceTag, URN: apiTestBucketURN, Key: apiTestTagKey},
		} {
			got, err := newTestAPIChecker(t, f).Do(context.Background(), call)
			if err != nil || got.Status != http.StatusForbidden || len(got.Actions) != 1 || got.Actions[0] != "account:apiovh:iam/resource/edit" || len(got.Resources) != 0 {
				t.Errorf("%s: Do = %+v, %v; want the 403 and its named action only", call.Op, got, err)
			}
		}
	})

	t.Run("created policy keeps its createdAt", func(t *testing.T) {
		f := newFakeCheckAPI(t)
		at := time.Date(2026, 10, 16, 12, 15, 3, 0, time.UTC)
		f.answer = func(rw http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(rw, `{"id":%q,"createdAt":%q}`, apiTestPolicy, at.Format(time.RFC3339))
		}
		got, err := newTestAPIChecker(t, f).Do(context.Background(), APICall{Op: APICreatePolicy, Body: []byte(`{"name":"x"}`)})
		if err != nil || got.ID != apiTestPolicy || !got.CreatedAt[apiTestPolicy].Equal(at) {
			t.Errorf("Do = %+v, %v; want the id and its createdAt %s", got, err, at)
		}
	})

	t.Run("carries", func(t *testing.T) {
		c := newTestAPIChecker(t, newFakeCheckAPI(t))
		for s, want := range map[string]bool{
			"x" + apiTestToken + "y":  true,
			"x" + apiTestSecret + "y": true,
			"account:apiovh:me/get":   false,
			"":                        false,
		} {
			if got := c.Carries(s); got != want {
				t.Errorf("Carries(%q) = %v, want %v", s, got, want)
			}
		}
	})
}
