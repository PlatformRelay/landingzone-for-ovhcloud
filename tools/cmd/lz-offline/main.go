// lz-offline is installed from an independently approved build outside the
// candidate. No candidate configuration is interpreted by this host process.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/PlatformRelay/ovh-landing-zone-accelerator/tools/internal/checks"
)

// Set by the independently reviewed build recipe, never by candidate flags.
var manifestSHA string

type identity struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Mode  uint32 `json:"mode"`
}
type resources struct {
	Prepared checks.Prepared     `json:"prepared"`
	Files    map[string]identity `json:"files"`
	Bwrap    string              `json:"bwrap"`
	BwrapSHA string              `json:"bwrap_sha256"`
}

func fail(err error)          { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
func hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func realDirectory(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("INPUT_PATH: absolute clean directory required")
	}
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("INPUT_PATH: real directory required: %s", current)
		}
		if current == "/" {
			break
		}
	}
	return nil
}
func regular(path string) ([]byte, error) {
	if err := realDirectory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("INPUT_FILE: regular non-symlink file required: %s", path)
	}
	if metadata, ok := info.Sys().(*syscall.Stat_t); !ok || metadata.Nlink != 1 {
		return nil, fmt.Errorf("INPUT_FILE: single link required: %s", path)
	}
	return os.ReadFile(path)
}

// Open every component without following links, then enforce the remaining
// candidate budget before allocating or reading. Growth cannot exceed the reader.
func openCandidate(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("INPUT_FILE: absolute path required")
	}
	descriptor, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for index, part := range parts {
		flags := syscall.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
		if index != len(parts)-1 {
			flags |= syscall.O_DIRECTORY
		} else {
			flags |= syscall.O_NONBLOCK
		}
		next, openErr := syscall.Openat(descriptor, part, flags, 0)
		syscall.Close(descriptor)
		if openErr != nil {
			return nil, fmt.Errorf("INPUT_FILE: no-link open required: %w", openErr)
		}
		descriptor = next
	}
	return os.NewFile(uintptr(descriptor), path), nil
}

func readCandidate(file *os.File, limit int64) ([]byte, os.FileMode, error) {
	if limit < 0 {
		return nil, 0, fmt.Errorf("INPUT_LIMIT: negative remaining budget")
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, 0, fmt.Errorf("INPUT_FILE: regular file required")
	}
	metadata, ok := info.Sys().(*syscall.Stat_t)
	if !ok || metadata.Nlink != 1 {
		return nil, 0, fmt.Errorf("INPUT_FILE: single link required")
	}
	if info.Size() < 0 || info.Size() > limit {
		return nil, 0, fmt.Errorf("INPUT_LIMIT: bounded snapshot exceeded")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, 0, err
	}
	if int64(len(data)) > limit {
		return nil, 0, fmt.Errorf("INPUT_LIMIT: candidate grew beyond budget")
	}
	after, err := file.Stat()
	if err != nil {
		return nil, 0, err
	}
	metadata, ok = after.Sys().(*syscall.Stat_t)
	if !ok || metadata.Nlink != 1 || after.Size() != int64(len(data)) || !after.ModTime().Equal(info.ModTime()) {
		return nil, 0, fmt.Errorf("INPUT_FILE: candidate changed while copying")
	}
	return data, info.Mode(), nil
}

type childOutput struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	overflow bool
	cancel   context.CancelFunc
}

func (output *childOutput) Write(data []byte) (int, error) {
	output.mu.Lock()
	defer output.mu.Unlock()
	if output.overflow {
		return 0, fmt.Errorf("CHILD_OUTPUT_LIMIT")
	}
	if len(data) > (8<<20)-output.buffer.Len() {
		output.overflow = true
		output.cancel()
		return 0, fmt.Errorf("CHILD_OUTPUT_LIMIT")
	}
	return output.buffer.Write(data)
}

