package bundle

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/checks"
)

// PrepareOptions locates a preparation's inputs: the private preparation
// root the bundle is written under, the qualified toolchain root, the crane
// binary that exports the image, and each fetched artefact's local file by
// its name in the pin table.
type PrepareOptions struct {
	Root, Toolchain, Crane string
	Archives               map[string]string
}

// launcherFile is the launcher whose digest Prepare records under the fixed
// path Launcher. It is Launcher in production; tests point it at a fake,
// since the runtime sandbox has no launcher of its own.
var launcherFile = Launcher

// Bounds on what one archive may hold, on an input file and on the image
// export. Variables only so that tests can lower them.
var (
	maxMembers     = 50000
	maxExpanded    = int64(256 << 20)
	maxNameLength  = 4096
	maxInputBytes  = int64(256 << 20)
	maxExportBytes = int64(256 << 20)
)

const (
	maxExportStderr = 1 << 20
	exportTimeout   = 120 * time.Second
	maxTaskBytes    = 64 << 20
	maxTflintBytes  = 128 << 20
	// The OpenTofu 1.13.0 and Terramate 0.17.3 binaries are 109 and 97 MiB.
	maxTofuBytes      = 256 << 20
	maxTerramateBytes = 256 << 20
)

const (
	providerMirror = "mirror/registry.opentofu.org/ovh/ovh"
	// tofurc installs the pinned provider from the mirror only, never from
	// the network.
	tofurc = "provider_installation {\n  filesystem_mirror {\n    path = \"/mirror\"\n    include = [\"registry.opentofu.org/ovh/ovh\"]\n  }\n}\n"
	// imageProjection says in the receipt what Project made of the image.
	imageProjection = "rooted filesystem without image /dev; runtime supplies private bwrap /dev; privileged mode bits stripped; owner read added to regular files and owner read/traverse added to directories for resource admission"
)

// mountPoints are the rootfs directories the entry's sandbox mounts over.
var mountPoints = []string{"proc", "dev", "tmp", "run", "home", "tcb", "tools", "mirror", "candidate"}

// Project extracts an image archive into dest, an absolute clean directory.
// It refuses an unrooted or duplicate member name, a member written through
// or over a link, a symlink resolving outside dest (an absolute target counts
// from dest, as Admit reads it), and any member that is not a directory,
// regular file or symlink: hard links, devices and FIFOs among them. Links
// are created after every other member; directories get their final modes
// last. On a refusal dest may hold part of the archive.
func Project(archive io.Reader, dest string) error {
	return projectArchive(tar.NewReader(archive), dest, func(string) (bool, error) { return false, nil })
}

// skipMember reports whether a rooted member name is left out of a
// projection, or refuses the archive.
type skipMember func(name string) (bool, error)

type deferredLink struct{ name, target string }

type projectedDir struct {
	name string
	mode os.FileMode
}

// projection is one archive being extracted under dest.
type projection struct {
	root     *os.Root
	dest     string
	seen     map[string]bool
	links    []deferredLink
	dirs     []projectedDir
	count    int
	expanded int64
}

func projectArchive(reader *tar.Reader, dest string, skip skipMember) error {
	if !filepath.IsAbs(dest) || filepath.Clean(dest) != dest {
		return fmt.Errorf("PROJECT_DEST: absolute clean directory required")
	}
	root, err := os.OpenRoot(dest)
	if err != nil {
		return fmt.Errorf("PROJECT_DEST: %w", err)
	}
	defer root.Close()
	p := &projection{root: root, dest: dest, seen: map[string]bool{}}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("PROJECT_ARCHIVE: %w", err)
		}
		if err := p.member(header, reader, skip); err != nil {
			return err
		}
	}
	return p.finish()
}

