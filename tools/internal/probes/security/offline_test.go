//go:build offlineboundary

// The boundary suite needs an installed lz-offline entry and probe binary; run
// it with -tags offlineboundary from the external driver, never by default.

package security

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

type observation struct {
	CredentialEnv     bool   `json:"credential_env"`
	CredentialFile    bool   `json:"credential_file"`
	Socket            bool   `json:"socket"`
	CacheWrite        bool   `json:"cache_write"`
	HostMarker        bool   `json:"host_marker"`
	LocalConfig       bool   `json:"local_config"`
	Network           bool   `json:"network"`
	NetworkReason     string `json:"network_reason"`
	SubprocessNetwork bool   `json:"subprocess_network"`
	SubprocessReason  string `json:"subprocess_reason"`
}

// Only the reviewed proof driver can supply the external entry and test-only TCB.
// Missing preparation is a fatal setup error, never an isolation success or RED.
func prepared(t *testing.T) (string, string) {
	t.Helper()
	entry, probe := os.Getenv("LZ_BOUNDARY_ENTRY"), os.Getenv("LZ_PROBE_BINARY")
	for _, p := range []string{entry, probe, "/usr/bin/bwrap"} {
		if !filepath.IsAbs(p) {
			t.Fatalf("HARNESS_SETUP: absolute proof path required: %q", p)
		}
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("HARNESS_SETUP: %v", err)
		}
	}
	return entry, probe
}

type fixture struct {
	candidate, host, url, probe string
	env                         []string
}

// Use only an existing UP interface's RFC1918 IPv4 address. Loopback has a
// local route even inside an empty network namespace and cannot prove ENETUNREACH.
// No discovery packet, route change, wildcard bind or external responder is used.
func fixtureIPv4(t *testing.T) net.IP {
	t.Helper()
	interfaces, err := net.Interfaces()
	if err != nil || len(interfaces) > 64 {
		t.Fatalf("HARNESS_SETUP: bounded interface metadata required: %v", err)
	}
	sort.Slice(interfaces, func(i, j int) bool { return interfaces[i].Index < interfaces[j].Index })
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			t.Fatalf("HARNESS_SETUP: interface addresses: %v", err)
		}
		if len(addresses) > 64 {
			t.Fatal("HARNESS_SETUP: bounded interface address inventory required")
		}
		sort.Slice(addresses, func(i, j int) bool { return addresses[i].String() < addresses[j].String() })
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err != nil {
				continue
			}
			if ipv4 := ip.To4(); ipv4 != nil && ipv4.IsPrivate() && !ipv4.IsLoopback() {
				return ipv4
			}
		}
	}
	t.Fatal("HARNESS_SETUP: existing UP nonloopback RFC1918 IPv4 address required")
	return nil
}

