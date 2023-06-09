package cmd

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/fluxcd/pkg/tar"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

// NewProxyArchiveFetcher creates and returns a new ProxyArchiveFetcher ready for
// use.
func NewProxyArchiveFetcher(cl corev1.ServicesGetter) *ProxyArchiveFetcher {
	return &ProxyArchiveFetcher{
		Client:       cl,
		maxUntarSize: tar.UnlimitedUntarSize,
	}
}

// ProxyArchiveFetcher uses a Kubernetes Client to make a "proxy" request
// via a Kubernetes Service to fetch the archiveURL.
type ProxyArchiveFetcher struct {
	Client corev1.ServicesGetter

	// TODO allow configuring this.
	maxUntarSize int
}

// Fetch implements the ArchiveFetcher implementation, but uses the Kube service
// proxy mechanism to get the archive.
func (p *ProxyArchiveFetcher) Fetch(archiveURL, checksum, dir string) error {
	parsed, err := parseArtifactURL(archiveURL)
	if err != nil {
		return err
	}

	responseWrapper := p.Client.Services(parsed.namespace).ProxyGet(parsed.scheme, parsed.name, parsed.port, parsed.path, nil)
	b, err := responseWrapper.DoRaw(context.TODO())
	if err != nil {
		return err
	}

	f, err := os.CreateTemp("", "fetch.*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(f.Name())

	// TODO: limiting?
	if _, err := f.Write(b); err != nil {
		return err
	}

	// We have just filled the file, to be able to read it from
	// the start we must go back to its beginning.
	_, err = f.Seek(0, 0)
	if err != nil {
		return fmt.Errorf("failed to seek back to beginning: %w", err)
	}

	// TODO: verify the digest!

	// Extracts the tar file.
	if err = tar.Untar(f, dir, tar.WithMaxUntarSize(p.maxUntarSize)); err != nil {
		return fmt.Errorf("failed to extract archive (check whether file size exceeds max download size): %w", err)
	}

	return nil
}

func parseArtifactURL(artifactURL string) (*service, error) {
	u, err := url.Parse(artifactURL)
	if err != nil {
		return nil, err
	}

	// Split hostname to get namespace and name.
	host := strings.Split(u.Hostname(), ".")

	if len(host) != 6 || host[2] != "svc" || u.Path == "/" {
		return nil, fmt.Errorf("invalid artifact URL %s", artifactURL)
	}

	port := u.Port()
	if port == "" {
		port = "80"
	}

	return &service{
		scheme:    u.Scheme,
		namespace: host[1],
		name:      host[0],
		path:      u.Path,
		port:      port,
	}, nil
}

// service represents the elements that we need to use the client Proxy to fetch
// a URL.
type service struct {
	scheme    string
	namespace string
	name      string
	path      string
	port      string
}