func (p *projection) member(header *tar.Header, content io.Reader, skip skipMember) error {
	if err := p.bound(header); err != nil {
		return err
	}
	// A PAX global header is metadata about the archive, not a member (as
	// tar readers that apply it, such as Python's tarfile, treat it).
	if header.Typeflag == tar.TypeXGlobalHeader {
		return nil
	}
	name, err := p.admit(header)
	if err != nil {
		return err
	}
	if skipped, err := skip(name); err != nil || skipped {
		return err
	}
	if err := p.refuseLinks(name); err != nil {
		return err
	}
	switch header.Typeflag {
	case tar.TypeDir:
		if err := p.root.MkdirAll(name, 0o700); err != nil {
			return fmt.Errorf("PROJECT_MEMBER: %w", err)
		}
		// The image's own permission bits, never setuid, setgid or sticky.
		p.dirs = append(p.dirs, projectedDir{name, os.FileMode(header.Mode)&0o755 | 0o500})
		return nil
	case tar.TypeReg:
		return p.file(name, os.FileMode(header.Mode)&0o755|0o400, content)
	case tar.TypeSymlink:
		if !linkInside(p.dest, filepath.Join(p.dest, name), header.Linkname) {
			return fmt.Errorf("PROJECT_LINK: escaping link %q", name)
		}
		p.links = append(p.links, deferredLink{name, header.Linkname})
		return nil
	default:
		return fmt.Errorf("PROJECT_MEMBER: non-file member %q type %q", header.Name, header.Typeflag)
	}
}

// bound counts an archive entry against the archive's bounds, before it is
// skipped or projected.
func (p *projection) bound(header *tar.Header) error {
	p.count++
	p.expanded += header.Size
	if p.count > maxMembers || header.Size < 0 || p.expanded > maxExpanded || len(header.Name) > maxNameLength || len(header.Linkname) > maxNameLength {
		return fmt.Errorf("PROJECT_LIMIT: archive limits exceeded")
	}
	return nil
}

// admit returns a member's rooted name, refusing a duplicate.
func (p *projection) admit(header *tar.Header) (string, error) {
	name, err := rootedName(header.Name)
	if err != nil {
		return "", err
	}
	if p.seen[name] {
		return "", fmt.Errorf("PROJECT_MEMBER: duplicate member %q", name)
	}
	p.seen[name] = true
	return name, nil
}

// rootedName normalises a member name ("./a//b/" is "a/b") and refuses an
// absolute name, a ".." component or an empty name.
func rootedName(name string) (string, error) {
	if strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("PROJECT_MEMBER: unrooted member %q", name)
	}
	var parts []string
	for _, part := range strings.Split(name, "/") {
		switch part {
		case "", ".":
		case "..":
			return "", fmt.Errorf("PROJECT_MEMBER: unrooted member %q", name)
		default:
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("PROJECT_MEMBER: unrooted member %q", name)
	}
	return strings.Join(parts, "/"), nil
}

// refuseLinks refuses a write through or over a link: neither name nor any
// of its ancestors under dest may be one.
func (p *projection) refuseLinks(name string) error {
	for current := name; current != "."; current = path.Dir(current) {
		info, err := p.root.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("PROJECT_LINK: member %q through or over a link", name)
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("PROJECT_MEMBER: %w", err)
		}
	}
	return nil
}

// file writes a new regular file; an existing entry of any kind is refused.
func (p *projection) file(name string, mode os.FileMode, content io.Reader) error {
	if err := p.root.MkdirAll(path.Dir(name), 0o700); err != nil {
		return fmt.Errorf("PROJECT_MEMBER: %w", err)
	}
	out, err := p.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("PROJECT_MEMBER: %w", err)
	}
	_, err = io.Copy(out, content)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("PROJECT_MEMBER: %w", err)
	}
	return p.root.Chmod(name, mode)
}

// finish creates the deferred links, checking again that no earlier link of
// the archive now sits on their path, then sets each directory's mode,
// deepest first, so every child was written while its parent was writable.
func (p *projection) finish() error {
	for _, link := range p.links {
		if err := p.refuseLinks(link.name); err != nil {
			return err
		}
		if err := p.root.MkdirAll(path.Dir(link.name), 0o700); err != nil {
			return fmt.Errorf("PROJECT_MEMBER: %w", err)
		}
		if err := p.root.Symlink(link.target, link.name); err != nil {
			return fmt.Errorf("PROJECT_MEMBER: %w", err)
		}
	}
	sort.SliceStable(p.dirs, func(i, j int) bool {
		return strings.Count(p.dirs[i].name, "/") > strings.Count(p.dirs[j].name, "/")
	})
	for _, dir := range p.dirs {
		if err := p.root.Chmod(dir.name, dir.mode); err != nil {
			return fmt.Errorf("PROJECT_MEMBER: %w", err)
		}
	}
	return nil
}

// linkInside reports whether the symlink at linkPath, with target, resolves
// inside root lexically: a relative target from the link's directory, an
// absolute one from root.
func linkInside(root, linkPath, target string) bool {
	resolved := filepath.Clean(filepath.Join(filepath.Dir(linkPath), target))
	if filepath.IsAbs(target) {
		resolved = filepath.Join(root, target)
	}
	return resolved == root || strings.HasPrefix(resolved, root+"/")
}

