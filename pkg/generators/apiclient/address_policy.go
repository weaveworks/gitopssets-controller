package apiclient

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// enforceAddressPolicy is disabled by tests that talk to a local httptest
// server. Production calls keep the default.
var enforceAddressPolicy = true

var lookupIPs = func(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

func validateEndpointAddress(ctx context.Context, endpoint *url.URL, allowClusterNetwork bool) error {
	host := endpoint.Hostname()
	if host == "" {
		return fmt.Errorf("refusing endpoint with an empty host")
	}
	if isClusterDNS(host) {
		if !allowClusterNetwork {
			return fmt.Errorf("refusing cluster address %s", host)
		}
		return nil
	}
	if ip := net.ParseIP(host); ip != nil {
		return checkIP(ip, allowClusterNetwork, host)
	}
	ips, err := lookupIPs(ctx, host)
	if err != nil {
		return fmt.Errorf("failed to resolve %s: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("failed to resolve %s", host)
	}
	for _, ip := range ips {
		if err := checkIP(ip, allowClusterNetwork, host); err != nil {
			return err
		}
	}
	return nil
}

func isClusterDNS(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	return host == "kubernetes" || strings.HasSuffix(host, ".svc") || strings.HasSuffix(host, ".cluster.local")
}

func checkIP(ip net.IP, allowClusterNetwork bool, host string) error {
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("refusing restricted address %s", host)
	}
	if ip.IsLoopback() && !allowClusterNetwork {
		return fmt.Errorf("refusing loopback address %s", host)
	}
	return nil
}

func applyAddressPolicy(httpClient *http.Client, allowClusterNetwork bool) {
	if httpClient == nil {
		return
	}
	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok || transport == nil {
		return
	}
	cloned := transport.Clone()
	baseDial := cloned.DialContext
	if baseDial == nil {
		dialer := &net.Dialer{Timeout: 30 * time.Second}
		baseDial = dialer.DialContext
	}
	cloned.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := baseDial(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		tcpAddr, ok := conn.RemoteAddr().(*net.TCPAddr)
		if !ok {
			return conn, nil
		}
		if err := checkIP(tcpAddr.IP, allowClusterNetwork, tcpAddr.IP.String()); err != nil {
			conn.Close()
			return nil, err
		}
		return conn, nil
	}
	httpClient.Transport = cloned
}
