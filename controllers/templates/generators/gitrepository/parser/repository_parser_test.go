package git

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/test"
)

func TestGenerateFromFiles(t *testing.T) {
	fetchTests := []struct {
		description string
		filename    string
		items       []templatesv1.GitRepositoryGeneratorFileItem
		want        []map[string]any
	}{
		{
			description: "simple yaml files",
			filename:    "/files.tar.gz",
			items: []templatesv1.GitRepositoryGeneratorFileItem{
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
			items: []templatesv1.GitRepositoryGeneratorFileItem{
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
			parser := NewRepositoryParser(logr.Discard())
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
	parser := NewRepositoryParser(logr.Discard())
	srv := test.StartFakeArchiveServer(t, "testdata")

	_, err := parser.GenerateFromFiles(context.TODO(), srv.URL+"/bad_files.tar.gz", strings.TrimSpace(mustReadFile(t, "testdata/bad_files.tar.gz.sum")), []templatesv1.GitRepositoryGeneratorFileItem{{Path: "files/dev.yaml"}})
	if err.Error() != `failed to parse archive file "files/dev.yaml": error converting YAML to JSON: yaml: line 4: could not find expected ':'` {
		t.Fatalf("got error %v", err)
	}
}

func TestGenerateFromFiles_missing_file(t *testing.T) {
	parser := NewRepositoryParser(logr.Discard())
	srv := test.StartFakeArchiveServer(t, "testdata")

	_, err := parser.GenerateFromFiles(context.TODO(), srv.URL+"/files.tar.gz", strings.TrimSpace(mustReadFile(t, "testdata/files.tar.gz.sum")), []templatesv1.GitRepositoryGeneratorFileItem{{Path: "files/test.yaml"}})
	if !strings.Contains(err.Error(), "failed to read from archive file \"files/test.yaml\"") {
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