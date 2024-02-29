package list

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/controllers/templates/generators"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ListGenerator is a generic JSON object list.
type ListGenerator struct {
	Logger logr.Logger
}

// GeneratorFactory is a function for creating per-reconciliation generators for
// the ListGenerator.
func GeneratorFactory(l logr.Logger, _ client.Reader) generators.Generator {
	return NewGenerator(l)
}

// NewGenerator creates and returns a new list generator.
func NewGenerator(l logr.Logger) *ListGenerator {
	return &ListGenerator{Logger: l}
}

func (g *ListGenerator) Generate(_ context.Context, sg *templatesv1.GitOpsSetGenerator, _ *templatesv1.GitOpsSet) ([]map[string]any, error) {
	if sg == nil {
		return nil, generators.ErrEmptyGitOpsSet
	}

	if sg.List == nil {
		return nil, nil
	}

	g.Logger.Info("generating params from List generator")

	res := make([]map[string]any, len(sg.List.Elements))
	for i, el := range sg.List.Elements {
		element := map[string]any{}
		if err := json.Unmarshal(el.Raw, &element); err != nil {
			return nil, fmt.Errorf("error unmarshaling list element: %w", err)
		}
		res[i] = element
	}

	return res, nil
}

// Interval is an implementation of the Generator interface.
func (g *ListGenerator) Interval(sg *templatesv1.GitOpsSetGenerator) time.Duration {
	return generators.NoRequeueInterval
}