func admitBundle(root string) (resources, error) {
	var r resources
	data, err := regular(filepath.Join(root, "resources.json"))
	if err != nil {
		return r, err
	}
	if len(manifestSHA) != 64 || hash(data) != manifestSHA {
		return r, fmt.Errorf("RESOURCE_MANIFEST: approved digest differs")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&r); err != nil {
		return r, fmt.Errorf("RESOURCE_MANIFEST: %w", err)
	}
	if err := checks.AdmitCapture(r.Prepared); err != nil {
		return r, err
	}
	if r.Bwrap != "/usr/bin/bwrap" {
		return r, fmt.Errorf("RESOURCE_HELPER: fixed launcher required")
	}
	helper, err := regular(r.Bwrap)
	if err != nil || hash(helper) != r.BwrapSHA {
		return r, fmt.Errorf("RESOURCE_HELPER: launcher differs")
	}
	actual := map[string]identity{}
	err = filepath.WalkDir(filepath.Join(root, "resources"), func(path string, entry os.DirEntry, incoming error) error {
		if incoming != nil {
			return incoming
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		value := identity{Mode: uint32(info.Mode().Perm())}
		switch {
		case entry.IsDir():
			value.Kind = "directory"
		case info.Mode().IsRegular():
			bytes, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			value.Kind = "file"
			value.Value = hash(bytes)
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			// Prepared rootfs links are allowed only inside its root; candidate
			// and helper links are never admitted.
			imageRoot := filepath.Join(root, "resources/rootfs")
			if !strings.HasPrefix(path, imageRoot+"/") {
				return fmt.Errorf("RESOURCE_LINK: %s", relative)
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(path), target))
			if filepath.IsAbs(target) {
				resolved = filepath.Join(imageRoot, target)
			}
			if resolved != imageRoot && !strings.HasPrefix(resolved, imageRoot+"/") {
				return fmt.Errorf("RESOURCE_LINK: escaping link")
			}
			value.Kind = "symlink"
			value.Value = target
		default:
			return fmt.Errorf("RESOURCE_TYPE: %s", relative)
		}
		actual[relative] = value
		if expected, ok := r.Files[relative]; !ok || expected != value {
			return fmt.Errorf("RESOURCE_IDENTITY: %s", relative)
		}
		return nil
	})
	if err != nil {
		return r, err
	}
	if len(actual) != len(r.Files) {
		return r, fmt.Errorf("RESOURCE_IDENTITY: missing prepared resource")
	}
	return r, nil
}

