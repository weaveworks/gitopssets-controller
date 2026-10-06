package templates

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
)

func TestRepeat(t *testing.T) {
	params := map[string]any{
		"name":     "web",
		"replicas": int64(2),
		"ports":    []any{"80", "443"},
		"missing":  []any(nil),
		"cluster":  struct{ Name string }{Name: "prod"},
	}

	tests := []struct {
		name    string
		repeat  string
		want    []any
		wantErr bool
	}{
		{
			name:   "string scalar",
			repeat: "{ $.name }",
			want:   []any{"web"},
		},
		{
			name:   "number scalar",
			repeat: "{ $.replicas }",
			want:   []any{int64(2)},
		},
		{
			name:   "struct field scalar",
			repeat: "{ $.cluster.Name }",
			want:   []any{"prod"},
		},
		{
			name:   "slice is flattened",
			repeat: "{ $.ports }",
			want:   []any{"80", "443"},
		},
		{
			name:   "nil slice yields no elements",
			repeat: "{ $.missing }",
		},
		{
			name:    "invalid path",
			repeat:  "{ $.does-not-exist }",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repeat(7, templatesv1.GitOpsSetTemplate{Repeat: tt.repeat}, params)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("repeat() error = %v", err)
			}

			var values []any
			for i, element := range got {
				if element["ElementIndex"] != 7 {
					t.Fatalf("ElementIndex = %v, want 7", element["ElementIndex"])
				}
				if element["RepeatIndex"] != i {
					t.Fatalf("RepeatIndex = %v, want %d", element["RepeatIndex"], i)
				}
				values = append(values, element["Repeat"])
			}
			if diff := cmp.Diff(tt.want, values); diff != "" {
				t.Errorf("repeat values mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
