//go:build linux

package stacks

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"
)

// Credential-open control of the authoring flow (005 T037): Linux only, because it watches the
// tripwires through inotify, which sees opens by every process, Terramate and git included.

// The authoring flow with credential tripwires in HOME (the bound account's files, the sandbox
// credentials, AWS-style S3 credentials) watched through inotify: no process of the flow, the
// Terramate and git children included, opens one of them or lists their directories.
func TestAuthoringFlowOpensNoCredential(t *testing.T) {
	home := authoringEnvironment(t)
	tripwires := []string{".config/ovh-lz/sandbox.env", ".config/ovh-lz/accounts/sandbox/account.env",
		".config/ovh-lz/accounts/sandbox/state-passphrase.env", ".aws/credentials", ".aws/config"}
	fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatalf("inotify: %v", err)
	}
	defer syscall.Close(fd)
	watched := map[int32]string{}
	watch := func(rel string) {
		wd, err := syscall.InotifyAddWatch(fd, filepath.Join(home, rel), syscall.IN_OPEN|syscall.IN_ACCESS)
		if err != nil {
			t.Fatalf("watch %s: %v", rel, err)
		}
		watched[int32(wd)] = rel
	}
	for _, rel := range tripwires {
		writeFile(t, filepath.Join(home, rel), []byte("tripwire, not a credential\n"))
		if err := os.Chmod(filepath.Join(home, rel), 0o600); err != nil {
			t.Fatal(err)
		}
		watch(rel)
	}
	for _, dir := range []string{".config/ovh-lz", ".config/ovh-lz/accounts/sandbox", ".aws"} {
		watch(dir)
	}
	authoringFlow(t)
	opened := map[string]bool{}
	buf := make([]byte, 64*1024)
	for {
		n, err := syscall.Read(fd, buf)
		if errors.Is(err, syscall.EAGAIN) || n <= 0 {
			break
		}
		if err != nil {
			t.Fatalf("reading inotify events: %v", err)
		}
		for off := 0; off+syscall.SizeofInotifyEvent <= n; {
			ev := (*syscall.InotifyEvent)(unsafe.Pointer(&buf[off]))
			if rel, ok := watched[ev.Wd]; ok && ev.Mask&(syscall.IN_OPEN|syscall.IN_ACCESS) != 0 {
				opened[rel] = true
			}
			off += syscall.SizeofInotifyEvent + int(ev.Len)
		}
	}
	for rel := range opened {
		t.Errorf("the authoring flow opened ~/%s", rel)
	}
}
