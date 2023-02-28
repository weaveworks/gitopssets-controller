package apiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"time"

	"github.com/go-logr/logr"
	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators"
	"k8s.io/client-go/util/jsonpath"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GeneratorFactory is a function for creating per-reconciliation generators for
// the MatrixGenerator.
func GeneratorFactory(httpClient *http.Client) generators.GeneratorFactory {
	return func(l logr.Logger, c client.Client) generators.Generator {
		return NewGenerator(l, c, httpClient)
	}
}

// APIClientGenerator generates from an API endpoint.
type APIClientGenerator struct {
	client.Client
	HTTPClient *http.Client
	logr.Logger
}

// NewGenerator creates and returns a new API client generator.
func NewGenerator(l logr.Logger, c client.Client, httpClient *http.Client) *APIClientGenerator {
	return &APIClientGenerator{
		Client:     c,
		Logger:     l,
		HTTPClient: httpClient,
	}
}

// Generate makes an HTTP request using the APIClient definition and returns the
// result converted to a slice of maps.
func (g *APIClientGenerator) Generate(ctx context.Context, sg *templatesv1.GitOpsSetGenerator, ks *templatesv1.GitOpsSet) ([]map[string]any, error) {
	if sg == nil {
		g.Logger.Info("no generator provided")
		return nil, generators.ErrEmptyGitOpsSet
	}

	if sg.APIClient == nil {
		g.Logger.Info("API client info is nil")
		return nil, nil
	}

	g.Logger.Info("generating params frou APIClient generator", "endpoint", sg.APIClient.Endpoint)

	req, err := http.NewRequestWithContext(ctx, sg.APIClient.Method, sg.APIClient.Endpoint, nil)
	if err != nil {
		g.Logger.Error(err, "failed to create request", "endpoint", sg.APIClient.Endpoint)
		return nil, err
	}

	resp, err := g.HTTPClient.Do(req)
	if err != nil {
		g.Logger.Error(err, "failed to fetch endpoint", "endpoint", sg.APIClient.Endpoint)
		return nil, err
	}
	defer resp.Body.Close()

	// Anything 400+ is an error?
	if resp.StatusCode >= http.StatusBadRequest {
		// TODO: this should read the body and insert it?
		g.Logger.Info("failed to fetch endpoint", "endpoint", sg.APIClient.Endpoint, "statusCode", resp.StatusCode)
		return nil, fmt.Errorf("got %d response from endpoint %s", resp.StatusCode, sg.APIClient.Endpoint)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		g.Logger.Error(err, "failed to read response", "endpoint", sg.APIClient.Endpoint)
		return nil, err
	}

	if sg.APIClient.JSONPath == "" {
		var result []map[string]any
		if err := json.Unmarshal(body, &result); err != nil {
			g.Logger.Error(err, "failed to unmarshal JSON response", "endpoint", sg.APIClient.Endpoint)
			return nil, fmt.Errorf("failed to unmarshal JSON response from endpoint %s", sg.APIClient.Endpoint)
		}

		res := []map[string]any{}
		for _, v := range result {
			res = append(res, v)
		}

		return res, nil
	}

	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		g.Logger.Error(err, "failed to unmarshal JSON response", "endpoint", sg.APIClient.Endpoint)
		return nil, fmt.Errorf("failed to unmarshal JSON response from endpoint %s", sg.APIClient.Endpoint)
	}

	jp := jsonpath.New("apiclient")
	err = jp.Parse(sg.APIClient.JSONPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JSONPath for APIClient generator %q: %w", sg.APIClient.JSONPath, err)
	}

	results, err := jp.FindResults(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to find results from expression %s accessing endpoint %s: %w", sg.APIClient.JSONPath, sg.APIClient.Endpoint, err)
	}

	res := []map[string]any{}
	for _, r := range results {
		if l := len(r); l != 1 {
			return nil, fmt.Errorf("%d results found with expression %s", l, sg.APIClient.JSONPath)
		}

		for _, v := range r {
			// TODO: improve error case handling!
			items, ok := v.Interface().([]any)
			if !ok {
				return nil, fmt.Errorf("failed to parse response JSONPath %s did not generate suitable array accessing endpoint %s", sg.APIClient.JSONPath, sg.APIClient.Endpoint)
			}
			for _, raw := range items {
				item, ok := raw.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("failed to parse response JSONPath %s did not generate suitable values accessing endpoint %s", sg.APIClient.JSONPath, sg.APIClient.Endpoint)
				}
				res = append(res, item)
			}
		}
	}

	return res, nil
}

// Interval is an implementation of the Generator interface.
func (g *APIClientGenerator) Interval(sg *templatesv1.GitOpsSetGenerator) time.Duration {
	return sg.APIClient.Interval.Duration
}

func extractResult(v reflect.Value) (interface{}, error) {
	if v.CanInterface() {
		return v.Interface(), nil
	}

	return nil, fmt.Errorf("JSONPath couldn't access field")
}
