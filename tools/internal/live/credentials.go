package live

// Credential selection by authority (FR-010, research R6, guard G1): every instance runs under the
// authority its stage names (data-model *Stage table*), and its children get that authority's
// credential variables only, read from that authority's files under the bound account:
//
//	bootstrap  sandbox.env (OAuth2) + accounts/<a>/state.env (account bucket S3 keys)
//	platform   accounts/<a>/platform-deployer.env + accounts/<a>/tenants/<t>/platform-state.env
//	tenant     accounts/<a>/tenants/<t>/deployer.env + accounts/<a>/tenants/<t>/state.env
//
// T058 pins the behaviour (credentials_test.go, apply_test.go); T059 implements it. Until then this
// file is a permissive stub that selects nothing.

import "github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"

// CondCredentials names the refusal of a credential file that cannot be the selected authority's:
// a needed variable missing, or a client id or S3 access key that another authority's file holds.
const CondCredentials = "credentials"

// AuthorityOf returns the authority instance id runs under: its stage's principal.
func AuthorityOf(m *stacks.Manifest, id string) (Authority, error) {
	return "", nil
}

// LoadCredentials returns the child variables of instance id's authority (OVH_ENDPOINT,
// OVH_CLIENT_ID, OVH_CLIENT_SECRET, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY), taken from that
// authority's two files for account under configRoot and from no other file (the other
// authorities' files of the account are only compared against). A missing file is *Blocked (the
// producer that writes it has not been applied); a file others can read is a CondFileMode
// refusal; a file without a needed variable, or whose client id or access key another
// authority's file of the account holds, is a CondCredentials refusal. No error carries a value.
func LoadCredentials(configRoot, account string, m *stacks.Manifest, id string) (Authority, map[string]string, error) {
	return "", nil, nil
}
