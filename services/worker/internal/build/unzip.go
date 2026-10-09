// Safe unzip for student sources (plan §8.1 step 3).
//
// The zip is attacker-controlled input: any file a student uploads must never
// make the worker write outside the destination directory, follow a planted
// symlink, or fill the disk. Every rule is therefore checked against the zip's
// central directory BEFORE a single byte is written, then files are streamed
// under destDir with a hard byte budget.
//
// Error classes (plan §6): anything about the ARCHIVE itself (unsafe paths,
// symlinks, too big, corrupt, no package.json) is a permanent error — the
// source will never build, retrying is wasted work. Pure I/O problems
// (disk full, EIO) return plain errors so the caller can retry.
package build

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Plan §8.1 limits for one source archive.
const (
	maxZipFiles     = 5000      // "max 5000 files"
	maxUncompressed = 200 << 20 // "max 200 MB uncompressed" (zip-bomb guard)
)

// permanentErr is the plan §6 "permanent" failure class: no retry can ever
// succeed, so the caller MarkFailed's immediately (the DB row is the record).
type permanentErr struct{ reason string }

func (e *permanentErr) Error() string { return e.reason }

func permanentf(format string, args ...any) error {
	return &permanentErr{reason: fmt.Sprintf(format, args...)}
}

// isPermanent reports whether an error came from the source archive itself
// rather than from infrastructure (the §6 retry decision).
func isPermanent(err error) bool {
	var p *permanentErr
	return errors.As(err, &p)
}

// Extract unpacks srcZip into destDir (deleted first, so a retry after a
// crash starts clean) and returns the project root: destDir itself, unless the
// zip wraps everything in a single top-level directory ("repo/" style — very
// common when zipping a folder), in which case that directory is the root.
func Extract(srcZip, destDir string) (string, error) {
	archive, err := zip.OpenReader(srcZip)
	if err != nil {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			// plan §8.1 step 2: missing source is permanent.
			return "", permanentf("source zip missing: %v", err)
		case errors.Is(err, zip.ErrFormat), errors.Is(err, zip.ErrAlgorithm):
			// Not a zip / unsupported compression: the user's file, not our disk.
			return "", permanentf("unreadable zip: %v", err)
		}
		return "", fmt.Errorf("open zip: %w", err) // transient: try again
	}
	defer archive.Close()

	// --- preflight: reject the whole archive before writing anything -------
	// Walking the central directory first means a hostile entry can never
	// race ahead of its own rejection — there is no partial write to clean up.
	fileCount := 0
	for _, entry := range archive.File {
		// Zip names always use "/", even on other platforms.
		name := strings.TrimSuffix(entry.Name, "/")
		// filepath.IsLocal = "relative and cannot escape": rejects absolute
		// paths ("/etc/passwd"), traversal ("../x", "a/../../x") and empty
		// names — one stdlib call covers the zip-slip class (plan §8.1).
		// (Worker containers are Linux, where slash-form zip names and the
		// OS path syntax are the same thing.)
		if !filepath.IsLocal(name) {
			return "", permanentf("zip entry escapes the destination: %q", entry.Name)
		}
		mode := entry.Mode()
		// A symlink entry is harmless inside the zip but dangerous on disk:
		// a later entry could then write THROUGH it (e.g. link to /etc, then
		// file "link/passwd"). Also reject devices/fifos — students only
		// need regular files and directories.
		if mode&fs.ModeSymlink != 0 {
			return "", permanentf("zip entry is a symlink: %q", entry.Name)
		}
		if mode.IsDir() {
			continue
		}
		if !mode.IsRegular() {
			return "", permanentf("zip entry is not a regular file: %q", entry.Name)
		}
		fileCount++
		if fileCount > maxZipFiles {
			return "", permanentf("zip has more than %d files", maxZipFiles)
		}
	}

	// --- extract -----------------------------------------------------------
	if err := os.RemoveAll(destDir); err != nil {
		return "", fmt.Errorf("clear work dir: %w", err)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("create work dir: %w", err)
	}

	budget := int64(maxUncompressed)
	for _, entry := range archive.File {
		name := strings.TrimSuffix(entry.Name, "/")
		target := filepath.Join(destDir, filepath.FromSlash(name))

		if entry.Mode().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", fmt.Errorf("mkdir %s: %w", name, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", fmt.Errorf("mkdir for %s: %w", name, err)
		}

		// Streaming with the remaining budget enforces the 200 MB cap against
		// ACTUAL bytes: entry headers lie (a "10 byte" entry can decompress to
		// gigabytes), so only what we really read counts.
		written, err := writeEntry(entry, target, budget)
		if err != nil {
			return "", err // already classified (permanent) or transient
		}
		budget -= written
		if budget < 0 {
			return "", permanentf("zip exceeds %d MB uncompressed", maxUncompressed>>20)
		}
	}

	// --- collapse a single top-level directory -----------------------------
	children, err := os.ReadDir(destDir)
	if err != nil {
		return "", fmt.Errorf("read work dir: %w", err)
	}
	root := destDir
	if len(children) == 1 && children[0].IsDir() {
		root = filepath.Join(destDir, children[0].Name())
	}

	// Project type "node" requires package.json at the root (plan §8.1
	// step 3) — checked here so every caller gets the same permanent failure.
	info, err := os.Stat(filepath.Join(root, "package.json"))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", permanentf("package.json not found at the project root")
	case err != nil:
		return "", fmt.Errorf("stat package.json: %w", err)
	case !info.Mode().IsRegular():
		return "", permanentf("package.json at the project root is not a regular file")
	}
	return root, nil
}

// writeEntry streams one zip entry to target and returns the bytes written.
// A short read that ends in a CRC mismatch means the archive is corrupt
// (permanent); every other error is infrastructure (transient).
func writeEntry(entry *zip.File, target string, budget int64) (int64, error) {
	src, err := entry.Open()
	if err != nil {
		if errors.Is(err, zip.ErrAlgorithm) {
			return 0, permanentf("unsupported compression in %q: %v", entry.Name, err)
		}
		return 0, fmt.Errorf("open entry %s: %w", entry.Name, err)
	}
	defer src.Close()

	dst, err := os.Create(target)
	if err != nil {
		return 0, fmt.Errorf("create %s: %w", target, err)
	}
	defer dst.Close()

	// budget+1: one byte past the cap is enough to detect the overflow.
	written, err := io.Copy(dst, io.LimitReader(src, budget+1))
	if err != nil {
		if errors.Is(err, zip.ErrChecksum) {
			return 0, permanentf("corrupt zip entry %q: %v", entry.Name, err)
		}
		return 0, fmt.Errorf("extract %s: %w", entry.Name, err)
	}
	return written, nil
}
