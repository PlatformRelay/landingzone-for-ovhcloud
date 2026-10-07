// This file is the run locks (FR-009, research R21, guard G14): a run touching an account or
// account-tenant stack holds the `account` lock, a run touching a tenant's stacks (its
// account-tenant stack included) holds `tenant-<t>`; locks are taken in a fixed order (account, then tenants sorted) without waiting,
// and a second holder is refused.
//
// The locks are workstation-local by design (data-model *Credential and state files*): DirLocks
// excludes runs that share one lock directory. Runs on different workstations are not excluded
// here; what serialises their writes is the S3 backend's state lockfile per state key (premise
// P2; UNVERIFIED until the owner's live probe session T010 closes it).
package stacks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sync"
	"syscall"
)

// ErrLocked is the refusal of a lock another run holds.
var ErrLocked = errors.New("locked: another run holds the lock")

// LockStore takes named locks without waiting: TryLock returns ErrLocked (wrapped) when another
// holder has the lock, else a release function.
type LockStore interface {
	TryLock(name string) (release func() error, err error)
}

// DirLocks is the LockStore of lock files <dir>/<name>.lock (directory 0700, files 0600). A lock is
// an exclusive flock(2) on its own open file description, so a second holder is refused in the
// same process as in another one, and the kernel drops the lock when its holder exits: a killed
// run leaves no stale lock.
func DirLocks(dir string) LockStore {
	return dirLocks{dir}
}

type dirLocks struct{ dir string }

// lockName is what a lock name may be: `account` or `tenant-<t>`, never a path.
var lockName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func (d dirLocks) TryLock(name string) (func() error, error) {
	if !lockName.MatchString(name) {
		return nil, fmt.Errorf("lock name %q is not a lock name", name)
	}
	if err := os.MkdirAll(d.dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(d.dir, name+".lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%s: %w", name, ErrLocked)
		}
		return nil, fmt.Errorf("lock %s: %w", name, err)
	}
	var once sync.Once
	var rerr error
	return func() error {
		once.Do(func() {
			rerr = errors.Join(syscall.Flock(int(f.Fd()), syscall.LOCK_UN), f.Close())
		})
		return rerr
	}, nil
}

// RunLocks returns the locks a run over ids needs, in the order they are taken: `account` when
// the run touches an account or account-tenant stack, then `tenant-<t>`, sorted, for every tenant
// whose stacks it touches (an account-tenant stack counts for its tenant). An id that is not a row of the manifest (unknown, empty or external) is
// refused.
func RunLocks(m *Manifest, ids []string) ([]string, error) {
	account := false
	var tenants []string
	for _, id := range ids {
		in, err := m.Row(id)
		if err != nil {
			return nil, err
		}
		if in.Scope == ScopeAccount || in.Scope == ScopeAccountTenant {
			account = true
		}
		if in.Scope == ScopeAccount {
			continue
		}
		// An account-tenant stack (tenant-state) also takes its tenant's lock: it writes the
		// bucket that tenant's runs use (coordinator decision 1, 2026-10-07).
		if in.Tenant == "" {
			return nil, fmt.Errorf("run locks: %s (scope %s) names no tenant", id, in.Scope)
		}
		if !slices.Contains(tenants, in.Tenant) {
			tenants = append(tenants, in.Tenant)
		}
	}
	slices.Sort(tenants)
	var out []string
	if account {
		out = append(out, "account")
	}
	for _, t := range tenants {
		out = append(out, "tenant-"+t)
	}
	return out, nil
}

// HoldRun takes the locks of a run over ids from store, in RunLocks' order; the returned function
// releases them in reverse order. The ids are checked before any lock is taken; a refusal on a
// later lock releases the locks already taken. The caller keeps the release function until the
// run ends (`defer release()`): with DirLocks it holds the lock files open, and a discarded one
// lets the garbage collector close them and drop the locks mid-run.
func HoldRun(store LockStore, m *Manifest, ids []string) (func() error, error) {
	names, err := RunLocks(m, ids)
	if err != nil {
		return nil, err
	}
	var held []func() error
	releaseAll := func() error {
		var errs []error
		for i := len(held) - 1; i >= 0; i-- {
			errs = append(errs, held[i]())
		}
		return errors.Join(errs...)
	}
	for _, n := range names {
		release, err := store.TryLock(n)
		if err != nil {
			return nil, errors.Join(err, releaseAll())
		}
		held = append(held, release)
	}
	return releaseAll, nil
}
