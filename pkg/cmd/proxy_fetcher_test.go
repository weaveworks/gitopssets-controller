package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
)

func TestProxyArchiveFetcherExtractsArchive(t *testing.T) {
	var body bytes.Buffer
	gzipWriter := gzip.NewWriter(&body)
	writer := tar.NewWriter(gzipWriter)
	content := []byte("name: web\n")
	if err := writer.WriteHeader(&tar.Header{Name: "values.yaml", Mode: 0o644, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}

	fetcher := NewProxyArchiveFetcher(proxyServices{body: body.Bytes()})
	dir := t.TempDir()
	err := fetcher.FetchWithContext(t.Context(), "http://source-controller.flux-system.svc.cluster.local./gitrepository/default/repo/abc.tar.gz", "sha256:abc", dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "values.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "name: web\n" {
		t.Fatalf("extracted %q", got)
	}
}

func TestProxyArchiveFetcherErrors(t *testing.T) {
	fetcher := NewProxyArchiveFetcher(proxyServices{})
	if err := fetcher.FetchWithContext(t.Context(), "http://short/archive.tar.gz", "", t.TempDir()); err == nil {
		t.Fatal("expected invalid URL error")
	}
	fetcher = NewProxyArchiveFetcher(proxyServices{err: io.ErrUnexpectedEOF})
	err := fetcher.FetchWithContext(t.Context(), "http://source-controller.flux-system.svc.cluster.local./gitrepository/default/repo/abc.tar.gz", "", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "unexpected EOF") {
		t.Fatalf("error = %v", err)
	}
}

type proxyServices struct {
	body []byte
	err  error
}

func (p proxyServices) Services(string) corev1.ServiceInterface {
	return proxyService{body: p.body, err: p.err}
}

type proxyService struct {
	corev1.ServiceInterface
	body []byte
	err  error
}

func (p proxyService) ProxyGet(string, string, string, string, map[string]string) rest.ResponseWrapper {
	return proxyResponse{body: p.body, err: p.err}
}

type proxyResponse struct {
	body []byte
	err  error
}

func (p proxyResponse) DoRaw(context.Context) ([]byte, error) {
	return p.body, p.err
}

func (p proxyResponse) Stream(context.Context) (io.ReadCloser, error) {
	if p.err != nil {
		return nil, p.err
	}
	return io.NopCloser(bytes.NewReader(p.body)), nil
}
