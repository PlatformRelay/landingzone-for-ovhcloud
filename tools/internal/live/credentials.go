package live

// Credential selection by authority (FR-010, research R6, guard G1): every instance runs under the
// authority its stage names (data-model *Stage table*), and its children get that authority's
// credential variables only, read from that authority's files under the bound account:
//
//	bootstrap  sandbox.env (OAuth2) + accounts/<a>/state.env (account bucket S3 keys)
//	platform   accounts/<a>/platform-deployer.env + accounts/<a>/tenants/<t>/platform-state.env
//	tenant     accounts/<a>/tenants/<t>/deployer.env + accounts/<a>/tenants/<t>/state.env
//
// bootstrap:account writes the bootstrap files, the lane writes the others after account-governance
// (the deployer clients) and the tenant's tenant-state instance (its S3 keys) are applied (apply.go).

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

// CondCredentials names the refusal of a credential file that cannot be the selected authority's:
// a needed variable missing, or a client id or S3 access key that another authority's file holds.
const CondCredentials = "credentials"

// AuthorityOf returns the authority instance id runs under: its stage's principal.
func AuthorityOf(m *stacks.Manifest, id string) (Authority, error) {
	in, err := m.Row(id)
	if err != nil {
		return "", err
	}
	st, ok := stacks.StageOf(in.Stage)
	if !ok || st.Principal == "" {
		return "", fmt.Errorf("%s: stage %s names no authority", id, in.Stage)
	}
	return Authority(st.Principal), nil
}

var oauthKeys = []string{"OVH_ENDPOINT", "OVH_CLIENT_ID", "OVH_CLIENT_SECRET"}

// authorityFiles are the OAuth2 and S3 files of authority a (of tenant, for platform and tenant),
// relative to the config root.
func authorityFiles(account string, a Authority, tenant string) ([2]string, error) {
	acct := filepath.Join("accounts", account)
	switch a {
	case AuthorityBootstrap:
		return [2]string{"sandbox.env", filepath.Join(acct, "state.env")}, nil
	case AuthorityPlatform:
		return [2]string{filepath.Join(acct, "platform-deployer.env"), filepath.Join(acct, "tenants", tenant, "platform-state.env")}, nil
	case AuthorityTenant:
		return [2]string{filepath.Join(acct, "tenants", tenant, "deployer.env"), filepath.Join(acct, "tenants", tenant, "state.env")}, nil
	}
	return [2]string{}, fmt.Errorf("unknown authority %q", a)
}

// writerOf names what writes file i (0 OAuth2, 1 S3) of authority a for tenant (decision 2,
// 2026-10-08: a missing file says what to run): bootstrap:account the bootstrap files,
// account-governance the deployer clients, the tenant's tenant-state instance its S3 keys.
func writerOf(m *stacks.Manifest, a Authority, i int, tenant string) string {
	switch {
	case a == AuthorityBootstrap:
		return "bootstrap:account"
	case i == 0:
		return "account-governance"
	}
	for _, in := range m.Instances {
		if in.Stage == "tenant-state" && in.Tenant == tenant {
			return in.ID
		}
	}
	return "the tenant-state instance of tenant " + tenant
}

// LoadCredentials returns the child variables of instance id's authority (OVH_ENDPOINT,
// OVH_CLIENT_ID, OVH_CLIENT_SECRET, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY), taken from that
// authority's two files for account under configRoot and from no other file (the other
// authorities' files of the account are only compared against). A missing file is *Blocked naming
// what writes it; a file others can read is a CondFileMode refusal; a file without a needed
// variable, or whose client id or access key another authority's file of the account holds, is a
// CondCredentials refusal. No error carries a value.
func LoadCredentials(configRoot, account string, m *stacks.Manifest, id string) (Authority, map[string]string, error) {
	if !accountID.MatchString(account) {
		return "", nil, fmt.Errorf("account id %q is not a plain path segment", account)
	}
	a, err := AuthorityOf(m, id)
	if err != nil {
		return "", nil, err
	}
	in, _ := m.Row(id)
	files, err := authorityFiles(account, a, in.Tenant)
	if err != nil {
		return a, nil, err
	}
	vars := map[string]string{}
	for i, rel := range files {
		keys := oauthKeys
		if i == 1 {
			keys = s3Keys
		}
		v, err := ReadCredentialFile(filepath.Join(configRoot, rel))
		if errors.Is(err, fs.ErrNotExist) {
			return a, nil, &Blocked{Phase: "credentials", Detail: fmt.Sprintf("%s: %s does not exist; %s writes it", id, rel, writerOf(m, a, i, in.Tenant))}
		}
		if err != nil {
			return a, nil, err
		}
		// Only the needed variables: whatever else the file holds never reaches a child.
		for _, k := range keys {
			if v[k] == "" {
				return a, nil, refuse(CondCredentials, "%s: %s has no %s", id, rel, k)
			}
			vars[k] = v[k]
		}
	}
	// Every other authority file of the account (every tenant's): a shared client or access key is
	// a mismatch (a tenant stack holding the platform's credential, V007).
	var tenants []string
	for _, row := range m.Instances {
		if row.Tenant != "" && !slices.Contains(tenants, row.Tenant) {
			tenants = append(tenants, row.Tenant)
		}
	}
	var others []string
	for _, oa := range []Authority{AuthorityBootstrap, AuthorityPlatform, AuthorityTenant} {
		for _, t := range append([]string{""}, tenants...) {
			if (oa == AuthorityBootstrap) != (t == "") {
				continue
			}
			f, _ := authorityFiles(account, oa, t)
			for _, rel := range f {
				if rel != files[0] && rel != files[1] && !slices.Contains(others, rel) {
					others = append(others, rel)
				}
			}
		}
	}
	for _, rel := range others {
		o, err := ReadCredentialFile(filepath.Join(configRoot, rel))
		if err != nil {
			continue // absent or unreadable: nothing to compare (its own stack refuses it)
		}
		if o["OVH_CLIENT_ID"] != "" && o["OVH_CLIENT_ID"] == vars["OVH_CLIENT_ID"] {
			return a, nil, refuse(CondCredentials, "%s: the client of %s is also the client of %s", id, files[0], rel)
		}
		if o["AWS_ACCESS_KEY_ID"] != "" && o["AWS_ACCESS_KEY_ID"] == vars["AWS_ACCESS_KEY_ID"] {
			return a, nil, refuse(CondCredentials, "%s: the access key of %s is also the access key of %s", id, files[1], rel)
		}
	}
	return a, vars, nil
}
