package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// The bootstrap's OVHcloud API calls (research R13): GET reads as the sandbox admin's OAuth2 client
// (bearerClient), and the root-key calls of --fresh-account (rootClient, rootkeys.go). No error
// carries text of the API's answer: an error answer may echo what the request sent.

// apiBase is the API base for path: IAM policies are a v2 route, everything else v1
// (kb/api/v2/iam.json, kb/api/v1/*.json).
func apiBase(a API, path string) string {
	if strings.HasPrefix(path, "/iam/") {
		return strings.TrimSuffix(a.BaseURL, "/v1") + "/v2"
	}
	return a.BaseURL
}

// apiStatus is a non-2xx answer: the request and the status code, nothing of the body.
type apiStatus struct {
	what string
	code int
}

func (e *apiStatus) Error() string { return fmt.Sprintf("%s answered %d", e.what, e.code) }

// statusOf is the status code of an apiStatus error, or 0.
func statusOf(err error) int {
	var st *apiStatus
	if errors.As(err, &st) {
		return st.code
	}
	return 0
}

// send runs req and decodes a 2xx answer into out (nil: the answer is discarded).
func send(a API, req *http.Request, what string, out any) error {
	_, err := sendHeader(a, req, what, out)
	return err
}

// sendHeader is send that also returns the answer's headers (v2 pagination).
func sendHeader(a API, req *http.Request, what string, out any) (http.Header, error) {
	resp, err := a.client().Do(req)
	if err != nil {
		return nil, quiet(what, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil || len(raw) > maxBody {
		return nil, fmt.Errorf("%s: unreadable answer", what)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &apiStatus{what: what, code: resp.StatusCode}
	}
	if out == nil {
		return resp.Header, nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return nil, fmt.Errorf("%s: unexpected answer", what)
	}
	return resp.Header, nil
}

// maxPages bounds a paginated v2 listing; a longer one is an error, never cut short.
const maxPages = 100

// bearerClient reads the API as an OAuth2 client (identify and the admin checks): GET only.
type bearerClient struct {
	api API
	tok string
}

// errRejected is a credential the token endpoint refused (400 or 401): the owner's remedy is
// --fresh-account, unlike a transport failure.
var errRejected = errors.New("credential rejected by the token endpoint")

func newBearer(ctx context.Context, a API, c Credential) (bearerClient, error) {
	tok, err := a.token(ctx, c)
	if err != nil {
		err = quietToken(err)
		var code int
		if n, _ := fmt.Sscanf(err.Error(), "token endpoint answered %d", &code); n == 1 && (code == http.StatusBadRequest || code == http.StatusUnauthorized) {
			return bearerClient{}, fmt.Errorf("%w (%d)", errRejected, code)
		}
		return bearerClient{}, err
	}
	return bearerClient{api: a, tok: tok}, nil
}

func (b bearerClient) get(ctx context.Context, path string, out any) error {
	_, err := b.page(ctx, path, "", out)
	return err
}

func (b bearerClient) page(ctx context.Context, path, cursor string, out any) (http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase(b.api, path)+path, nil)
	if err != nil {
		return nil, fmt.Errorf("GET %s: bad request", path)
	}
	req.Header.Set("Authorization", "Bearer "+b.tok)
	req.Header.Set("Accept", "application/json")
	if cursor != "" {
		req.Header.Set("X-Pagination-Cursor", cursor)
	}
	return sendHeader(b.api, req, "GET "+path, out)
}

// getAll reads every page of a v2 listing (kb guide manage-and-operate/api/apiv2.mdx: the next
// page's cursor comes in X-Pagination-Cursor-Next and goes back in X-Pagination-Cursor; its
// absence marks the last page).
func getAll[T any](ctx context.Context, b bearerClient, path string) ([]T, error) {
	var all []T
	cursor := ""
	for range maxPages {
		var page []T
		h, err := b.page(ctx, path, cursor, &page)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if cursor = h.Get("X-Pagination-Cursor-Next"); cursor == "" {
			return all, nil
		}
	}
	return nil, fmt.Errorf("GET %s: more than %d pages", path, maxPages)
}
