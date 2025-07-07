package parser

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/fluxcd/pkg/http/fetch"
	"github.com/fluxcd/pkg/tar"
	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/test"
)

func TestGenerateFromFiles(t *testing.T) {
	fetchTests := []struct {
		description string
		filename    string
		items       []templatesv1.RepositoryGeneratorFileItem
		want        []map[string]any
	}{
		{
			description: "simple yaml files",
			filename:    "/files.tar.gz",
			items: []templatesv1.RepositoryGeneratorFileItem{
				{Path: "files/dev.yaml"}, {Path: "files/production.yaml"}, {Path: "files/staging.yaml"}},
			want: []map[string]any{
				{"environment": "dev", "instances": 2.0},
				{"environment": "production", "instances": 10.0},
				{"environment": "staging", "instances": 5.0},
			},
		},
		{
			description: "simple json files",
			filename:    "/json_files.tar.gz",
			items: []templatesv1.RepositoryGeneratorFileItem{
				{Path: "files/dev.json"}, {Path: "files/production.json"}, {Path: "files/staging.json"}},
			want: []map[string]any{
				{"environment": "dev", "instances": 1.0},
				{"environment": "production", "instances": 10.0},
				{"environment": "staging", "instances": 5.0},
			},
		},
	}

	srv := test.StartFakeArchiveServer(t, "testdata")
	for _, tt := range fetchTests {
		t.Run(tt.description, func(t *testing.T) {
			parser := NewRepositoryParser(logr.Discard(), fetch.NewArchiveFetcher(2, tar.UnlimitedUntarSize, tar.UnlimitedUntarSize, ""))
			parsed, err := parser.GenerateFromFiles(context.TODO(), srv.URL+tt.filename, strings.TrimSpace(mustReadFile(t, "testdata"+tt.filename+".sum")), tt.items)
			if err != nil {
				t.Fatal(err)
			}
			sort.Slice(parsed, func(i, j int) bool { return parsed[i]["environment"].(string) < parsed[j]["environment"].(string) })
			if diff := cmp.Diff(tt.want, parsed); diff != "" {
				t.Fatalf("failed to parse artifacts:\n%s", diff)
			}
		})
	}
}

func TestGenerateFromFiles_bad_yaml(t *testing.T) {
	parser := NewRepositoryParser(logr.Discard(), fetch.NewArchiveFetcher(2, tar.UnlimitedUntarSize, tar.UnlimitedUntarSize, ""))
	srv := test.StartFakeArchiveServer(t, "testdata")

	_, err := parser.GenerateFromFiles(context.TODO(), srv.URL+"/bad_files.tar.gz", strings.TrimSpace(mustReadFile(t, "testdata/bad_files.tar.gz.sum")), []templatesv1.RepositoryGeneratorFileItem{{Path: "files/dev.yaml"}})
	if err.Error() != `failed to parse archive file "files/dev.yaml": error converting YAML to JSON: yaml: line 4: could not find expected ':'` {
		t.Fatalf("got error %v", err)
	}
}

func TestGenerateFromFiles_missing_file(t *testing.T) {
	parser := NewRepositoryParser(logr.Discard(), fetch.NewArchiveFetcher(2, tar.UnlimitedUntarSize, tar.UnlimitedUntarSize, ""))
	srv := test.StartFakeArchiveServer(t, "testdata")

	_, err := parser.GenerateFromFiles(context.TODO(), srv.URL+"/files.tar.gz", strings.TrimSpace(mustReadFile(t, "testdata/files.tar.gz.sum")), []templatesv1.RepositoryGeneratorFileItem{{Path: "files/test.yaml"}})
	if !strings.Contains(err.Error(), "failed to read from archive file \"files/test.yaml\"") {
		t.Fatalf("got error %v", err)
	}
}

func TestGenerateFromFiles_missing_url(t *testing.T) {
	parser := NewRepositoryParser(logr.Discard(), fetch.NewArchiveFetcher(2, tar.UnlimitedUntarSize, tar.UnlimitedUntarSize, ""))
	srv := test.StartFakeArchiveServer(t, "testdata")

	_, err := parser.GenerateFromFiles(context.TODO(), srv.URL+"/missing.tar.gz", strings.TrimSpace(mustReadFile(t, "testdata/files.tar.gz.sum")), []templatesv1.RepositoryGeneratorFileItem{{Path: "files/test.yaml"}})
	if !strings.Contains(err.Error(), "failed to get archive URL") {
		t.Fatalf("got error %v", err)
	}
}

