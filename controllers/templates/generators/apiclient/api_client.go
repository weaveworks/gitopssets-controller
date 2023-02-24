package apiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators"
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
	// TODO: check that the response status code is right

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

// Interval is an implementation of the Generator interface.
func (g *APIClientGenerator) Interval(sg *templatesv1.GitOpsSetGenerator) time.Duration {
	return sg.APIClient.Interval.Duration
}
