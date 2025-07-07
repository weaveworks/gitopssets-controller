package cmd

import (
	"context"
	"path/filepath"
	"testing"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/gitops-tools/gitopssets-controller/test"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ client.Reader = (*localObjectReader)(nil)

func TestLocalObjectReader_Get_v1GitRepository(t *testing.T) {
	v := localObjectReader{logger: logr.Discard(), repositoryRoot: "testdata"}

	gr := sourcev1.GitRepository{}
	test.AssertNoError(t, v.Get(context.TODO(), client.ObjectKey{Name: "testing", Namespace: "testing"}, &gr))

	rootURL, err := filepath.Abs("testdata")
	test.AssertNoError(t, err)
	wantURL := "file://" + rootURL + "/testing"
	if gr.Status.Artifact.URL != wantURL {
		t.Fatalf("got Artifact URL %q, want %q", wantURL, gr.Status.Artifact.URL)
	}
}

func TestLocalObjectReader_Get_v1beta2GitRepository(t *testing.T) {
	v := localObjectReader{logger: logr.Discard(), repositoryRoot: "testdata"}

	gr := v1beta2.GitRepository{}
	test.AssertNoError(t, v.Get(context.TODO(), client.ObjectKey{Name: "demo-gr", Namespace: "testing"}, &gr))

	rootURL, err := filepath.Abs("testdata")
	test.AssertNoError(t, err)
	wantURL := "file://" + rootURL + "/demo-gr"
	if gr.Status.Artifact.URL != wantURL {
		t.Fatalf("got Artifact URL %q, want %q", wantURL, gr.Status.Artifact.URL)
	}
}

func TestLocalObjectReader_Get_v1beta2OCIRepository(t *testing.T) {
	v := localObjectReader{logger: logr.Discard(), repositoryRoot: "testdata"}

	gr := v1beta2.OCIRepository{}
	test.AssertNoError(t, v.Get(context.TODO(), client.ObjectKey{Name: "demo-or", Namespace: "testing"}, &gr))

	rootURL, err := filepath.Abs("testdata")
	test.AssertNoError(t, err)
	wantURL := "file://" + rootURL + "/demo-or"
	if gr.Status.Artifact.URL != wantURL {
		t.Fatalf("got Artifact URL %q, want %q", wantURL, gr.Status.Artifact.URL)
	}
}
