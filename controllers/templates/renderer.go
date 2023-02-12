package templates

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"github.com/gitops-tools/pkg/sanitize"
	"github.com/imdario/mergo"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	yamlserializer "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/util/jsonpath"

	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators"
)

var templateFuncs template.FuncMap = makeTemplateFunctions()

// Render parses the GitOpsSet and renders the template resources using
// the configured generators and templates.
func Render(ctx context.Context, r *templatesv1.GitOpsSet, configuredGenerators map[string]generators.Generator) ([]*unstructured.Unstructured, error) {
	rendered := []*unstructured.Unstructured{}

	for _, gen := range r.Spec.Generators {
		generated, err := generate(ctx, gen, configuredGenerators, r)
		if err != nil {
			return nil, fmt.Errorf("failed to generate template for set %s: %w", r.GetName(), err)
		}

		for _, params := range generated {
			for _, param := range params {
				for _, template := range r.Spec.Templates {
					namespacedName := types.NamespacedName{Name: r.GetName(), Namespace: r.GetNamespace()}
					res, err := renderTemplateParams(template, param, namespacedName)
					if err != nil {
						return nil, fmt.Errorf("failed to render template params for set %s: %w", r.GetName(), err)
					}

					rendered = append(rendered, res...)
				}
			}
		}
	}

	return rendered, nil
}

func repeat(tmpl templatesv1.GitOpsSetTemplate, params map[string]any) ([]any, error) {
	if tmpl.Repeat == "" {
		return []any{
			map[string]any{
				"element": params,
			},
		}, nil
	}

	jp := jsonpath.New("repeat")
	err := jp.Parse(tmpl.Repeat)
	if err != nil {
		return nil, fmt.Errorf("failed to parse repeat on template %q: %w", tmpl.Repeat, err)
	}

	results, err := jp.FindResults(params)
	if err != nil {
		return nil, fmt.Errorf("failed to find results from expression %q: %w", tmpl.Repeat, err)
	}

	repeated := []any{}
	for _, result := range results {
		for _, v := range result {
			slice, ok := v.Interface().([]any)
			if ok {
				repeated = append(repeated, slice...)
			} else {
				repeated = append(repeated, v)
			}
		}
	}

	elements := []any{}
	for _, v := range repeated {
		elements = append(elements, map[string]any{
			"element": params,
			"repeat":  v,
		})
	}

	return elements, nil
}

func renderTemplateParams(tmpl templatesv1.GitOpsSetTemplate, params map[string]any, name types.NamespacedName) ([]*unstructured.Unstructured, error) {
	var objects []*unstructured.Unstructured

	repeatedParams, err := repeat(tmpl, params)
	if err != nil {
		return nil, err
	}

	for _, p := range repeatedParams {
		rendered, err := render(tmpl.Content.Raw, p)
		if err != nil {
			return nil, err
		}

		// Technically multiple objects could be in the YAML...
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

			if uns.GetKind() != "Namespace" {
				uns.SetNamespace(name.Namespace)

				// Add source labels
				labels := map[string]string{
					"templates.weave.works/name":      name.Name,
					"templates.weave.works/namespace": name.Namespace,
				}

				renderedLabels := uns.GetLabels()
				if err := mergo.Merge(&labels, renderedLabels, mergo.WithOverride); err != nil {
					return nil, fmt.Errorf("failed to merge existing labels to default labels: %w", err)
				}
				uns.SetLabels(labels)
			}

			objects = append(objects, uns)
		}
	}

	return objects, nil
}

// TODO: pass the `GitOpsSet` through to here so that we can fix the
// `template.New` to include the name/namespace.
func render(b []byte, params any) ([]byte, error) {
	t, err := template.New("gitopsset-template").Funcs(templateFuncs).Parse(string(b))
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	var out bytes.Buffer
	if err := t.Execute(&out, params); err != nil {
		return nil, fmt.Errorf("failed to render template: %w", err)
	}

	return out.Bytes(), nil
}

func generate(ctx context.Context, generator templatesv1.GitOpsSetGenerator, allGenerators map[string]generators.Generator, gitopsSet *templatesv1.GitOpsSet) ([][]map[string]any, error) {
	generated := [][]map[string]any{}
	generators := generators.FindRelevantGenerators(&generator, allGenerators)
	for _, g := range generators {
		res, err := g.Generate(ctx, &generator, gitopsSet)
		if err != nil {
			return nil, err
		}

		generated = append(generated, res)
	}

	return generated, nil
}

func makeTemplateFunctions() template.FuncMap {
	f := sprig.TxtFuncMap()
	unwanted := []string{
		"env", "expandenv", "getHostByName", "genPrivateKey", "derivePassword", "sha256sum",
		"base", "dir", "ext", "clean", "isAbs", "osBase", "osDir", "osExt", "osClean", "osIsAbs"}

	for _, v := range unwanted {
		delete(f, v)
	}

	f["sanitize"] = sanitize.SanitizeDNSName

	return f
}