// VerifyArchive accepts path only if it is a single-link regular file, not a
// link, in a real directory, and its SHA-256 is sha256: what was verified
// cannot be swapped behind a link before use.
func VerifyArchive(path, sha256 string) error {
	_, err := readVerified(path, sha256)
	return err
}

// readVerified returns the bytes it verified, so what is used is what was
// verified.
func readVerified(path, sha256 string) ([]byte, error) {
	data, err := readInput(path)
	if err != nil {
		return nil, err
	}
	if Hash(data) != sha256 {
		return nil, fmt.Errorf("PIN_DIGEST: %s differs from its pin", path)
	}
	return data, nil
}

// readInput reads at most maxInputBytes of a single-link regular file in a
// real directory.
func readInput(path string) ([]byte, error) {
	data, _, err := readInputMode(path)
	return data, err
}

// readInputMode is readInput returning the file's mode too. Every component
// is opened without following a link and the file is judged by what was
// opened, so a link swapped in after a check, for the file or for any
// directory above it, is never followed.
func readInputMode(path string) ([]byte, os.FileMode, error) {
	file, err := openNoFollow(path)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, 0, fmt.Errorf("INPUT_FILE: %w", err)
	}
	if metadata, ok := info.Sys().(*syscall.Stat_t); !ok || !info.Mode().IsRegular() || metadata.Nlink != 1 {
		return nil, 0, fmt.Errorf("INPUT_FILE: single-link regular file required: %s", path)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxInputBytes+1))
	if err != nil {
		return nil, 0, fmt.Errorf("INPUT_FILE: %w", err)
	}
	if int64(len(data)) > maxInputBytes {
		return nil, 0, fmt.Errorf("INPUT_FILE: larger than %d bytes: %s", maxInputBytes, path)
	}
	return data, info.Mode(), nil
}

// openNoFollow opens an absolute clean path for reading one component at a
// time from /, each with O_NOFOLLOW: a directory above the file that is a
// link is refused by the kernel, not followed. The file itself is opened
// non-blocking, so a special file is judged, not waited on.
func openNoFollow(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return nil, fmt.Errorf("INPUT_PATH: absolute clean path required: %s", path)
	}
	dir, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("INPUT_PATH: %w", err)
	}
	parts := strings.Split(path[1:], "/")
	for _, part := range parts[:len(parts)-1] {
		next, err := syscall.Openat(dir, part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		syscall.Close(dir)
		if err != nil {
			return nil, fmt.Errorf("INPUT_PATH: real directory required: %s: %w", path, err)
		}
		dir = next
	}
	fd, err := syscall.Openat(dir, parts[len(parts)-1], syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	syscall.Close(dir)
	if err != nil {
		return nil, fmt.Errorf("INPUT_FILE: regular non-symlink file required: %s: %w", path, err)
	}
	return os.NewFile(uintptr(fd), path), nil
}

// pin is name's digest in the pin tables: a tool's release archive in
// checks.Artifacts, any other artefact in checks.Packages.
func pin(name string) (string, bool) {
	if sum, ok := checks.Artifacts[name]; ok {
		return sum, true
	}
	row, ok := checks.Packages[name]
	return row.SHA256, ok
}

// Prepare writes the runtime bundle to o.Root/bundle from pinned inputs only,
// reading every pin from the checks tables: each archive must be one the
// tables pin, with its pinned digest, and every pinned artefact must be
// given. The toolchain's Go tree must match o.Root/proof/go-files.sha256
// exactly. The bundle is built in o.Root/staging and renamed into place, and
// a receipt is written to o.Root/prepared.json before it; bundle, staging and
// the receipt must all be absent. On a refusal staging is removed and neither
// a bundle nor a receipt is left.
func Prepare(o PrepareOptions) error {
	if err := RealDirectory(o.Root); err != nil {
		return err
	}
	if !filepath.IsAbs(o.Toolchain) || !filepath.IsAbs(o.Crane) {
		return fmt.Errorf("PREPARE_INPUT: absolute toolchain and crane paths required")
	}
	final, stage, receipt := filepath.Join(o.Root, "bundle"), filepath.Join(o.Root, "staging"), filepath.Join(o.Root, "prepared.json")
	if _, err := os.Lstat(receipt); !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("PREPARE_DESTINATION: absent private destinations required: prepared.json")
	}
	// Mkdir is exclusive, so an existing bundle or staging directory is
	// refused, never merged into. The empty bundle directory holds the name
	// until the finished bundle is renamed over it: rename replaces only an
	// empty directory, so a bundle that appears meanwhile is never replaced.
	if err := os.Mkdir(final, 0o700); err != nil {
		return fmt.Errorf("PREPARE_DESTINATION: absent private destinations required: %w", err)
	}
	if err := os.Mkdir(stage, 0o700); err != nil {
		return errors.Join(fmt.Errorf("PREPARE_DESTINATION: absent private destinations required: %w", err), os.Remove(final))
	}
	if err := build(o, stage, final, receipt); err != nil {
		return errors.Join(fmt.Errorf("PREPARE: %w", err), removeStaging(stage), os.Remove(final))
	}
	return nil
}

