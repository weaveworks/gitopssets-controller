package apiclient

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
)

func TestValidateEndpointAddress(t *testing.T) {
	originalLookup := lookupIPs
	t.Cleanup(func() { lookupIPs = originalLookup })
	lookupIPs = func(ctx context.Context, host string) ([]net.IP, error) {
		switch host {
		case "metadata.example":
			return []net.IP{net.ParseIP("169.254.169.254")}, nil
		case "public.example":
			return []net.IP{net.ParseIP("1.2.3.4")}, nil
		default:
			return nil, context.DeadlineExceeded
		}
	}

	cases := []struct {
		name    string
		raw     string
		allow   bool
		wantErr string
	}{
		{name: "link local", raw: "http://169.254.169.254/latest/meta-data?token=secret", wantErr: "restricted"},
		{name: "link local stays blocked when cluster network is allowed", raw: "http://169.254.169.254/latest", allow: true, wantErr: "restricted"},
		{name: "ipv6 link local", raw: "http://[fe80::1]/", wantErr: "restricted"},
		{name: "unspecified", raw: "http://0.0.0.0/", wantErr: "restricted"},
		{name: "multicast", raw: "http://224.0.0.1/", wantErr: "restricted"},
		{name: "loopback", raw: "http://127.0.0.1:8080/v1", wantErr: "loopback"},
		{name: "loopback allowed", raw: "http://127.0.0.1/v1", allow: true},
		{name: "cluster dns", raw: "https://kubernetes.default.svc/api?token=secret", wantErr: "cluster"},
		{name: "cluster local", raw: "https://example.cluster.local/api", wantErr: "cluster"},
		{name: "cluster dns allowed", raw: "https://kubernetes.default.svc/api", allow: true},
		{name: "resolved metadata address", raw: "https://metadata.example/latest?token=secret", wantErr: "restricted"},
		{name: "public address", raw: "https://public.example/items?token=secret"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			endpoint, err := url.Parse(tt.raw)
			if err != nil {
				t.Fatal(err)
			}
			err = validateEndpointAddress(t.Context(), endpoint, tt.allow)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v", err)
			}
			if strings.Contains(err.Error(), "token") || strings.Contains(err.Error(), "?") {
				t.Fatalf("error leaked the query string: %v", err)
			}
		})
	}
}

func TestGenerateRejectsMetadataAddressBeforeCallingTheClient(t *testing.T) {
	previous := enforceAddressPolicy
	enforceAddressPolicy = true
	t.Cleanup(func() { enforceAddressPolicy = previous })

	gen := NewGenerator(logr.Discard(), nil, func(*tls.Config) *http.Client {
		t.Fatal("blocked address was requested")
		return nil
	})
	_, err := gen.Generate(t.Context(), &templatesv1.GitOpsSetGenerator{
		APIClient: &templatesv1.APIClientGenerator{Endpoint: "http://169.254.169.254/latest/meta-data?token=secret"},
	}, &templatesv1.GitOpsSet{ObjectMeta: metav1.ObjectMeta{Namespace: "demo"}})
	if err == nil || !strings.Contains(err.Error(), "restricted") || strings.Contains(err.Error(), "token") {
		t.Fatalf("err = %v", err)
	}
}
