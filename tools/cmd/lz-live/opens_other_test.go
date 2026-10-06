//go:build !linux

package main

import "testing"

// watchOpens needs inotify (Linux, where lz-live runs): elsewhere it observes nothing.
func watchOpens(t *testing.T, paths ...string) func() []string {
	t.Helper()
	return func() []string { return nil }
}

const opensObservable = false