// Snapshot an explicit input allowlist, never the repository root. Every copied
// file is regular/single-link and bounded; secrets/cache/config are excluded.
func snapshot(candidate, destination string) error {
	if err := realDirectory(candidate); err != nil {
		return err
	}
	allowed := []string{"Taskfile.yml", "mise.toml", "tools", "harness", "modules", "components", "stages", "profiles", "schemas", "policies", "catalog", "examples", "probe.sh", "included.yml", "bin", "lz-offline"}
	var total int64
	count, entries := 0, 0
	var copyEntry func(*os.File, string, int) error
	copyEntry = func(file *os.File, target string, depth int) error {
		entries++
		if entries > 10000 || depth > 64 {
			return fmt.Errorf("INPUT_LIMIT: entry/depth budget exceeded")
		}
		info, err := file.Stat()
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := os.MkdirAll(target, 0700); err != nil {
				return err
			}
			for {
				children, readErr := file.ReadDir(128)
				for _, child := range children {
					base := child.Name()
					if base == ".git" || base == ".local" || base == ".terraform" || base == ".cache" || base == ".env" || strings.HasPrefix(base, ".env.") || base == ".envrc" {
						continue
					}
					descriptor, err := syscall.Openat(int(file.Fd()), base, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
					if err != nil {
						return fmt.Errorf("INPUT_FILE: no-link child open required: %w", err)
					}
					opened := os.NewFile(uintptr(descriptor), base)
					copyErr := copyEntry(opened, filepath.Join(target, base), depth+1)
					closeErr := opened.Close()
					if copyErr != nil {
						return copyErr
					}
					if closeErr != nil {
						return closeErr
					}
				}
				if readErr == io.EOF {
					return nil
				}
				if readErr != nil {
					return readErr
				}
			}
		}
		if count >= 10000 {
			return fmt.Errorf("INPUT_LIMIT: file count exceeded")
		}
		limit := int64(16 << 20)
		if remaining := int64(128<<20) - total; remaining < limit {
			limit = remaining
		}
		data, sourceMode, err := readCandidate(file, limit)
		if err != nil {
			return err
		}
		count++
		total += int64(len(data))
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		mode := os.FileMode(0600)
		if sourceMode&0111 != 0 {
			mode = 0700
		}
		return os.WriteFile(target, data, mode)
	}
	for _, name := range allowed {
		path := filepath.Join(candidate, name)
		file, err := openCandidate(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		copyErr := copyEntry(file, filepath.Join(destination, name), 0)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if _, err := os.Stat(filepath.Join(destination, "Taskfile.yml")); err != nil {
		return fmt.Errorf("INPUT_TASK: Taskfile required")
	}
	return nil
}

func boundary() error {
	if _, err := checks.RuntimePrepared(); err != nil {
		return err
	}
	address := "192.0.2.1:9"
	_, err := net.Dial("tcp4", address)
	if !errors.Is(err, syscall.ENETUNREACH) {
		return fmt.Errorf("BOUNDARY_NETWORK: strict kernel ENETUNREACH required: %v", err)
	}
	child := exec.Command("/tcb/lz-offline", "--network-probe")
	child.Env = []string{"PATH=/tcb:/usr/bin:/bin", "HOME=/home/offline"}
	if output, err := child.CombinedOutput(); err != nil {
		return fmt.Errorf("BOUNDARY_SUBPROCESS: %w: %s", err, output)
	}
	return nil
}

func run() (result error) {
	if len(os.Args) == 2 {
		switch os.Args[1] {
		case "--verify":
			return checks.VerifyRuntime()
		case "--boundary":
			return boundary()
		case "--network-probe":
			if _, err := checks.RuntimePrepared(); err != nil {
				return err
			}
			_, err := net.Dial("tcp4", "192.0.2.1:9")
			if !errors.Is(err, syscall.ENETUNREACH) {
				return fmt.Errorf("BOUNDARY_NETWORK: %v", err)
			}
			return nil
		}
	}
	if len(os.Args) < 6 || os.Args[1] != "--candidate" || os.Args[3] != "--" || os.Args[4] != "task" {
		return fmt.Errorf("USAGE: lz-offline --candidate <absolute-checkout> -- task <target>")
	}
	if len(os.Args) != 6 || !map[string]bool{"verify:toolchain": true, "test:offline-boundary": true, "probe": true}[os.Args[5]] {
		return fmt.Errorf("COMMAND_ADMISSION: explicit child target required")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if _, err := regular(executable); err != nil {
		return err
	}
	root := filepath.Dir(executable)
	if err := realDirectory(root); err != nil {
		return err
	}
	candidate := os.Args[2]
	if root == candidate || strings.HasPrefix(root, candidate+"/") {
		return fmt.Errorf("ENTRY_LOCATION: external install required")
	}
	r, err := admitBundle(root)
	if err != nil {
		return err
	}
	// All candidate interpretation follows admission and occurs inside a copied
	// private snapshot; only Go filesystem reads occur here on the host.
	scratch, err := os.MkdirTemp(filepath.Dir(root), "offline-")
	if err != nil {
		return err
	}
	cleaned := false
	defer func() {
		if !cleaned {
			if err := os.RemoveAll(scratch); err != nil {
				result = fmt.Errorf("CLEANUP_FAILED: %w", err)
			}
		}
	}()
	copyRoot := filepath.Join(scratch, "candidate")
	if err := os.Mkdir(copyRoot, 0700); err != nil {
		return err
	}
	if err := snapshot(candidate, copyRoot); err != nil {
		return err
	}
	namespace, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		return err
	}
	gate, err := json.Marshal(checks.RuntimeGate{HostNetwork: namespace, Prepared: r.Prepared})
	if err != nil {
		return err
	}
	gatePath := filepath.Join(scratch, "admission.json")
	if err := os.WriteFile(gatePath, gate, 0600); err != nil {
		return err
	}
	res := filepath.Join(root, "resources")
	args := []string{"--die-with-parent", "--new-session", "--unshare-user", "--unshare-pid", "--unshare-ipc", "--unshare-uts", "--unshare-net", "--cap-drop", "ALL",
		"--ro-bind", filepath.Join(res, "rootfs"), "/", "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp", "--tmpfs", "/run", "--tmpfs", "/home",
		"--dir", "/home/offline", "--dir", "/run/lz", "--tmpfs", "/tcb", "--tmpfs", "/tools",
		"--ro-bind", filepath.Join(res, "go"), "/tools/go", "--ro-bind", filepath.Join(res, "task"), "/tcb/task",
		"--ro-bind", filepath.Join(res, "tofu"), "/tcb/tofu", "--ro-bind", filepath.Join(res, "terramate"), "/tcb/terramate",
		"--ro-bind", executable, "/tcb/lz-offline", "--ro-bind", filepath.Join(res, "mirror"), "/mirror",
		"--ro-bind", filepath.Join(res, "tofurc"), "/run/lz/tofurc", "--ro-bind", gatePath, "/run/lz/admission.json",
		"--bind", copyRoot, "/candidate", "--clearenv", "--setenv", "PATH", "/tcb:/tools/go/bin:/usr/bin:/bin", "--setenv", "HOME", "/home/offline",
		"--setenv", "TF_CLI_CONFIG_FILE", "/run/lz/tofurc", "--setenv", "GOTOOLCHAIN", "local", "--setenv", "GOENV", "off", "--setenv", "GOWORK", "off",
		"--setenv", "GOPROXY", "off", "--setenv", "GOSUMDB", "off", "--setenv", "GOCACHE", "/tmp/go-cache", "--setenv", "GOPATH", "/tmp/go-path", "--setenv", "CGO_ENABLED", "0"}
	// The bounded independent proof driver supplies only exact synthetic metadata.
	// These values cannot select mounts, helpers, config or launch arguments.
	if os.Args[5] == "probe" {
		for _, name := range []string{"LZ_HOST", "LZ_URL"} {
			value := os.Getenv(name)
			if value != "" {
				args = append(args, "--setenv", name, value)
			}
		}
		probe := filepath.Join(root, "probe")
		data, err := regular(probe)
		if err != nil {
			return err
		}
		approved, err := regular(filepath.Join(root, "probe.sha256"))
		if err != nil || hash(data) != strings.TrimSpace(string(approved)) {
			return fmt.Errorf("PROBE_IDENTITY")
		}
		args = append(args, "--ro-bind", probe, "/tcb/probe", "--setenv", "LZ_PROBE", "1", "--setenv", "LZ_PROBE_BINARY", "/tcb/probe")
	}
	// Verify the actual namespace/mount gate before loading the candidate Taskfile.
	// Tool mount points live on private tmpfs; seal them before the child starts.
	args = append(args, "--remount-ro", "/tcb", "--remount-ro", "/tools", "--chdir", "/candidate", "/tcb/lz-offline", "--inside", os.Args[5])
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	output := &childOutput{cancel: cancel}
	command := exec.CommandContext(ctx, r.Bwrap, args...)
	command.Env = []string{}
	command.Dir = "/"
	command.WaitDelay = 5 * time.Second
	command.Stdout, command.Stderr = output, output
	childErr := command.Run()
	if err := os.RemoveAll(scratch); err != nil {
		return fmt.Errorf("CLEANUP_FAILED: %w", err)
	}
	cleaned = true
	if output.overflow {
		return fmt.Errorf("CHILD_OUTPUT_LIMIT")
	}
	if _, err := os.Stdout.Write(output.buffer.Bytes()); err != nil {
		return err
	}
	if childErr != nil {
		return fmt.Errorf("CHILD_FAILED: %w", childErr)
	}
	switch os.Args[5] {
	case "verify:toolchain":
		_, err = fmt.Fprintln(os.Stdout, "TOOLCHAIN_QUALIFIED go=1.27.1 tofu=1.13.0 terramate=0.17.3 network=none")
	case "test:offline-boundary":
		_, err = fmt.Fprintln(os.Stdout, "BOUNDARY_QUALIFIED process=kernel:ENETUNREACH subprocess=kernel:ENETUNREACH")
	}
	if err != nil {
		return err
	}
	return nil
}

func main() {
	if len(os.Args) == 3 && os.Args[1] == "--inside" {
		if err := checks.VerifyRuntime(); err != nil {
			fail(err)
		}
		// Absolute trusted Task ignores candidate bin/launcher replacements.
		args := []string{"--taskfile", "/candidate/Taskfile.yml", os.Args[2]}
		command := exec.Command("/tcb/task", args...)
		command.Env = os.Environ()
		command.Dir = "/candidate"
		command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := command.Run(); err != nil {
			fail(err)
		}
		return
	}
	if err := run(); err != nil {
		fail(err)
	}
}