func newFixture(t *testing.T, probe, attack string) fixture {
	t.Helper()
	privateDir := func(prefix string) string {
		t.Helper()
		root, err := os.MkdirTemp("", prefix)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.RemoveAll(root); err != nil {
				t.Error(err)
			}
		})
		return root
	}
	candidate, host := privateDir("c-"), privateDir("h-")
	write := func(root, name, body string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(host, "token", "SYNTHETIC-FILE-CREDENTIAL", 0600)
	listener, err := net.Listen("unix", filepath.Join(host, "runtime.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	// Bind only the selected local address and an OS-assigned ephemeral port.
	// This constant-response server accepts no commands or request-body input.
	tcp, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: fixtureIPv4(t)})
	if err != nil {
		t.Fatalf("HARNESS_SETUP: owned responder bind: %v", err)
	}
	server := &httptest.Server{Listener: tcp, Config: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "T002-REACHABLE") }), ReadHeaderTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, IdleTimeout: time.Second}}
	server.Start()
	t.Cleanup(server.Close)
	// A fixed script, generated from a fixed absolute probe path. No arbitrary
	// candidate source is admitted; hostile files below are exact bounded fixtures.
	write(candidate, "probe.sh", "#!/bin/sh\nexec /tcb/probe -test.run '^TestOfflineBoundaryProbe$'\n", 0700)
	base := "version: '3'\ntasks:\n  probe:\n    cmds:\n      - ./probe.sh\n"
	switch attack {
	case "task-variable":
		base = "version: '3'\nvars:\n  ATTACK:\n    sh: ./probe.sh 1>&2\ntasks:\n  probe:\n    cmds:\n      - 'true # {{.ATTACK}}'\n"
	case "task-include":
		write(candidate, "included.yml", "version: '3'\ntasks:\n  attack:\n    cmds:\n      - ./probe.sh\n", 0600)
		base = "version: '3'\nincludes:\n  hostile: ./included.yml\ntasks:\n  probe:\n    cmds:\n      - task: hostile:attack\n"
	case "task-hook":
		base = "version: '3'\ntasks:\n  probe:\n    cmds:\n      - defer: ./probe.sh\n      - 'true'\n"
	case "launcher-replacement":
		if err := os.Mkdir(filepath.Join(candidate, "bin"), 0700); err != nil {
			t.Fatal(err)
		}
		write(filepath.Join(candidate, "bin"), "task", "#!/bin/sh\nexec ./probe.sh\n", 0700)
		write(candidate, "lz-offline", "#!/bin/sh\nexec ./probe.sh\n", 0700)
	case "local-config":
		write(candidate, ".env", "LZ_LOCAL_CONFIG=SYNTHETIC-LOCAL-CONFIG\n", 0600)
		base = "version: '3'\ndotenv: ['.env']\ntasks:\n  probe:\n    cmds:\n      - ./probe.sh\n"
	}
	write(candidate, "Taskfile.yml", base, 0600)
	env := []string{"PATH=" + candidate + "/bin:/tcb:/usr/bin:/bin", "HOME=" + host,
		"LZ_PROBE=1", "LZ_HOST=" + host, "LZ_URL=" + server.URL, "LZ_PROBE_BINARY=" + probe,
		"LZ_PROOF_CLOSURE=" + os.Getenv("LZ_PROOF_CLOSURE"),
		"OVH_CLIENT_SECRET=SYNTHETIC-ENV-CREDENTIAL", "AUTH_TOKEN=SYNTHETIC-FETCH-TOKEN"}
	return fixture{candidate, host, server.URL, probe, env}
}

// Invocation modes: the subject entry, the sensor's isolated calibration, and
// the sensor's unisolated positive control.
const (
	subject = iota
	isolatedControl
	hostControl
)

func invoke(t *testing.T, f fixture, entry string, mode int) []observation {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch mode {
	case hostControl:
		// Positive control: the probe runs directly on the host with isolation
		// off, proving every sensor can observe the exposure it guards against.
		cmd = exec.CommandContext(ctx, f.probe, "-test.run", "^TestOfflineBoundaryProbe$")
		cmd.Dir = f.candidate
	case isolatedControl:
		// Calibration only: this is the sensor's known isolated positive context,
		// not a substitute implementation or proof that the subject is isolated.
		cmd = exec.CommandContext(ctx, "/usr/bin/bwrap", "--die-with-parent", "--new-session", "--unshare-net", "--unshare-user", "--unshare-pid", "--unshare-ipc", "--unshare-uts",
			"--ro-bind", "/usr", "/usr", "--symlink", "usr/bin", "/bin", "--symlink", "usr/lib", "/lib", "--symlink", "usr/lib64", "/lib64",
			"--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp", "--tmpfs", "/etc", "--dir", "/tcb", "--ro-bind", f.probe, "/tcb/probe", "--bind", f.candidate, "/candidate",
			"--clearenv", "--setenv", "PATH", "/tcb:/usr/bin:/bin", "--setenv", "LZ_PROBE", "1", "--setenv", "LZ_HOST", f.host,
			"--setenv", "LZ_URL", f.url, "--setenv", "LZ_PROBE_BINARY", "/tcb/probe", "--chdir", "/candidate", "/tcb/probe", "-test.run", "^TestOfflineBoundaryProbe$")
	default:
		cmd = exec.CommandContext(ctx, entry, "--candidate", f.candidate, "--", "task", "probe")
		cmd.Dir = f.candidate
	}
	cmd.Env = f.env
	if mode == hostControl {
		// The value a loaded candidate dotenv would set, so the control also
		// proves the local-config sensor.
		cmd.Env = append(append([]string{}, f.env...), "LZ_LOCAL_CONFIG=SYNTHETIC-LOCAL-CONFIG")
	}
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil || err != nil {
		t.Fatalf("HARNESS_SETUP: completed probe required: err=%v timeout=%v output=%s", err, ctx.Err(), out)
	}
	var rows []observation
	scan := bufio.NewScanner(strings.NewReader(string(out)))
	for scan.Scan() {
		line := scan.Text()
		if i := strings.Index(line, "LZ_OBSERVATION "); i >= 0 {
			var row observation
			if err := json.Unmarshal([]byte(line[i+len("LZ_OBSERVATION "):]), &row); err != nil {
				t.Fatal(err)
			}
			rows = append(rows, row)
		}
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatalf("HARNESS_SETUP: no probe discovery: %s", out)
	}
	for _, row := range rows {
		data, _ := json.Marshal(row)
		t.Logf("OBSERVATION %s", data)
	}
	return rows
}

