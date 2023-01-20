package templates

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"reflect"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"github.com/gitops-tools/pkg/sanitize"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	yamlserializer "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/util/yaml"

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
					res, err := renderTemplateParams(template, param, r.GetNamespace())
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

func renderTemplateParams(tmpl templatesv1.GitOpsSetTemplate, params map[string]any, ns string) ([]*unstructured.Unstructured, error) {
	rendered, err := render(tmpl.Content.Raw, params)
	if err != nil {
		return nil, err
	}

	// Technically multiple objects could be in the YAML...
	var objects []*unstructured.Unstructured
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

// TODO: pass the `GitOpsSet` through to here so that we can fix the
// `template.New` to include the name/namespace.
func render(b []byte, params map[string]any) ([]byte, error) {
	t, err := template.New("gitopsset-template").Funcs(templateFuncs).Parse(string(b))
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	data := map[string]any{
		"element": params,
	}

	var out bytes.Buffer
	if err := t.Execute(&out, data); err != nil {
		return nil, fmt.Errorf("failed to render template: %w", err)
	}

	return out.Bytes(), nil
}

func generate(ctx context.Context, generator templatesv1.GitOpsSetGenerator, allGenerators map[string]generators.Generator, gitopsSet *templatesv1.GitOpsSet) ([][]map[string]any, error) {
	generated := [][]map[string]any{}
	generators := findRelevantGenerators(&generator, allGenerators)
	for _, g := range generators {
		res, err := g.Generate(ctx, &generator, gitopsSet)
		if err != nil {
			return nil, err
		}

		generated = append(generated, res)
	}

	return generated, nil
}

func findRelevantGenerators(setGenerator *templatesv1.GitOpsSetGenerator, allGenerators map[string]generators.Generator) []generators.Generator {
	var res []generators.Generator
	v := reflect.Indirect(reflect.ValueOf(setGenerator))
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if !field.CanInterface() {
			continue
		}

		if !reflect.ValueOf(field.Interface()).IsNil() {
			res = append(res, allGenerators[v.Type().Field(i).Name])
		}
	}

	return res
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
