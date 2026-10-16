package live

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Credential is an OAuth2 client-credentials pair (service account) for one endpoint.
type Credential struct {
	Endpoint     string // e.g. ovh-eu
	ClientID     string
	ClientSecret string
}

// Binding is the bound account from accounts/<account>/account.env.
type Binding struct {
	AccountID string // LZ_ACCOUNT_ID
	Endpoint  string // OVH_ENDPOINT
	Org       string // LZ_ORG
}

// API reaches the OVHcloud API. Production fills it from the endpoint; tests point it at a fake.
type API struct {
	TokenURL string // OAuth2 token endpoint, e.g. https://www.ovh.com/auth/oauth2/token
	BaseURL  string // API base, e.g. https://eu.api.ovh.com/v1
	HTTP     *http.Client
}

// maxBody bounds what the binding reads of any API answer.
const maxBody = 1 << 20

// client never follows a redirect: on 307/308 net/http re-sends the POST body, which holds the
// client secret, to wherever the redirect points.
func (a API) client() *http.Client {
	c := http.Client{Timeout: 30 * time.Second}
	if a.HTTP != nil {
		c = *a.HTTP
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c
}

// callError is a failed binding call that names the call and the status or transport class and
// quotes nothing the answer chose (FR-003): no header, no body, no redirect URL.
type callError struct{ msg string }

func (e *callError) Error() string { return e.msg }

// statusRecorder keeps the status of the last answer: Go's client parses a 3xx answer's Location
// before it consults CheckRedirect and, when that fails, returns an error that quotes the header
// and no response (net/http client.go, "failed to parse Location header").
type statusRecorder struct {
	next http.RoundTripper
	code int
}

func (s *statusRecorder) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := s.next.RoundTrip(r)
	if resp != nil {
		s.code = resp.StatusCode
	}
	return resp, err
}

// do sends req and decodes a 200 JSON answer into v. No error quotes the answer (body, headers, a
// redirect target) or the request: they are the API's, and the request may carry a secret.
func (a API) do(req *http.Request, what string, v any) error {
	c := a.client()
	rec := &statusRecorder{next: c.Transport}
	if rec.next == nil {
		rec.next = http.DefaultTransport
	}
	c.Transport = rec
	resp, err := c.Do(req)
	if err != nil {
		if rec.code/100 == 3 {
			return &callError{fmt.Sprintf("%s answered 3xx", what)}
		}
		// Do's errors are *url.Error (net/http Client.Do), so quiet keeps only the class.
		return &callError{quiet(what, err).Error()}
	}
	defer resp.Body.Close()
	body := io.LimitReader(resp.Body, maxBody)
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, body)
		return &callError{fmt.Sprintf("%s answered %d", what, resp.StatusCode)}
	}
	if err := json.NewDecoder(body).Decode(v); err != nil {
		return &callError{fmt.Sprintf("%s answered 200 with a body that does not decode", what)}
	}
	return nil
}

// token runs the OAuth2 client-credentials grant (OVHcloud guide "Authenticate to the API with
// a service account"): secret in the POST form, never in the URL.
func (a API) token(ctx context.Context, c Credential) (string, error) {
	tok, _, err := a.tokenAnswer(ctx, c)
	return tok, err
}

// tokenAnswer is token with the answer's raw expires_in (absent: nil); spec 003 T032 judges a
// token's residual window against it.
func (a API) tokenAnswer(ctx context.Context, c Credential) (string, json.RawMessage, error) {
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
		"scope":         {"all"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", nil, &callError{"token endpoint: the request cannot be built from the configured URL"}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var t struct {
		AccessToken string          `json:"access_token"`
		ExpiresIn   json.RawMessage `json:"expires_in"`
	}
	if err := a.do(req, "token endpoint", &t); err != nil {
		return "", nil, err
	}
	if t.AccessToken == "" {
		return "", nil, &callError{"token endpoint answered 200 without an access token"}
	}
	return t.AccessToken, t.ExpiresIn, nil
}

// Account returns the account the credential authenticates as, from GET /auth/details, which
// every credential class may call without an IAM action (premise P26). It never calls GET /me,
// which needs account:apiovh:me/get and fails for both deployer classes.
// A failed call's error ends with the next action (FR-003).
func (a API) Account(ctx context.Context, c Credential) (string, error) {
	account, err := a.account(ctx, c)
	if err != nil {
		return "", fmt.Errorf("%w; %s", err, bindNext)
	}
	return account, nil
}

// bindNext is the next action on a failed binding call (FR-003).
const bindNext = "check the API endpoint, the network and the credential, then re-run"

func (a API) account(ctx context.Context, c Credential) (string, error) {
	tok, err := a.token(ctx, c)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.BaseURL+"/auth/details", nil)
	if err != nil {
		return "", &callError{"GET /auth/details: the request cannot be built from the configured URL"}
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	var d struct {
		Account string `json:"account"`
	}
	if err := a.do(req, "GET /auth/details", &d); err != nil {
		return "", err
	}
	if d.Account == "" {
		return "", &callError{"GET /auth/details answered 200 without an account id"}
	}
	return d.Account, nil
}

// Bind checks a credential against the bound account and the manifest org before use (guard
// G13). It returns a *Refusal naming the mismatch; the endpoint and org are compared before the
// credential is sent anywhere.
func Bind(ctx context.Context, api API, c Credential, b Binding, manifestOrg string) error {
	if b.AccountID == "" || b.Endpoint == "" || b.Org == "" {
		// An empty field would match an empty credential endpoint or manifest org.
		return refuse(CondAccount, "account.env binding incomplete (account %q, endpoint %q, org %q)", b.AccountID, b.Endpoint, b.Org)
	}
	if c.Endpoint != b.Endpoint {
		return refuse(CondEndpoint, "credential endpoint %q, bound %q", c.Endpoint, b.Endpoint)
	}
	if manifestOrg != b.Org {
		return refuse(CondOrg, "manifest org %q, bound %q", manifestOrg, b.Org)
	}
	account, err := api.Account(ctx, c)
	if err != nil {
		return err
	}
	if account != b.AccountID {
		// The answered id is not quoted: it is answer-chosen text (FR-003).
		return refuse(CondAccount, "the credential authenticates as another account than the bound %q", b.AccountID)
	}
	return nil
}
