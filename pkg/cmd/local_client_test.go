package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/gitops-tools/gitopssets-controller/test"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ client.Reader = (*localObjectReader)(nil)

func TestLocalObjectReader_Get_v1GitRepository(t *testing.T) {
	v := localObjectReader{logger: logr.Discard(), repositoryRoot: "testdata"}

	gr := sourcev1.GitRepository{}
	test.AssertNoError(t, v.Get(t.Context(), client.ObjectKey{Name: "testing", Namespace: "testing"}, &gr))

	rootURL, err := filepath.Abs("testdata")
	test.AssertNoError(t, err)
	wantURL := "file://" + rootURL + "/testing"
	if gr.Status.Artifact.URL != wantURL {
		t.Fatalf("got Artifact URL %q, want %q", wantURL, gr.Status.Artifact.URL)
	}
}

func TestLocalObjectReader_Get_v1OCIRepository(t *testing.T) {
	v := localObjectReader{logger: logr.Discard(), repositoryRoot: "testdata"}

	repo := sourcev1.OCIRepository{}
	test.AssertNoError(t, v.Get(t.Context(), client.ObjectKey{Name: "demo-or", Namespace: "testing"}, &repo))

	rootURL, err := filepath.Abs("testdata")
	test.AssertNoError(t, err)
	wantURL := "file://" + rootURL + "/demo-or"
	if repo.Status.Artifact.URL != wantURL {
		t.Fatalf("got Artifact URL %q, want %q", repo.Status.Artifact.URL, wantURL)
	}
}

func TestLocalObjectReader_Get_unsupportedType(t *testing.T) {
	v := localObjectReader{logger: logr.Discard(), repositoryRoot: "testdata"}

	err := v.Get(t.Context(), client.ObjectKey{Name: "demo"}, &corev1.ConfigMap{})
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("error = %v, want unsupported type", err)
	}
}

func TestLocalObjectReader_ListIsNotImplemented(t *testing.T) {
	v := localObjectReader{logger: logr.Discard(), repositoryRoot: "testdata"}
	err := v.List(t.Context(), &sourcev1.GitRepositoryList{})
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("error = %v", err)
	}
}
