package templates

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"reflect"
	"strings"
	"text/template"

	"dario.cat/mergo"
	"github.com/Masterminds/sprig/v3"
	"github.com/gitops-tools/pkg/sanitize"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	celext "github.com/google/cel-go/ext"
	"google.golang.org/protobuf/types/known/structpb"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	yamlserializer "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/util/jsonpath"
	syaml "sigs.k8s.io/yaml"

	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators"
)

// TemplateDelimiterAnnotation can be added to a Template to change the Go
// template delimiter.
//
// It's assumed to be a string with "left,right"
// By default the delimiters are the standard Go templating delimiters:
// {{ and }}.
const TemplateDelimiterAnnotation string = "templates.weave.works/delimiters"

var (
	templateFuncs template.FuncMap = makeTemplateFunctions()
	listType                       = reflect.TypeOf(&structpb.ListValue{})
)

// Render parses the GitOpsSet and renders the template resources using
// the configured generators and templates.
func Render(ctx context.Context, r *templatesv1.GitOpsSet, configuredGenerators map[string]generators.Generator) ([]*unstructured.Unstructured, error) {
	rendered := []*unstructured.Unstructured{}

	index := 0
	for _, gen := range r.Spec.Generators {
		generated, err := generate(ctx, gen, configuredGenerators, r)
		if err != nil {
			return nil, fmt.Errorf("failed to generate template for set %s: %w", r.GetName(), err)
		}

		for _, params := range generated {
			for _, param := range params {
				for _, template := range r.Spec.Templates {
					res, err := renderTemplateParams(index, template, param, *r)
					if err != nil {
						return nil, fmt.Errorf("failed to render template params for set %s: %w", r.GetName(), err)
					}

					rendered = append(rendered, res...)
					index++
				}
			}
		}
	}

	return rendered, nil
}

func repeatFromJSONPath(repeatString string, params map[string]any) ([]any, error) {
	jp := jsonpath.New("repeat")
	err := jp.Parse(repeatString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse repeat on template %q: %w", repeatString, err)
	}

	results, err := jp.FindResults(params)
	if err != nil {
		return nil, fmt.Errorf("failed to find results from expression %q: %w", repeatString, err)
	}

	var repeated []any
	for _, result := range results {
		for _, v := range result {
			slice, ok := v.Interface().([]any)
			if ok {
				repeated = append(repeated, slice...)
				continue
			}

			if !v.IsNil() {
				repeated = append(repeated, v.Interface())
			}
		}
	}
	return repeated, nil
}

func repeatFromCEL(repeatString string, params map[string]any) ([]any, error) {
	env, err := makeCELEnv()
	if err != nil {
		return nil, err
	}

	evalContext := map[string]interface{}{
		"Element": params,
	}

	evaluated, err := evaluate(repeatString, env, evalContext)
	if err != nil {
		return nil, err
	}

	repeated := []any{}

	switch v := evaluated.(type) {
	case traits.Lister:
		it := v.Iterator()
		for it.HasNext().Value() == true {
			repeatElement := it.Next()

			switch repeatElement.(type) {
			case traits.Mapper:
				raw, err := repeatElement.ConvertToNative(reflect.TypeOf(map[string]any{}))
				if err == nil {
					repeated = append(repeated, raw.(map[string]any))
				}
			default:
				repeated = append(repeated, repeatElement)
			}
		}
	}

	return repeated, nil
}

func repeat(index int, tmpl templatesv1.GitOpsSetTemplate, params map[string]any) ([]map[string]any, error) {
	if tmpl.Repeat == "" {
		return []map[string]any{
			map[string]any{
				"Element":      params,
				"ElementIndex": index,
			},
		}, nil
	}

	var repeated []any

	if strings.HasPrefix(tmpl.Repeat, "cel:") {
		repeats, err := repeatFromCEL(strings.TrimPrefix(tmpl.Repeat, "cel:"), params)
		if err != nil {
			return nil, err
		}

		repeated = repeats
	} else {
		repeats, err := repeatFromJSONPath(tmpl.Repeat, params)
		if err != nil {
			return nil, err
		}

		repeated = repeats
	}

	elements := []map[string]any{}
	for i, v := range repeated {
		elements = append(elements, map[string]any{
			"Element":      params,
			"ElementIndex": index,
			"Repeat":       v,
			"RepeatIndex":  i,
		})
	}

	return elements, nil
}

