package test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// StartFakeArchiveServer starts an http server that serves the provided
// directory.
func StartFakeArchiveServer(t *testing.T, dir string) *httptest.Server {
	ts := httptest.NewServer(http.FileServer(http.Dir(dir)))
	t.Cleanup(ts.Close)

	return ts
}
