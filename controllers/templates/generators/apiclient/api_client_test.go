package apiclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/controllers/templates/generators"
	"github.com/gitops-tools/gitopssets-controller/test"
)

var _ generators.Generator = (*APIClientGenerator)(nil)

func TestGenerate_with_no_generator(t *testing.T) {
	gen := GeneratorFactory(DefaultClientFactory)(logr.Discard(), nil)
	_, err := gen.Generate(context.TODO(), nil, nil)

	if err != generators.ErrEmptyGitOpsSet {
		t.Errorf("got error %v", err)
	}
}

func TestGenerate_with_no_config(t *testing.T) {
	gen := GeneratorFactory(DefaultClientFactory)(logr.Discard(), nil)
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

	cert, err := x509.ParseCertificate(ts.TLS.Certificates[0].Certificate[0])
	test.AssertNoError(t, err)
	block := pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	}
	testServerCA := pem.EncodeToMemory(&block)

	testCases := []struct {
		name          string
		apiClient     *templatesv1.APIClientGenerator
		clientFactory HTTPClientFactory
		objs          []runtime.Object
		want          []map[string]any
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
			name: "simple API endpoint with expandList false",
			apiClient: &templatesv1.APIClientGenerator{
				Endpoint:      ts.URL + "/api/non-array",
				Method:        http.MethodGet,
				SingleElement: true,
			},
			want: []map[string]any{
				{
					"things": []any{
						map[string]any{"name": "testing1"},
						map[string]any{"name": "testing2"},
					},
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
			name: "simple API endpoint with POST body request",
			apiClient: &templatesv1.APIClientGenerator{
				Endpoint: ts.URL + "/api/post-body",
				Method:   http.MethodPost,
				Body:     &apiextensionsv1.JSON{Raw: []byte(`{"user":"demo","groups":["group1"]}`)},
			},
			want: []map[string]any{
				{
					"name":   "demo",
					"groups": []any{"group1"},
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
		{
			name: "request with custom secret headers",
			apiClient: &templatesv1.APIClientGenerator{
				Endpoint: ts.URL + "/api/secret-header",
				Method:   http.MethodGet,
				HeadersRef: &templatesv1.HeadersReference{
					Name: "test-secret",
					Kind: "Secret",
				},
			},
			objs: []runtime.Object{newTestSecret()},
			want: []map[string]any{
				{
					"name": "Bearer 1234567",
				},
				{
					"name": "test-value",
				},
			},
		},
		{
			name: "request with custom configmap headers",
			apiClient: &templatesv1.APIClientGenerator{
				Endpoint: ts.URL + "/api/config-header",
				Method:   http.MethodGet,
				HeadersRef: &templatesv1.HeadersReference{
					Name: "test-configmap",
					Kind: "ConfigMap",
				},
			},
			objs: []runtime.Object{newTestConfigMap()},
			want: []map[string]any{
				{
					"name": "config-value",
				},
				{
					"name": "configuration",
				},
			},
		},
		{
			name: "access API with custom CA",
			apiClient: &templatesv1.APIClientGenerator{
				Endpoint: ts.URL + "/api/get-testing",
				Method:   http.MethodGet,
				SecretRef: &corev1.LocalObjectReference{
					Name: "https-ca-credentials",
				},
			},
			objs: []runtime.Object{newTestSecret(func(s *corev1.Secret) {
				s.ObjectMeta.Name = "https-ca-credentials"
				s.Data = map[string][]byte{
					"caFile": testServerCA,
				}
			})},
			clientFactory: func(config *tls.Config) *http.Client {
				return &http.Client{
					Transport: &http.Transport{
						TLSClientConfig: config,
					},
				}
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
			factory := func(_ *tls.Config) *http.Client {
				return ts.Client()
			}
			if tt.clientFactory != nil {
				factory = tt.clientFactory
			}

			gen := GeneratorFactory(factory)(logr.Discard(), newFakeClient(t, tt.objs...))

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
		objs      []runtime.Object
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
			name: "non-array response",
			apiClient: &templatesv1.APIClientGenerator{
				Endpoint: ts.URL + "/api/non-array",
			},
			wantErr: fmt.Sprintf("failed to unmarshal JSON from endpoint %s, response is an object not an array", ts.URL+"/api/non-array"),
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
			name: "jsonpath expression with invalid JSON response",
			apiClient: &templatesv1.APIClientGenerator{
				Endpoint: ts.URL + "/api/bad",
				JSONPath: "{ $.testing }",
			},
			wantErr: fmt.Sprintf("failed to unmarshal JSON response from endpoint %s", ts.URL+"/api/bad"),
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
		{
			name: "Reference to unknown secret",
			apiClient: &templatesv1.APIClientGenerator{
				Endpoint: ts.URL + "/api/get-testing",
				HeadersRef: &templatesv1.HeadersReference{
					Name: "test-secret",
					Kind: "Secret",
				},
			},
			wantErr: `failed to load Secret for Request headers default/test-secret: secrets "test-secret" not found`,
		},
		{
			name: "Reference to unknown config-map",
			apiClient: &templatesv1.APIClientGenerator{
				Endpoint: ts.URL + "/api/get-testing",
				HeadersRef: &templatesv1.HeadersReference{
					Name: "test-configmap",
					Kind: "ConfigMap",
				},
			},
			wantErr: `failed to load ConfigMap for Request headers default/test-configmap: configmaps "test-configmap" not found`,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			factory := func(_ *tls.Config) *http.Client {
				return ts.Client()
			}
			gen := GeneratorFactory(factory)(logr.Discard(), newFakeClient(t, tt.objs...))

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
	gen := NewGenerator(logr.Discard(), fake.NewFakeClient(), DefaultClientFactory)
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

	mux.HandleFunc("/api/secret-header", func(w http.ResponseWriter, r *http.Request) {
		enc := json.NewEncoder(w)
		if err := enc.Encode([]map[string]any{
			{
				"name": r.Header.Get("Authorization"),
			},
			{
				"name": r.Header.Get("Second-Value"),
			},
		}); err != nil {
			t.Fatal(err)
		}
	})

	mux.HandleFunc("/api/config-header", func(w http.ResponseWriter, r *http.Request) {
		enc := json.NewEncoder(w)
		if err := enc.Encode([]map[string]any{
			{
				"name": r.Header.Get("Config-Key"),
			},
			{
				"name": r.Header.Get("testValue"),
			},
		}); err != nil {
			t.Fatal(err)
		}
	})

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

	mux.HandleFunc("/api/post-body", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "unsupported method", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "unsupported content type", http.StatusUnsupportedMediaType)
			return
		}
		var body map[string]any
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&body); err != nil {
			http.Error(w, "invalid json "+err.Error(), http.StatusBadRequest)
			return
		}

		enc := json.NewEncoder(w)
		if err := enc.Encode([]map[string]any{
			{
				"name":   body["user"],
				"groups": body["groups"],
			},
		}); err != nil {
			t.Fatal(err)
		}
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

func Test_addHeadersFromSecretToRequest(t *testing.T) {
	secret := newTestSecret()
	kc := newFakeClient(t, secret)

	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := addHeadersFromSecretToRequest(context.TODO(), kc, req, client.ObjectKeyFromObject(secret)); err != nil {
		t.Fatal(err)
	}

	if v := req.Header.Get("Testing"); v != "value" {
		t.Fatalf("got header %s value %q, want %q", "Testing", v, "value")
	}
}

func Test_addHeadersFromConfigMapToRequest(t *testing.T) {
	configMap := newTestConfigMap()
	kc := newFakeClient(t, configMap)

	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := addHeadersFromConfigMapToRequest(context.TODO(), kc, req, client.ObjectKeyFromObject(configMap)); err != nil {
		t.Fatal(err)
	}

	if v := req.Header.Get("Config-Key"); v != "config-value" {
		t.Fatalf("got header %s value %q, want %q", "Config-Key", v, "config-value")
	}
}

func newFakeClient(t *testing.T, objs ...runtime.Object) client.WithWatch {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	return fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()
}

func newTestSecret(opts ...func(*corev1.Secret)) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"testing":       []byte("value"),
			"Authorization": []byte("Bearer 1234567"),
			"second-value":  []byte("test-value"),
		},
	}

	for _, opt := range opts {
		opt(secret)
	}

	return secret
}

func newTestConfigMap(opts ...func(*corev1.ConfigMap)) *corev1.ConfigMap {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap",
			Namespace: "default",
		},
		Data: map[string]string{
			"config-key": "config-value",
			"testValue":  "configuration",
		},
	}

	for _, opt := range opts {
		opt(configMap)
	}

	return configMap
}