func renderTemplateParams(index int, tmpl templatesv1.GitOpsSetTemplate, params map[string]any, gs templatesv1.GitOpsSet) ([]*unstructured.Unstructured, error) {
	var objects []*unstructured.Unstructured

	repeatedParams, err := repeat(index, tmpl, params)
	if err != nil {
		return nil, err
	}

	// Raw extension is always JSON bytes, so convert back to YAML bytes as the gitopssets was
	// most likely written in YAML, this supports correctly templating numbers
	//
	// Example:
	// 1. As the yaml gitops.yaml file we have: `num: ${{ .Element.Number }}`
	// 2. As the RawExtension (JSON) when gitops.yaml is loaded to cluster: `{ "num": "${{ .Element.Number }}"}`
	// 3. [HERE] Convert back to YAML bytes which strips quotes again: `num: ${{ .Element.Number }}`
	// 4. Rendered correctly as a number type without quotes: `num: 1`
	// 5. Applied back into the cluster as number type
	//
	yamlBytes, err := syaml.JSONToYAML(tmpl.Content.Raw)
	if err != nil {
		return nil, fmt.Errorf("failed to convert template to YAML: %w", err)
	}

	for _, p := range repeatedParams {
		rendered, err := render(yamlBytes, p, gs)
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

			if IsNamespacedObject(uns) {
				if uns.GetNamespace() == "" {
					uns.SetNamespace(gs.GetNamespace())
				}
			}

			// Add source labels
			labels := map[string]string{
				"templates.weave.works/name":      gs.GetName(),
				"templates.weave.works/namespace": gs.GetNamespace(),
			}

			renderedLabels := uns.GetLabels()
			if err := mergo.Merge(&labels, renderedLabels, mergo.WithOverride); err != nil {
				return nil, fmt.Errorf("failed to merge existing labels to default labels: %w", err)
			}
			uns.SetLabels(labels)

			objects = append(objects, uns)
		}
	}

	return objects, nil
}

func render(b []byte, params map[string]any, gs templatesv1.GitOpsSet) ([]byte, error) {
	t, err := template.New(fmt.Sprintf("%s/%s", gs.GetNamespace(), gs.GetName())).
		Option("missingkey=error").
		Delims(templateDelims(gs)).
		Funcs(templateFuncs).Parse(string(b))
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	if err := mergo.Merge(&params, templateParams(gs), mergo.WithOverride); err != nil {
		return nil, fmt.Errorf("failed to generate context when rendering template: %w", err)
	}

	var out bytes.Buffer
	if err := t.Execute(&out, params); err != nil {
		return nil, fmt.Errorf("failed to render template: %w", err)
	}

	return out.Bytes(), nil
}

func templateParams(gs templatesv1.GitOpsSet) map[string]any {
	return map[string]any{
		"GitOpsSet": map[string]any{
			"Name":      gs.GetName(),
			"Namespace": gs.GetNamespace(),
		},
	}
}

func generate(ctx context.Context, generator templatesv1.GitOpsSetGenerator, allGenerators map[string]generators.Generator, gitopsSet *templatesv1.GitOpsSet) ([][]map[string]any, error) {
	generated := [][]map[string]any{}
	generators, err := generators.FindRelevantGenerators(&generator, allGenerators)
	if err != nil {
		return nil, err
	}
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
	f["getordefault"] = func(element map[string]any, key string, def interface{}) interface{} {
		if v, ok := element[key]; ok {
			return v
		}

		return def
	}
	f["toYaml"] = func(v interface{}) string {
		data, err := syaml.Marshal(v)
		if err != nil {
			// Swallow errors inside of a template.
			return ""
		}
		return strings.TrimSuffix(string(data), "\n")
	}

	return f
}

func templateDelims(gs templatesv1.GitOpsSet) (string, string) {
	ann, ok := gs.GetAnnotations()[TemplateDelimiterAnnotation]
	if ok {
		if elems := strings.Split(ann, ","); len(elems) == 2 {
			return elems[0], elems[1]
		}
	}
	return "{{", "}}"
}

func evaluate(expr string, env *cel.Env, data map[string]any) (ref.Val, error) {
	parsed, issues := env.Parse(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("failed to parse expression %#v: %w", expr, issues.Err())
	}

	checked, issues := env.Check(parsed)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("expression %#v check failed: %w", expr, issues.Err())
	}

	prg, err := env.Program(checked, cel.EvalOptions(cel.OptOptimize))
	if err != nil {
		return nil, fmt.Errorf("expression %#v failed to create a Program: %w", expr, err)
	}

	out, _, err := prg.Eval(data)
	if err != nil {
		return nil, fmt.Errorf("expression %#v failed to evaluate: %w", expr, err)
	}
	return out, nil
}

func makeCELEnv() (*cel.Env, error) {
	mapStrDyn := decls.NewMapType(decls.String, decls.Dyn)
	return cel.NewEnv(
		celext.Strings(),
		celext.Encoders(),
		cel.Declarations(
			decls.NewVar("Element", mapStrDyn),
		))
}
