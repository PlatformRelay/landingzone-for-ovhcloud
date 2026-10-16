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
	"os"
	"path/filepath"
	"slices"
	"strings"

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
	if a == AuthorityBootstrap {
		return "bootstrap:account"
	}
	if w := setOf(m, a, i, tenant); w != "" {
		return w
	}
	return "the tenant-state instance of tenant " + tenant
}

// setOf names the writing instance whose credential set holds file i (0 OAuth2, 1 S3) of authority
// a for tenant (FR-005): account-governance the deployer clients, the tenant's tenant-state
// instance its S3 keys. It is "" for the bootstrap files, which no set holds, and for a tenant
// without a tenant-state row.
func setOf(m *stacks.Manifest, a Authority, i int, tenant string) string {
	switch {
	case a == AuthorityBootstrap:
		return ""
	case i == 0:
		return setInstanceGovernance
	}
	for _, in := range m.Instances {
		if in.Stage == "tenant-state" && in.Tenant == tenant {
			return in.ID
		}
	}
	return ""
}

// setCurrent resolves the generation `current` names in a set directory ("" while `current` does
// not exist). It is a variable only so a test can count the resolutions of one load.
var setCurrent = currentGeneration

// setLoad resolves the credential-set paths of one load. Each set's `current` is resolved at most
// once (data-model: "readers resolve `current` once per load"): a switch between two reads of one
// load cannot give it files of two generations.
type setLoad struct {
	root, account string
	gens          map[string]setGen // by writing instance
}

type setGen struct {
	name string // "" while `current` does not exist
	err  error
}

func newSetLoad(root, account string) *setLoad {
	return &setLoad{root: root, account: account, gens: map[string]setGen{}}
}

// path is the path of file rel (relative to the account directory) of instance's set: below the
// generation `current` names, or the flat path while `current` does not exist.
func (l *setLoad) path(instance, rel string) (string, error) {
	acct, err := AccountDir(l.root, l.account)
	if err != nil {
		return "", err
	}
	if !accountID.MatchString(instance) {
		return "", fmt.Errorf("credential set: instance %q is not a plain path segment", instance)
	}
	if !filepath.IsLocal(rel) || filepath.Clean(rel) != rel {
		return "", fmt.Errorf("credential set: %q is not a path below the account directory", rel)
	}
	g, ok := l.gens[instance]
	if !ok {
		// The set is read only from where its writer put it: no symlink on the way to it (the
		// writer's credentialDir refuses one too). currentGeneration is the writer's own check: a
		// regular file whose whole content (no trimming) is a gen- name of a sibling directory,
		// CREDENTIAL_POINTER_INVALID otherwise.
		if g.err = noSymlinkBelow(acct, filepath.Join("sets", instance)); g.err == nil {
			g.name, g.err = setCurrent(filepath.Join(acct, "sets", instance))
		}
		l.gens[instance] = g
	}
	switch {
	case g.err != nil:
		return "", g.err
	case g.name == "":
		return filepath.Join(acct, rel), nil
	}
	gen := filepath.Join(acct, "sets", instance, g.name)
	if err := noSymlinkBelow(gen, rel); err != nil {
		return "", err
	}
	return filepath.Join(gen, rel), nil
}

// noSymlinkBelow refuses a symlink at any existing component of rel below base; a missing
// component ends the check (the read that follows reports it).
func noSymlinkBelow(base, rel string) error {
	p := base
	for _, c := range strings.Split(rel, string(filepath.Separator)) {
		p = filepath.Join(p, c)
		fi, err := os.Lstat(p)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("credential set: %s is a symlink (symlinks are not followed)", p)
		}
	}
	return nil
}

// CredentialSetPath returns the path of file rel (relative to the account directory, as readers
// use it today) of instance's credential set under root (FR-005, data-model *Credential set*):
// below the generation `current` names, or the flat path while `current` does not exist. A
// pointer that is not a `gen-` name of an existing sibling directory is refused, the error text
// starting CREDENTIAL_POINTER_INVALID.
func CredentialSetPath(root, account, instance, rel string) (string, error) {
	return newSetLoad(root, account).path(instance, rel)
}