// build prepares the bundle in stage, writes the receipt and publishes the
// bundle; on an error stage is still there and the receipt is not.
func build(o PrepareOptions, stage, final, receipt string) error {
	data, err := prepareStage(o, stage)
	if err != nil {
		return err
	}
	if err := writeNew(receipt, data, 0o600); err != nil {
		return err
	}
	// rename(2) itself, not os.Rename, which refuses any existing directory
	// after a separate check: the kernel replaces the empty reservation and
	// refuses a directory that is no longer empty.
	if err := syscall.Rename(stage, final); err != nil {
		return errors.Join(fmt.Errorf("PREPARE_DESTINATION: publish: %w", err), os.Remove(receipt))
	}
	return nil
}

// prepareStage builds the bundle under stage and returns the receipt.
func prepareStage(o PrepareOptions, stage string) ([]byte, error) {
	if err := verifyArchives(o.Archives); err != nil {
		return nil, err
	}
	resources := filepath.Join(stage, "resources")
	if err := os.Mkdir(resources, 0o700); err != nil {
		return nil, err
	}
	if err := copyGo(o, filepath.Join(resources, "go")); err != nil {
		return nil, err
	}
	if err := extractReleaseTools(o.Archives, resources); err != nil {
		return nil, err
	}
	exportDigest, err := prepareRootfs(o, stage, filepath.Join(resources, "rootfs"))
	if err != nil {
		return nil, err
	}
	providerDigest, err := mirrorProvider(o.Archives, resources)
	if err != nil {
		return nil, err
	}
	if err := writeNew(filepath.Join(resources, "tofurc"), []byte(tofurc), 0o600); err != nil {
		return nil, err
	}
	manifestDigest, err := writeManifest(stage)
	if err != nil {
		return nil, err
	}
	receipt, err := json.MarshalIndent(map[string]any{
		"image": checks.Image, "image_export_sha256": exportDigest, "image_projection": imageProjection,
		"provider_sha256": providerDigest, "packages": checks.PackagePins(), "resources_sha256": manifestDigest,
	}, "", "  ")
	return append(receipt, '\n'), err
}

