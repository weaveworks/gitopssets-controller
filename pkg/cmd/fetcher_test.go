package cmd

import (
	"testing"
)

func TestParseArtifactURL(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantName  string
		wantNS    string
		wantPort  string
		wantPath  string
		wantError bool
	}{
		{
			name:     "explicit port",
			raw:      "http://source-controller.flux-system.svc.cluster.local.:80/gitrepository/default/repo/abc.tar.gz",
			wantName: "source-controller",
			wantNS:   "flux-system",
			wantPort: "80",
			wantPath: "/gitrepository/default/repo/abc.tar.gz",
		},
		{
			name:     "default port",
			raw:      "https://source-controller.flux-system.svc.cluster.local./gitrepository/default/repo/abc.tar.gz",
			wantName: "source-controller",
			wantNS:   "flux-system",
			wantPort: "80",
			wantPath: "/gitrepository/default/repo/abc.tar.gz",
		},
		{
			name:      "root path",
			raw:       "http://source-controller.flux-system.svc.cluster.local/",
			wantError: true,
		},
		{
			name:      "short host",
			raw:       "http://source-controller/archive.tar.gz",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseArtifactURL(tt.raw)
			if tt.wantError {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.name != tt.wantName || got.namespace != tt.wantNS || got.port != tt.wantPort || got.path != tt.wantPath {
				t.Fatalf("parsed %+v", got)
			}
		})
	}
}
