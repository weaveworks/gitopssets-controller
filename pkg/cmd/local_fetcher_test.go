package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
)

func TestLocalFetcherCopiesTree(t *testing.T) {
	src := t.TempDir()
	if err := os.Mkdir(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "values.yaml"), []byte("name: web\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("values.yaml", filepath.Join(src, "sub", "link.yaml")); err != nil {
		t.Fatal(err)
	}

	dst := t.TempDir()
	fetcher := localFetcher{logger: logr.Discard()}
	if err := fetcher.FetchWithContext(t.Context(), "file://"+src, "", dst); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dst, "sub", "values.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "name: web\n" {
		t.Fatalf("file = %q", got)
	}
	link, err := os.Readlink(filepath.Join(dst, "sub", "link.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if link != "values.yaml" {
		t.Fatalf("symlink = %q", link)
	}
}

func TestLocalFetcherRejectsInvalidURL(t *testing.T) {
	fetcher := localFetcher{logger: logr.Discard()}
	err := fetcher.FetchWithContext(t.Context(), "://", "", t.TempDir())
	if err == nil {
		t.Fatal("expected invalid URL error")
	}
}

func TestCopyFileMissingSource(t *testing.T) {
	err := copyFile(filepath.Join(t.TempDir(), "out"), filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("expected missing source error")
	}
}
