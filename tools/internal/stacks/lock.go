// This file is the run locks (FR-009, research R21, guard G14): a run touching an account or
// account-tenant stack holds the `account` lock, a run touching a tenant's stacks holds
// `tenant-<t>`; locks are taken in a fixed order and a second holder is refused.
//
// T040 stub: the signatures lock_test.go pins, with permissive bodies (no lock is ever taken or
// refused). T041 implements them.
package stacks

import "errors"

// ErrLocked is the refusal of a lock another run holds.
var ErrLocked = errors.New("locked: another run holds the lock")

// LockStore takes named locks without waiting: TryLock returns ErrLocked (wrapped) when another
// holder has the lock, else a release function.
type LockStore interface {
	TryLock(name string) (release func() error, err error)
}

// DirLocks is the LockStore of lock files in dir; a lock is released when its holder releases it
// or exits.
func DirLocks(dir string) LockStore {
	return permissiveLocks{}
}

type permissiveLocks struct{}

func (permissiveLocks) TryLock(string) (func() error, error) { return func() error { return nil }, nil }

// RunLocks returns the locks a run over ids needs, in the order they are taken.
func RunLocks(m *Manifest, ids []string) ([]string, error) {
	return nil, nil
}

// HoldRun takes the locks of a run over ids from store; the returned function releases them.
func HoldRun(store LockStore, m *Manifest, ids []string) (release func() error, err error) {
	return func() error { return nil }, nil
}
