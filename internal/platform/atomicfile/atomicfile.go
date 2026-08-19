// Package atomicfile replaces a file's contents without ever leaving a
// half-written one behind: the data goes to a temp file beside the target, is
// flushed to disk, and is only then renamed over it.
//
// Every file this program owns is written whole and read whole by something
// else at an arbitrary moment — the guard state and the settings are both read
// by the notify loop every minute, and the generated station catalog is read
// by the compiler — so a torn file is never merely cosmetic. It is a day of
// wrong or missing briefs, or a build that no longer compiles.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// Write creates path's parent directory if needed, then replaces path's
// contents with data. On any failure path keeps its previous contents and no
// temp file is left behind.
func Write(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}

	// The temp file is created in the target's own directory, not the system
	// temp dir, because a rename is only atomic within a single filesystem.
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	// Removed unconditionally: after a successful rename there is nothing
	// left at tmpName and the error is meaningless, and after a failure this
	// is the only thing that stops a stray temp file accumulating.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	// Flushed to disk before the rename that makes it visible — otherwise a
	// crash between the rename and the OS's own lazy flush can leave the
	// now-current file empty or truncated, which is exactly the torn write
	// this dance exists to prevent in the first place.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
