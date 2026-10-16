package bundle

import (
	"errors"
	"io"
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

var errNotImplemented = errors.New("PREPARE: not implemented")

// Project extracts an image archive into dest, refusing any member, symlink
// or hard link that escapes dest. Stub until T033.
func Project(archive io.Reader, dest string) error { return errNotImplemented }

// VerifyArchive accepts path only if its SHA-256 is sha256. Stub until T033.
func VerifyArchive(path, sha256 string) error { return errNotImplemented }

// Prepare writes the runtime bundle under o.Root from pinned inputs only,
// reading every pin from the checks tables. Stub until T033.
func Prepare(o PrepareOptions) error {
	_ = launcherFile
	return errNotImplemented
}