// LoadCredentials returns the child variables of instance id's authority (OVH_ENDPOINT,
// OVH_CLIENT_ID, OVH_CLIENT_SECRET, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY), taken from that
// authority's two files for account under configRoot and from no other file (the other
// authorities' files of the account are only compared against). A file a credential set holds is
// read from the generation its writer's `current` names, each `current` resolved once per load,
// and from the flat path while that writer has no `current` (FR-005 migration). A missing file is
// *Blocked naming what writes it; an invalid pointer is refused (CREDENTIAL_POINTER_INVALID); a
// file others can read is a CondFileMode refusal; a file without a needed variable, or whose
// client id or access key another authority's file of the account holds, is a CondCredentials
// refusal. No error carries a value.
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
	sets := newSetLoad(configRoot, account)
	acct := filepath.Join("accounts", account)
	// resolve is the path of file rel (relative to configRoot) as this load reads it, and that
	// path relative to configRoot for messages.
	resolve := func(rel, set string) (string, string, error) {
		if set == "" {
			return filepath.Join(configRoot, rel), rel, nil
		}
		inAcct, err := filepath.Rel(acct, rel)
		if err != nil {
			return "", "", err
		}
		p, err := sets.path(set, inAcct)
		if err != nil {
			return "", "", err
		}
		shown, err := filepath.Rel(configRoot, p)
		if err != nil {
			shown = rel
		}
		return p, shown, nil
	}
	vars := map[string]string{}
	shown := [2]string{}
	for i, rel := range files {
		keys := oauthKeys
		if i == 1 {
			keys = s3Keys
		}
		p, s, err := resolve(rel, setOf(m, a, i, in.Tenant))
		if err != nil {
			return a, nil, fmt.Errorf("%s: %w", id, err)
		}
		shown[i] = s
		v, err := ReadCredentialFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			return a, nil, &Blocked{Phase: "credentials", Detail: fmt.Sprintf("%s: %s does not exist; %s writes it", id, s, writerOf(m, a, i, in.Tenant))}
		}
		if err != nil {
			return a, nil, err
		}
		// Only the needed variables: whatever else the file holds never reaches a child.
		for _, k := range keys {
			if v[k] == "" {
				return a, nil, refuse(CondCredentials, "%s: %s has no %s", id, s, k)
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
	type other struct{ rel, set string }
	var others []other
	for _, oa := range []Authority{AuthorityBootstrap, AuthorityPlatform, AuthorityTenant} {
		for _, t := range append([]string{""}, tenants...) {
			if (oa == AuthorityBootstrap) != (t == "") {
				continue
			}
			f, _ := authorityFiles(account, oa, t)
			for i, rel := range f {
				o := other{rel, setOf(m, oa, i, t)}
				if rel != files[0] && rel != files[1] && !slices.Contains(others, o) {
					others = append(others, o)
				}
			}
		}
	}
	for _, of := range others {
		p, s, err := resolve(of.rel, of.set)
		if err != nil {
			continue // an invalid pointer: nothing to compare (its own stacks refuse it)
		}
		o, err := ReadCredentialFile(p)
		if err != nil {
			continue // absent or unreadable: nothing to compare (its own stack refuses it)
		}
		if o["OVH_CLIENT_ID"] != "" && o["OVH_CLIENT_ID"] == vars["OVH_CLIENT_ID"] {
			return a, nil, refuse(CondCredentials, "%s: the client of %s is also the client of %s", id, shown[0], s)
		}
		if o["AWS_ACCESS_KEY_ID"] != "" && o["AWS_ACCESS_KEY_ID"] == vars["AWS_ACCESS_KEY_ID"] {
			return a, nil, refuse(CondCredentials, "%s: the access key of %s is also the access key of %s", id, shown[1], s)
		}
	}
	return a, vars, nil
}
