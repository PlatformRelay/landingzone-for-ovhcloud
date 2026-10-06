package live

import (
	"context"
	"net/http"
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

// Account returns the account the credential authenticates as, from GET /auth/details, which
// every credential class may call without an IAM action (premise P26).
//
// Stub (T052). T053 implements it.
func (a API) Account(ctx context.Context, c Credential) (string, error) {
	return "", nil
}

// Bind checks a credential against the bound account and the manifest org before use (guard
// G13). It returns a *Refusal naming the mismatch.
//
// Stub (T052): admits everything. T053 implements it.
func Bind(ctx context.Context, api API, c Credential, b Binding, manifestOrg string) error {
	return nil
}
