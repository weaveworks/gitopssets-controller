package config

import (
	"context"
	"fmt"
	"time"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/controllers/templates/generators"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ConfigGenerator generates a single resource from a referenced ConfigMap or
// Secret.
type ConfigGenerator struct {
	Client client.Reader
	logr.Logger
}

// GeneratorFactory is a function for creating per-reconciliation generators for
// the ConfigGenerator.
func GeneratorFactory(l logr.Logger, c client.Reader) generators.Generator {
	return NewGenerator(l, c)
}

// NewGenerator creates and returns a new config generator.
func NewGenerator(l logr.Logger, c client.Reader) *ConfigGenerator {
	return &ConfigGenerator{
		Client: c,
		Logger: l,
	}
}

func (g *ConfigGenerator) Generate(ctx context.Context, sg *templatesv1.GitOpsSetGenerator, ks *templatesv1.GitOpsSet) ([]map[string]any, error) {
	if sg == nil {
		return nil, generators.ErrEmptyGitOpsSet
	}

	if sg.Config == nil {
		return nil, nil
	}
	g.Logger.Info("generating params from Config generator")

	var paramsList []map[string]any

	switch sg.Config.Kind {
	case "ConfigMap":
		data, err := configMapToParams(ctx, g.Client, client.ObjectKey{Name: sg.Config.Name, Namespace: ks.GetNamespace()})
		if err != nil {
			return nil, err
		}
		paramsList = append(paramsList, data)

	case "Secret":
		data, err := secretToParams(ctx, g.Client, client.ObjectKey{Name: sg.Config.Name, Namespace: ks.GetNamespace()})
		if err != nil {
			return nil, err
		}
		paramsList = append(paramsList, data)

	default:
		return nil, fmt.Errorf("unknown Config Kind %q %q", sg.Config.Kind, sg.Config.Name)
	}

	return paramsList, nil
}

// Interval is an implementation of the Generator interface.
func (g *ConfigGenerator) Interval(sg *templatesv1.GitOpsSetGenerator) time.Duration {
	return generators.NoRequeueInterval
}

func configMapToParams(ctx context.Context, k8sClient client.Reader, key client.ObjectKey) (map[string]any, error) {
	var configMap corev1.ConfigMap

	if err := k8sClient.Get(ctx, key, &configMap); err != nil {
		return nil, err
	}

	return mapToAnyMap(configMap.Data), nil
}

func secretToParams(ctx context.Context, k8sClient client.Reader, key client.ObjectKey) (map[string]any, error) {
	var secret corev1.Secret

	if err := k8sClient.Get(ctx, key, &secret); err != nil {
		return nil, err
	}

	return mapToAnyMap(secret.Data), nil
}

func mapToAnyMap[V string | []byte](m map[string]V) map[string]any {
	result := map[string]any{}

	for k, v := range m {
		result[k] = string(v)
	}

	return result
}
