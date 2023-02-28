package apiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators"
	"github.com/weaveworks/gitopssets-controller/test"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ generators.Generator = (*APIClientGenerator)(nil)

func TestGenerate_with_no_generator(t *testing.T) {
	gen := GeneratorFactory(http.DefaultClient)(logr.Discard(), nil)
	_, err := gen.Generate(context.TODO(), nil, nil)

	if err != generators.ErrEmptyGitOpsSet {
		t.Errorf("got error %v", err)
	}
}

func TestGenerate_with_no_config(t *testing.T) {
	gen := GeneratorFactory(http.DefaultClient)(logr.Discard(), nil)
	got, err := gen.Generate(context.TODO(), &templatesv1.GitOpsSetGenerator{}, nil)

	if err != nil {
		t.Errorf("got an error with no pull requests: %s", err)
	}
	if got != nil {
		t.Errorf("got %v, want %v with no APIClient generator", got, nil)
	}
}

func TestGenerate(t *testing.T) {
	ts := httptest.NewTLSServer(newTestMux(t))
	defer ts.Close()

	testCases := []struct {
		name      string
		apiClient *templatesv1.APIClientGenerator
		want      []map[string]any
	}{
		{
			name: "simple API endpoint with get request",
			apiClient: &templatesv1.APIClientGenerator{
				Endpoint: ts.URL + "/api/get-testing",
				Method:   http.MethodGet,
			},
			want: []map[string]any{
				{
					"name": "testing1",
				},
				{
					"name": "testing2",
				},
			},
		},
		{
			name: "simple API endpoint with post request",
			apiClient: &templatesv1.APIClientGenerator{
				Endpoint: ts.URL + "/api/post-testing",
				Method:   http.MethodPost,
			},
			want: []map[string]any{
				{
					"name": "testing1",
				},
				{
					"name": "testing2",
				},
			},
		},
		{
			name: "api endpoint returning map with JSONPath",
			apiClient: &templatesv1.APIClientGenerator{
				Endpoint: ts.URL + "/api/non-array",
				Method:   http.MethodGet,
				JSONPath: "{ $.things }",
			},
			want: []map[string]any{
				{
					"name": "testing1",
				},
				{
					"name": "testing2",
				},
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			gen := GeneratorFactory(ts.Client())(logr.Discard(), fake.NewFakeClient())

			gsg := templatesv1.GitOpsSetGenerator{
				APIClient: tt.apiClient,
			}

			got, err := gen.Generate(context.TODO(), &gsg,
				&templatesv1.GitOpsSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "demo-set",
						Namespace: "default",
					},
					Spec: templatesv1.GitOpsSetSpec{
						Generators: []templatesv1.GitOpsSetGenerator{
							gsg,
						},
					},
				})

			test.AssertNoError(t, err)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Fatalf("failed to generate pull requests:\n%s", diff)
			}
		})
	}
}

func TestGenerate_errors(t *testing.T) {
	ts := httptest.NewTLSServer(newTestMux(t))
	defer ts.Close()

	testCases := []struct {
		name      string
		wantErr   string
		apiClient *templatesv1.APIClientGenerator
	}{
		{
			name: "endpoint returning 404",
			apiClient: &templatesv1.APIClientGenerator{
				Endpoint: ts.URL + "/unknown",
			},
			wantErr: fmt.Sprintf("got 404 response from endpoint %s", ts.URL+"/unknown"),
		},
		{
			name: "invalid JSON response",
			apiClient: &templatesv1.APIClientGenerator{
				Endpoint: ts.URL + "/api/bad",
			},
			wantErr: fmt.Sprintf("failed to unmarshal JSON response from endpoint %s", ts.URL+"/api/bad"),
		},
		{
			name: "jsonpath expression failure",
			apiClient: &templatesv1.APIClientGenerator{
				Endpoint: ts.URL + "/api/get-testing",
				JSONPath: "{",
			},
			wantErr: `failed to parse JSONPath for APIClient generator "{": unclosed action`,
		},
		{
			name: "JSONPath references missing key",
			apiClient: &templatesv1.APIClientGenerator{
				Endpoint: ts.URL + "/api/get-testing",
				JSONPath: "{ $.things }",
			},
			wantErr: `failed to find results from expression { \$.things } accessing endpoint.*: things is not found`,
		},
		{
			name: "JSONPath is not slice of maps",
			apiClient: &templatesv1.APIClientGenerator{
				Endpoint: ts.URL + "/api/map-of-strings",
				JSONPath: "{ $.things }",
			},
			wantErr: `failed to parse response JSONPath { \$.things } did not generate suitable values accessing endpoint`,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			gen := GeneratorFactory(ts.Client())(logr.Discard(), nil)

			gsg := templatesv1.GitOpsSetGenerator{
				APIClient: tt.apiClient,
			}

			_, err := gen.Generate(context.TODO(), &gsg,
				&templatesv1.GitOpsSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "demo-set",
						Namespace: "default",
					},
					Spec: templatesv1.GitOpsSetSpec{
						Generators: []templatesv1.GitOpsSetGenerator{
							gsg,
						},
					},
				})

			test.AssertErrorMatch(t, tt.wantErr, err)
		})
	}
}

func TestAPIClientGenerator_GetInterval(t *testing.T) {
	interval := time.Minute * 10
	gen := NewGenerator(logr.Discard(), fake.NewFakeClient(), http.DefaultClient)
	sg := &templatesv1.GitOpsSetGenerator{
		APIClient: &templatesv1.APIClientGenerator{
			Endpoint: "https://example.com/testing",
			Interval: metav1.Duration{Duration: interval},
		},
	}

	d := gen.Interval(sg)

	if d != interval {
		t.Fatalf("got %#v want %#v", d, interval)
	}
}

func newTestMux(t *testing.T) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()

	writeResponse := func(w http.ResponseWriter) {
		enc := json.NewEncoder(w)
		if err := enc.Encode([]map[string]any{
			{
				"name": "testing1",
			},

			{
				"name": "testing2",
			},
		}); err != nil {
			t.Fatal(err)
		}
	}

	mux.HandleFunc("/api/map-of-strings", func(w http.ResponseWriter, r *http.Request) {
		enc := json.NewEncoder(w)
		if err := enc.Encode(map[string]any{
			"things": []any{
				"testing1",
				"testing2",
			},
		}); err != nil {
			t.Fatal(err)
		}

	})

	mux.HandleFunc("/api/get-testing", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "wrong test endpoint", http.StatusMethodNotAllowed)
			return
		}
		writeResponse(w)
	})

	mux.HandleFunc("/api/post-testing", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "wrong test endpoint", http.StatusMethodNotAllowed)
			return
		}
		writeResponse(w)
	})

	mux.HandleFunc("/api/non-array", func(w http.ResponseWriter, r *http.Request) {
		enc := json.NewEncoder(w)
		if err := enc.Encode(map[string]any{
			"things": []any{
				map[string]string{
					"name": "testing1",
				},
				map[string]string{
					"name": "testing2",
				},
			},
		}); err != nil {
			t.Fatal(err)
		}

	})

	mux.HandleFunc("/api/bad", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{`))
	})

	return mux
}