func TestOfflineBoundaryControls(t *testing.T) {
	entry, probe := prepared(t)
	f := newFixture(t, probe, "")
	t.Run("network-on-isolation-off", func(t *testing.T) {
		rows := invoke(t, f, entry, hostControl)
		for _, r := range rows {
			if !r.Network || !r.SubprocessNetwork || !r.CredentialEnv || !r.CredentialFile || !r.Socket || !r.CacheWrite || !r.HostMarker || !r.LocalConfig {
				t.Fatalf("HARNESS_SETUP: bounded positive/mutation control unreachable: %+v", r)
			}
		}
	})
	t.Run("network-off-isolation-control", func(t *testing.T) {
		rows := invoke(t, f, entry, isolatedControl)
		for _, r := range rows {
			if r.NetworkReason != "kernel:ENETUNREACH" || r.SubprocessReason != "kernel:ENETUNREACH" {
				t.Fatalf("HARNESS_SETUP: actual kernel denial required, not outage/DNS/timeout: %+v", r)
			}
			assertIsolated(t, r)
		}
	})
}

// The qualification targets run trusted code only. A candidate that redefines
// them must neither run its own commands nor turn them into no-ops: neither in
// its root Taskfile nor in a stacks/Taskfile.yml (stacks/ is snapshotted since
// 001/T025) that the root Taskfile includes without a namespace.
func TestTrustedTargetsIgnoreCandidate(t *testing.T) {
	entry, probe := prepared(t)
	for _, placement := range []string{"root", "stacks"} {
		for target, verdict := range map[string]string{"test:offline-boundary": "BOUNDARY_QUALIFIED", "verify:toolchain": "TOOLCHAIN_QUALIFIED"} {
			t.Run(placement+"/"+target, func(t *testing.T) {
				trustedTargetIgnoresCandidate(t, entry, probe, placement, target, verdict)
			})
		}
	}
}

func trustedTargetIgnoresCandidate(t *testing.T, entry, probe, placement, target, verdict string) {
	f := newFixture(t, probe, "")
	hostile := "version: '3'\ntasks:\n  " + target + ":\n    cmds:\n      - echo LZ_OBSERVATION candidate-definition-ran\n"
	taskfile := filepath.Join(f.candidate, "Taskfile.yml")
	if placement == "stacks" {
		if err := os.Mkdir(filepath.Join(f.candidate, "stacks"), 0700); err != nil {
			t.Fatal(err)
		}
		root := "version: '3'\nincludes:\n  stacks:\n    taskfile: ./stacks/Taskfile.yml\n    flatten: true\n"
		if err := os.WriteFile(taskfile, []byte(root), 0600); err != nil {
			t.Fatal(err)
		}
		taskfile = filepath.Join(f.candidate, "stacks", "Taskfile.yml")
		// A candidate target from the same file proves it reaches the child:
		// otherwise the trusted target's verdict below would prove nothing.
		hostile += "  stacks-control:\n    cmds:\n      - echo LZ_STACKS_CONTROL ran\n"
	}
	if err := os.WriteFile(taskfile, []byte(hostile), 0600); err != nil {
		t.Fatal(err)
	}
	if placement == "stacks" {
		if out, err := runEntry(t, f, entry, "stacks-control"); err != nil || !strings.Contains(string(out), "LZ_STACKS_CONTROL ran") {
			t.Fatalf("BEHAVIORAL_RED: stacks/Taskfile.yml did not reach the candidate child: %v %s", err, out)
		}
	}
	out, err := runEntry(t, f, entry, target)
	if err != nil {
		t.Fatalf("HARNESS_SETUP: trusted target failed: %v %s", err, out)
	}
	if strings.Contains(string(out), "LZ_OBSERVATION") || !strings.Contains(string(out), verdict) {
		t.Errorf("BEHAVIORAL_RED: candidate definition of %s ran or qualification missing: %s", target, out)
	}
	if _, err := os.Stat(filepath.Join(f.host, "marker")); err == nil {
		t.Errorf("BEHAVIORAL_RED: candidate definition of %s reached the host", target)
	}
}