func TestGenerateFromDirectories(t *testing.T) {
	fetchTests := []struct {
		description string
		filename    string
		items       []templatesv1.RepositoryGeneratorDirectoryItem
		want        []map[string]any
	}{
		{
			description: "simple path",
			filename:    "/directories.tar.gz",
			items: []templatesv1.RepositoryGeneratorDirectoryItem{
				{Path: "applications/*"}},
			want: []map[string]any{
				{"Directory": "./applications/backend", "Base": "backend"},
				{"Directory": "./applications/frontend", "Base": "frontend"},
			},
		},
		{
			// TODO: non-glob path
			description: "rooted path",
			filename:    "/directories.tar.gz",
			items: []templatesv1.RepositoryGeneratorDirectoryItem{
				{Path: "*"}},
			want: []map[string]any{
				{"Directory": "./applications", "Base": "applications"},
			},
		},
		{
			description: "non-glob path",
			filename:    "/directories.tar.gz",
			items: []templatesv1.RepositoryGeneratorDirectoryItem{
				{Path: "/applications"}},
			want: []map[string]any{
				{"Directory": "./applications", "Base": "applications"},
			},
		},
		{
			description: "exclusion",
			filename:    "/directories.tar.gz",
			items: []templatesv1.RepositoryGeneratorDirectoryItem{
				{Path: "applications/*"},
				{Path: "applications/backend", Exclude: true}},
			want: []map[string]any{
				{"Directory": "./applications/frontend", "Base": "frontend"},
			},
		},
		{
			description: "exclusion different form",
			filename:    "/directories.tar.gz",
			items: []templatesv1.RepositoryGeneratorDirectoryItem{
				{Path: "applications/*"},
				{Path: "./applications/backend", Exclude: true}},
			want: []map[string]any{
				{"Directory": "./applications/frontend", "Base": "frontend"},
			},
		},
		{
			description: "exclusion different form",
			filename:    "/directories.tar.gz",
			items: []templatesv1.RepositoryGeneratorDirectoryItem{
				{Path: "applications/*"},
				{Path: "./applications/backend/", Exclude: true}},
			want: []map[string]any{
				{"Directory": "./applications/frontend", "Base": "frontend"},
			},
		},
	}

	srv := test.StartFakeArchiveServer(t, "testdata")
	for _, tt := range fetchTests {
		t.Run(tt.description, func(t *testing.T) {
			parser := NewRepositoryParser(logr.Discard(), fetch.NewArchiveFetcher(2, tar.UnlimitedUntarSize, tar.UnlimitedUntarSize, ""))
			parsed, err := parser.GenerateFromDirectories(context.TODO(), srv.URL+tt.filename,
				strings.TrimSpace(mustReadFile(t, "testdata"+tt.filename+".sum")), tt.items)
			if err != nil {
				t.Fatal(err)
			}

			sort.Slice(parsed, func(i, j int) bool { return parsed[i]["Directory"].(string) < parsed[j]["Directory"].(string) })
			if diff := cmp.Diff(tt.want, parsed); diff != "" {
				t.Fatalf("failed to scan directory:\n%s", diff)
			}
		})
	}
}

func TestGenerateFromDirectories_missing_dir(t *testing.T) {
	parser := NewRepositoryParser(logr.Discard(), fetch.NewArchiveFetcher(2, tar.UnlimitedUntarSize, tar.UnlimitedUntarSize, ""))
	srv := test.StartFakeArchiveServer(t, "testdata")

	generated, err := parser.GenerateFromDirectories(context.TODO(), srv.URL+"/directories.tar.gz",
		strings.TrimSpace(mustReadFile(t, "testdata/directories.tar.gz.sum")), []templatesv1.RepositoryGeneratorDirectoryItem{{Path: "files"}})
	if err != nil {
		t.Fatal(err)
	}

	if len(generated) > 0 {
		t.Fatalf("missing path generated %v", generated)
	}
}

func TestGenerateFromDirectories_missing_url(t *testing.T) {
	parser := NewRepositoryParser(logr.Discard(), fetch.NewArchiveFetcher(2, tar.UnlimitedUntarSize, tar.UnlimitedUntarSize, ""))
	srv := test.StartFakeArchiveServer(t, "testdata")

	_, err := parser.GenerateFromDirectories(context.TODO(), srv.URL+"/missing.tar.gz", strings.TrimSpace(mustReadFile(t, "testdata/files.tar.gz.sum")),
		[]templatesv1.RepositoryGeneratorDirectoryItem{{Path: "files/test.yaml"}})
	if !strings.Contains(err.Error(), "failed to get archive URL") {
		t.Fatalf("got error %v", err)
	}
}

func mustReadFile(t *testing.T, filename string) string {
	t.Helper()
	b, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}