// verifyArchives requires exactly the pinned artefacts, each with its pinned
// digest.
func verifyArchives(archives map[string]string) error {
	for _, name := range slices.Sorted(maps.Keys(archives)) {
		sum, ok := pin(name)
		if !ok {
			return fmt.Errorf("PIN_UNKNOWN: %s is not in the pin table", name)
		}
		if err := VerifyArchive(archives[name], sum); err != nil {
			return err
		}
	}
	for _, name := range slices.Sorted(maps.Keys(checks.PackagePins())) {
		if _, ok := archives[name]; !ok {
			return fmt.Errorf("PIN_ARCHIVE: no archive for %s", name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(checks.Artifacts)) {
		if _, ok := archives[name]; !ok {
			return fmt.Errorf("PIN_ARCHIVE: no archive for %s", name)
		}
	}
	return nil
}

// copyGo copies the toolchain's Go tree only if it holds exactly the files
// its proof lists, each with its listed digest, and nothing else: no extra
// file, link or special file. Only listed files are copied, each read without
// following a link and checked again.
func copyGo(o PrepareOptions, dest string) error {
	source := filepath.Join(o.Toolchain, "tools/go/go")
	expected, err := goProof(filepath.Join(o.Root, "proof/go-files.sha256"), source)
	if err != nil {
		return err
	}
	if err := checkGoTree(source, expected); err != nil {
		return err
	}
	if err := os.Mkdir(dest, 0o700); err != nil {
		return err
	}
	for _, file := range slices.Sorted(maps.Keys(expected)) {
		if err := copyGoFile(file, filepath.Join(dest, strings.TrimPrefix(file, source+"/")), expected[file]); err != nil {
			return err
		}
	}
	return nil
}

// goProof reads "<sha256>  <absolute path>" lines, each path unique, clean
// and under source.
func goProof(proof, source string) (map[string]string, error) {
	data, err := readInput(proof)
	if err != nil {
		return nil, err
	}
	expected := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		sum, name, ok := strings.Cut(line, "  ")
		_, duplicate := expected[name]
		if !ok || duplicate || filepath.Clean(name) != name || !strings.HasPrefix(name, source+"/") {
			return nil, fmt.Errorf("GO_PROOF: bounded unique Go manifest path required: %q", line)
		}
		expected[name] = sum
	}
	return expected, nil
}

// checkGoTree requires every entry under source to be a real directory or a
// listed single-link regular file with its listed digest, and every listed
// file to be present.
func checkGoTree(source string, expected map[string]string) error {
	if info, err := os.Lstat(source); err != nil || !info.IsDir() {
		return fmt.Errorf("GO_PROOF: real Go directory required")
	}
	seen := 0
	err := filepath.WalkDir(source, func(file string, entry fs.DirEntry, incoming error) error {
		if incoming != nil || entry.IsDir() {
			return incoming
		}
		// A file the proof does not list is verified against "", which no
		// digest equals.
		if _, err := readVerified(file, expected[file]); err != nil {
			return fmt.Errorf("GO_PROOF: extra, linked or changed file %s", file)
		}
		seen++
		return nil
	})
	if err == nil && seen != len(expected) {
		err = fmt.Errorf("GO_PROOF: Go tree differs from its proof")
	}
	return err
}

func copyGoFile(source, dest, sum string) error {
	data, mode, err := readInputMode(source)
	if err != nil {
		return fmt.Errorf("GO_PROOF: %w", err)
	}
	if Hash(data) != sum {
		return fmt.Errorf("GO_PROOF: copy differs from its proof: %s", source)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	// The toolchain's owner bits only: nothing is shared with group or others.
	return writeNew(dest, data, mode.Perm()&0o700)
}

// releaseTools are the binaries taken from the tools' verified release
// archives, by pin name: tofu, terramate and task each as the one bounded
// member of that name in a gzipped tar, tflint as the only file of a zip.
var releaseTools = []struct {
	name  string
	zip   bool
	limit int64
}{
	{"tofu", false, maxTofuBytes},
	{"terramate", false, maxTerramateBytes},
	{"task", false, maxTaskBytes},
	{"tflint", true, maxTflintBytes},
}

// extractReleaseTools writes each release tool, owner-only, from the very
// bytes of its archive that were verified against its pin.
func extractReleaseTools(archives map[string]string, resources string) error {
	for _, tool := range releaseTools {
		archive, err := readVerified(archives[tool.name], checks.Artifacts[tool.name])
		if err != nil {
			return err
		}
		member := tarMember
		if tool.zip {
			member = zipOnlyMember
		}
		data, err := member(archive, tool.name, tool.limit)
		if err != nil {
			return err
		}
		if err := writeNew(filepath.Join(resources, tool.name), data, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// tarMember returns the one bounded regular member called name of a gzipped
// tar archive.
func tarMember(archive []byte, name string, limit int64) ([]byte, error) {
	stream, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("RELEASE_ARCHIVE: %w", err)
	}
	reader := tar.NewReader(stream)
	var found []byte
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("RELEASE_ARCHIVE: %w", err)
		}
		if header.Name != name {
			continue
		}
		if found != nil || header.Typeflag != tar.TypeReg || header.Size > limit {
			return nil, fmt.Errorf("RELEASE_ARCHIVE: one bounded regular %s member required", name)
		}
		if found, err = io.ReadAll(reader); err != nil {
			return nil, fmt.Errorf("RELEASE_ARCHIVE: %w", err)
		}
	}
	if found == nil {
		return nil, fmt.Errorf("RELEASE_ARCHIVE: no %s member", name)
	}
	return found, nil
}

// zipOnlyMember returns the content of a zip archive that holds exactly one
// bounded file called name.
func zipOnlyMember(archive []byte, name string, limit int64) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("RELEASE_ARCHIVE: %w", err)
	}
	if len(reader.File) != 1 || reader.File[0].Name != name || !reader.File[0].Mode().IsRegular() || reader.File[0].UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("RELEASE_ARCHIVE: exactly one bounded %s file required", name)
	}
	content, err := reader.File[0].Open()
	if err != nil {
		return nil, fmt.Errorf("RELEASE_ARCHIVE: %w", err)
	}
	defer content.Close()
	data, err := io.ReadAll(io.LimitReader(content, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, fmt.Errorf("RELEASE_ARCHIVE: %s unreadable or over its bound", name)
	}
	return data, nil
}