func runEntry(t *testing.T, f fixture, entry, target string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, entry, "--candidate", f.candidate, "--", "task", target)
	cmd.Dir, cmd.Env = f.candidate, f.env
	return cmd.CombinedOutput()
}

func TestOfflineBoundary(t *testing.T) {
	entry, probe := prepared(t)
	for _, attack := range []string{"credentials-env", "credentials-file", "socket", "cache", "subprocess", "task-variable", "task-include", "task-hook", "launcher-replacement", "local-config"} {
		t.Run(attack, func(t *testing.T) {
			f := newFixture(t, probe, attack)
			for _, r := range invoke(t, f, entry, subject) {
				assertIsolated(t, r)
			}
		})
	}
}

func assertIsolated(t *testing.T, r observation) {
	t.Helper()
	if r.NetworkReason != "kernel:ENETUNREACH" || r.SubprocessReason != "kernel:ENETUNREACH" {
		t.Errorf("BEHAVIORAL_RED: subject kernel denial required, not outage/DNS/timeout: %+v", r)
	}
	if r.CredentialEnv || r.CredentialFile || r.Socket || r.CacheWrite || r.HostMarker || r.LocalConfig || r.Network || r.SubprocessNetwork {
		t.Errorf("BEHAVIORAL_RED: advertised entry exposed host authority before isolation: %+v", r)
	}
}

func networkProbe() (bool, string) {
	client := http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(os.Getenv("LZ_URL"))
	if err == nil {
		response.Body.Close()
		return response.StatusCode == 200, "reachable"
	}
	if errors.Is(err, syscall.ENETUNREACH) {
		return false, "kernel:ENETUNREACH"
	}
	return false, "unqualified:" + err.Error()
}

// Fixed helper code is built into a separately installed probe binary. It has
// no arbitrary command/input seam and reads only driver-owned synthetic paths.
func TestOfflineBoundaryProbe(t *testing.T) {
	if os.Getenv("LZ_PROBE") != "1" {
		return
	}
	if os.Getenv("LZ_CHILD") == "1" {
		ok, reason := networkProbe()
		data, _ := json.Marshal(observation{Network: ok, NetworkReason: reason})
		fmt.Printf("LZ_OBSERVATION %s\n", data)
		return
	}
	host := os.Getenv("LZ_HOST")
	_, fileErr := os.ReadFile(filepath.Join(host, "token"))
	sock, err := net.DialTimeout("unix", filepath.Join(host, "runtime.sock"), time.Second)
	if err == nil {
		sock.Close()
	}
	cacheErr := os.WriteFile(filepath.Join(host, "cache"), []byte("SYNTHETIC-CACHE-WRITE"), 0600)
	markerErr := os.WriteFile(filepath.Join(host, "marker"), []byte("SYNTHETIC-HOST-LAUNCH"), 0600)
	ok, reason := networkProbe()
	r := observation{CredentialEnv: os.Getenv("OVH_CLIENT_SECRET") != "" || os.Getenv("AUTH_TOKEN") != "", CredentialFile: fileErr == nil, Socket: err == nil, CacheWrite: cacheErr == nil, HostMarker: markerErr == nil, LocalConfig: os.Getenv("LZ_LOCAL_CONFIG") != "", Network: ok, NetworkReason: reason}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Getenv("LZ_PROBE_BINARY"), "-test.run", "^TestOfflineBoundaryProbe$")
	cmd.Env = append(os.Environ(), "LZ_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("HARNESS_SETUP: subprocess failed: %v %s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "LZ_OBSERVATION ") {
			var child observation
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "LZ_OBSERVATION ")), &child); err != nil {
				t.Fatal(err)
			}
			r.SubprocessNetwork = child.Network
			r.SubprocessReason = child.NetworkReason
		}
	}
	if r.SubprocessReason == "" {
		t.Fatal("HARNESS_SETUP: subprocess observation missing")
	}
	data, _ := json.Marshal(r)
	fmt.Printf("LZ_OBSERVATION %s\n", data)
}
