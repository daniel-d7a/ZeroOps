package build

import (
	"archive/zip"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type zentry struct {
	name string
	body string
	mode fs.FileMode
}

// makeZip writes a zip on disk from entry specs and returns its path.
func makeZip(t *testing.T, entries []zentry) string {
	t.Helper()
	f, err := os.Create(filepath.Join(t.TempDir(), "src.zip"))
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		h.SetMode(mode)
		fw, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return f.Name()
}

func wantPermanent(t *testing.T, src, dest, substr string) {
	t.Helper()
	_, err := Extract(src, dest)
	if err == nil {
		t.Fatalf("expected permanent error containing %q, got nil", substr)
	}
	if !isPermanent(err) {
		t.Fatalf("expected permanent error, got transient: %v", err)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("error %q does not contain %q", err.Error(), substr)
	}
}

func TestExtractHappyPath(t *testing.T) {
	src := makeZip(t, []zentry{
		{name: "package.json", body: `{"name":"app"}`},
		{name: "server.js", body: "console.log(1)"},
	})
	dest := filepath.Join(t.TempDir(), "work")

	root, err := Extract(src, dest)
	if err != nil {
		t.Fatal(err)
	}
	if root != dest {
		t.Fatalf("root = %q, want %q", root, dest)
	}
	got, err := os.ReadFile(filepath.Join(dest, "server.js"))
	if err != nil || string(got) != "console.log(1)" {
		t.Fatalf("server.js = %q, %v", got, err)
	}
}

func TestExtractCollapsesSingleTopLevelDir(t *testing.T) {
	src := makeZip(t, []zentry{
		{name: "repo/package.json", body: `{"name":"app"}`},
		{name: "repo/index.js", body: "x"},
	})
	dest := filepath.Join(t.TempDir(), "work")

	root, err := Extract(src, dest)
	if err != nil {
		t.Fatal(err)
	}
	if root != filepath.Join(dest, "repo") {
		t.Fatalf("root = %q, want the repo/ subdir", root)
	}
}

// Zip-slip: no entry may write outside destDir (plan §8.1). Preflight rejects
// before any byte lands, and the escaped file must not exist afterwards.
func TestExtractRejectsTraversal(t *testing.T) {
	for _, name := range []string{"../evil.txt", "a/../../evil.txt", "/etc/evil"} {
		t.Run(name, func(t *testing.T) {
			src := makeZip(t, []zentry{{name: name, body: "pwned"}})
			dest := filepath.Join(t.TempDir(), "work")
			wantPermanent(t, src, dest, "escapes the destination")
			if _, err := os.Stat(filepath.Join(filepath.Dir(dest), "evil.txt")); err == nil {
				t.Fatal("file escaped the destination")
			}
		})
	}
}

func TestExtractRejectsSymlinkEntries(t *testing.T) {
	src := makeZip(t, []zentry{
		{name: "package.json", body: "{}"},
		{name: "link", body: "/etc/passwd", mode: fs.ModeSymlink | 0o777},
	})
	wantPermanent(t, src, filepath.Join(t.TempDir(), "work"), "is a symlink")
}

func TestExtractRejectsNonRegularEntries(t *testing.T) {
	src := makeZip(t, []zentry{
		{name: "fif.o", body: "", mode: fs.ModeNamedPipe | 0o644},
	})
	wantPermanent(t, src, filepath.Join(t.TempDir(), "work"), "not a regular file")
}

func TestExtractRequiresPackageJSONAtRoot(t *testing.T) {
	src := makeZip(t, []zentry{{name: "server.js", body: "x"}})
	wantPermanent(t, src, filepath.Join(t.TempDir(), "work"), "package.json not found")

	// package.json exists but is a directory → still permanent (sibling entry
	// prevents the single-top-dir collapse from masking it)
	asDir := makeZip(t, []zentry{
		{name: "package.json/", body: "", mode: fs.ModeDir | 0o755},
		{name: "index.js", body: "x"},
	})
	wantPermanent(t, asDir, filepath.Join(t.TempDir(), "work"), "not a regular file")
}

func TestExtractRejectsFileCountLimit(t *testing.T) {
	entries := []zentry{{name: "package.json", body: "{}"}}
	for i := 0; i < maxZipFiles; i++ {
		entries = append(entries, zentry{name: fmt.Sprintf("f/%d.txt", i), body: "x"})
	}
	wantPermanent(t, makeZip(t, entries), filepath.Join(t.TempDir(), "work"), "more than 5000 files")
}

func TestExtractClassifiesArchiveProblemsAsPermanent(t *testing.T) {
	// not a zip at all
	junk := filepath.Join(t.TempDir(), "junk.zip")
	if err := os.WriteFile(junk, []byte("this is not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantPermanent(t, junk, filepath.Join(t.TempDir(), "work"), "unreadable zip")

	// missing source (plan §8.1 step 2)
	wantPermanent(t, filepath.Join(t.TempDir(), "missing.zip"), filepath.Join(t.TempDir(), "work"), "source zip missing")
}

func TestIsPermanentClassification(t *testing.T) {
	if isPermanent(fmt.Errorf("disk full: %w", fmt.Errorf("write"))) {
		t.Fatal("infrastructure errors must stay transient")
	}
	if !isPermanent(&permanentErr{reason: "x"}) {
		t.Fatal("archive errors must be permanent")
	}
}