// prepareRootfs exports the pinned image, projects it without its /dev,
// unpacks the pinned Wolfi packages over it and adds the directories the
// sandbox mounts over. It returns the export's digest.
func prepareRootfs(o PrepareOptions, stage, rootfs string) (string, error) {
	transport := filepath.Join(stage, "transport")
	if err := os.Mkdir(transport, 0o700); err != nil {
		return "", err
	}
	exportDigest, err := exportImage(o.Crane, transport)
	if err != nil {
		return "", err
	}
	if err := os.Mkdir(rootfs, 0o700); err != nil {
		return "", err
	}
	if err := projectImage(filepath.Join(transport, "image.tar"), rootfs); err != nil {
		return "", err
	}
	if err := unpackPackages(o.Archives, rootfs); err != nil {
		return "", err
	}
	for _, name := range mountPoints {
		if err := mountPoint(filepath.Join(rootfs, name)); err != nil {
			return "", err
		}
	}
	return exportDigest, os.RemoveAll(transport)
}

// exportImage runs crane to export the pinned image for linux/amd64 into
// transport/image.tar, bounded in time and bytes, with an environment of its
// own. crane checks the registry content against the image's digest.
func exportImage(crane, transport string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), exportTimeout)
	defer cancel()
	out, err := os.OpenFile(filepath.Join(transport, "image.tar"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	defer out.Close()
	digest := sha256.New()
	var stderr bytes.Buffer
	command := exec.CommandContext(ctx, crane, "export", "--platform=linux/amd64", checks.Image, "-")
	command.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + transport, "DOCKER_CONFIG=" + transport, "XDG_CACHE_HOME=" + transport}
	command.Dir = transport
	command.Stdout = &boundedWriter{io.MultiWriter(out, digest), maxExportBytes, cancel}
	command.Stderr = &boundedWriter{&stderr, maxExportStderr, cancel}
	command.WaitDelay = 10 * time.Second
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("IMAGE_EXPORT: %w: %q", err, lastBytes(stderr.String(), 512))
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// boundedWriter refuses writes past its budget and then ends the command.
type boundedWriter struct {
	w      io.Writer
	left   int64
	cancel context.CancelFunc
}

func (b *boundedWriter) Write(data []byte) (int, error) {
	if int64(len(data)) > b.left {
		b.cancel()
		return 0, fmt.Errorf("IMAGE_EXPORT: byte limit exceeded")
	}
	b.left -= int64(len(data))
	return b.w.Write(data)
}

func lastBytes(text string, n int) string {
	text = strings.TrimSpace(text)
	if len(text) > n {
		return text[len(text)-n:]
	}
	return text
}

// projectImage projects the exported image, leaving out its /dev: the
// sandbox supplies a private one, and no device is made on the host.
func projectImage(archive, rootfs string) error {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	return projectArchive(tar.NewReader(file), rootfs, func(name string) (bool, error) {
		return name == "dev" || strings.HasPrefix(name, "dev/"), nil
	})
}

// unpackPackages unpacks each pinned Wolfi package's data over the rootfs,
// in name order. Concatenated gzip members read as one tar stream, and the
// data may not replace or traverse anything already there.
func unpackPackages(archives map[string]string, rootfs string) error {
	for _, name := range slices.Sorted(maps.Keys(checks.PackagePins())) {
		if checks.Packages[name].Kind != checks.KindAPK {
			continue
		}
		data, err := readVerified(archives[name], checks.Packages[name].SHA256)
		if err != nil {
			return err
		}
		stream, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("PACKAGE_ARCHIVE: %s: %w", name, err)
		}
		if err := projectArchive(tar.NewReader(stream), rootfs, packageControl); err != nil {
			return fmt.Errorf("PACKAGE_ARCHIVE: %s: %w", name, err)
		}
	}
	return nil
}

