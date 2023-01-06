package reconciler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"text/template"

	"github.com/gitops-tools/pkg/sanitize"
	"github.com/weaveworks/gitops-sets-controller/controllers/render/generators"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	yamlserializer "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/util/yaml"

	templatesv1 "github.com/weaveworks/gitops-sets-controller/api/v1alpha1"
)

var funcMap = template.FuncMap{
	"sanitize": sanitize.SanitizeDNSName,
}

// RenderTemplates parses the GitOpsSet and renders the template resources using
// the configured generators and templates.
func RenderTemplates(ctx context.Context, r *templatesv1.GitOpsSet, configuredGenerators map[string]generators.Generator) ([]runtime.Object, error) {
	rendered := []runtime.Object{}

	for _, gen := range r.Spec.Generators {
		transformed, err := transform(ctx, gen, configuredGenerators, r.Spec.Template, r)
		if err != nil {
			return nil, fmt.Errorf("failed to transform template for set %s: %w", r.GetName(), err)
		}

		for _, result := range transformed {
			for _, param := range result.Params {
				// TODO: This should iterate!
				res, err := renderTemplateParams(r.Spec.Template, param, r.GetNamespace())
				if err != nil {
					return nil, fmt.Errorf("failed to render template params for set %s: %w", r.GetName(), err)
				}

				rendered = append(rendered, res...)
			}
		}
	}

	return rendered, nil
}

func renderTemplateParams(tmpl templatesv1.GitOpsSetTemplate, params map[string]any, ns string) ([]runtime.Object, error) {
	rendered, err := render(tmpl.Raw, params)
	if err != nil {
		return nil, err
	}

	// Technically multiple objects could be in the YAML...
	var objects []runtime.Object
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(rendered), 100)
	for {
		var rawObj runtime.RawExtension
		if err := decoder.Decode(&rawObj); err != nil {
			if err != io.EOF {
				return nil, fmt.Errorf("failed to parse rendered template: %w", err)
			}
			break
		}

		m, _, err := yamlserializer.NewDecodingSerializer(unstructured.UnstructuredJSONScheme).Decode(rawObj.Raw, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to decode rendered template: %w", err)
		}

		unstructuredMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(m)
		if err != nil {
			return nil, fmt.Errorf("failed convert parsed template: %w", err)
		}
		delete(unstructuredMap, "status")
		uns := &unstructured.Unstructured{Object: unstructuredMap}
		uns.SetNamespace(ns)
		objects = append(objects, uns)
	}

	return objects, nil
}

func render(b []byte, params map[string]any) ([]byte, error) {
	t, err := template.New("gitopsset-template").Funcs(funcMap).Parse(string(b))
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	var out bytes.Buffer
	if err := t.Execute(&out, params); err != nil {
		return nil, fmt.Errorf("failed to render template: %w", err)
	}

	return out.Bytes(), nil
}