// packageControl skips an apk's control members (.PKGINFO, .melange.yaml, a
// signature). Install scripts are never run, so a package that has one, or
// any other top-level dot member, is refused rather than installed
// incompletely.
func packageControl(name string) (bool, error) {
	if strings.Contains(name, "/") || !strings.HasPrefix(name, ".") {
		return false, nil
	}
	if name == ".PKGINFO" || name == ".melange.yaml" || strings.HasPrefix(name, ".SIGN.") {
		return true, nil
	}
	return false, fmt.Errorf("PACKAGE_CONTROL: control member %q refused", name)
}

// mountPoint keeps an existing real directory or creates a private one.
func mountPoint(dir string) error {
	info, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return os.Mkdir(dir, 0o700)
	}
	if err != nil || !info.IsDir() {
		return fmt.Errorf("PREPARE_ROOTFS: real directory required: %s", filepath.Base(dir))
	}
	return nil
}

// mirrorProvider copies the one pinned provider package, verified, into the
// filesystem mirror and returns its digest.
func mirrorProvider(archives map[string]string, resources string) (string, error) {
	var providers []string
	for name, row := range checks.Packages {
		if row.Kind == checks.KindProvider {
			providers = append(providers, name)
		}
	}
	if len(providers) != 1 {
		return "", fmt.Errorf("PIN_PROVIDER: exactly one pinned provider required")
	}
	name := providers[0]
	data, err := readVerified(archives[name], checks.Packages[name].SHA256)
	if err != nil {
		return "", err
	}
	mirror := filepath.Join(resources, providerMirror)
	if err := os.MkdirAll(mirror, 0o700); err != nil {
		return "", err
	}
	return Hash(data), writeNew(filepath.Join(mirror, name), data, 0o600)
}

// writeManifest writes stage/resources.json: the pins, the identity of every
// prepared resource and the launcher's digest. It returns the manifest's
// digest, which the entry is built with.
func writeManifest(stage string) (string, error) {
	files, err := Inventory(stage)
	if err != nil {
		return "", err
	}
	launcher, err := readInput(launcherFile)
	if err != nil {
		return "", fmt.Errorf("RESOURCE_HELPER: %w", err)
	}
	manifest, err := json.MarshalIndent(Resources{Prepared: checks.Pinned(), Files: files, Bwrap: Launcher, BwrapSHA: Hash(launcher)}, "", "  ")
	if err != nil {
		return "", err
	}
	manifest = append(manifest, '\n')
	return Hash(manifest), writeNew(filepath.Join(stage, "resources.json"), manifest, 0o600)
}

// writeNew writes a file that must not exist yet, with exactly mode whatever
// the process's umask; a file it fails to complete is removed.
func writeNew(file string, data []byte, mode os.FileMode) error {
	out, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, err = out.Write(data)
	if err == nil {
		err = out.Chmod(mode)
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return errors.Join(err, os.Remove(file))
	}
	return nil
}

// openStaging opens a root on stage only if what it opened is the real
// directory stage named when checked, so a link swapped in for stage or for
// a directory above it between the check and the open is refused.
func openStaging(stage string) (*os.Root, error) {
	before, err := os.Lstat(stage)
	if err != nil || !before.IsDir() {
		return nil, fmt.Errorf("PREPARE_CLEANUP: real staging directory required")
	}
	root, err := os.OpenRoot(stage)
	if err != nil {
		return nil, fmt.Errorf("PREPARE_CLEANUP: %w", err)
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(before, opened) {
		root.Close()
		return nil, fmt.Errorf("PREPARE_CLEANUP: staging directory changed while opened")
	}
	return root, nil
}

// removeStaging removes the owned staging directory. Projected directories
// can be read-only, so owner access is restored inside it first, through a
// root opened on it, so nothing outside it is changed even if a directory is
// replaced by a link meanwhile.
func removeStaging(stage string) error {
	root, err := openStaging(stage)
	if err != nil {
		return err
	}
	defer root.Close()
	err = fs.WalkDir(root.FS(), ".", func(dir string, entry fs.DirEntry, incoming error) error {
		if incoming != nil || !entry.IsDir() {
			return incoming
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return root.Chmod(dir, info.Mode().Perm()|0o700)
	})
	if err != nil {
		return fmt.Errorf("PREPARE_CLEANUP: %w", err)
	}
	return os.RemoveAll(stage)
}
