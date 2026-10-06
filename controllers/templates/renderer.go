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
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	yamlserializer "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/util/jsonpath"
	syaml "sigs.k8s.io/yaml"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/pkg/generators"
)

// TemplateDelimiterAnnotation can be added to a Template to change the Go
// template delimiter.
//
// It's assumed to be a string with "left,right"
// By default the delimiters are the standard Go templating delimiters:
// {{ and }}.
const TemplateDelimiterAnnotation string = "sets.gitops.pro/delimiters"

var templateFuncs template.FuncMap = makeTemplateFunctions()

// Render parses the GitOpsSet and renders the template resources using
// the configured generators and templates.
func Render(ctx context.Context, r *templatesv1.GitOpsSet, configuredGenerators map[string]generators.Generator) ([]*unstructured.Unstructured, error) {
	return RenderWithMapper(ctx, r, configuredGenerators, nil)
}

// RenderedObject is one object produced from a template and one element.
type RenderedObject struct {
	Object             *unstructured.Unstructured
	ElementKey         int
	TemplateName       string
	Requires           []string
	DeletionPolicy     string
	ServiceAccountName string
}

// RenderWithMapper renders templates and uses mapper to decide whether an
// object is namespaced. A nil mapper keeps the historical kind check.
func RenderWithMapper(ctx context.Context, r *templatesv1.GitOpsSet, configuredGenerators map[string]generators.Generator, mapper meta.RESTMapper) ([]*unstructured.Unstructured, error) {
	rendered, err := RenderObjects(ctx, r, configuredGenerators, mapper)
	if err != nil {
		return nil, err
	}
	objects := make([]*unstructured.Unstructured, len(rendered))
	for i := range rendered {
		objects[i] = rendered[i].Object
	}
	return objects, nil
}

// RenderObjects renders templates and keeps the element and template that
// produced each object. ElementKey is shared by every template of one element.
func RenderObjects(ctx context.Context, r *templatesv1.GitOpsSet, configuredGenerators map[string]generators.Generator, mapper meta.RESTMapper) ([]RenderedObject, error) {
	if err := validateTemplates(r); err != nil {
		return nil, err
	}
	for i, template := range r.Spec.Templates {
		if len(template.Content.Raw) == 0 {
			return nil, fmt.Errorf("template %d content must not be empty", i)
		}
	}
	var rendered []RenderedObject

	index := 0
	elementKey := 0
	for genIndex, gen := range r.Spec.Generators {
		generated, err := generate(ctx, gen, configuredGenerators, r)
		if err != nil {
			return nil, fmt.Errorf("failed to generate template for set %s: %w", r.GetName(), err)
		}
		for i := range generated {
			generated[i], err = filterElements(genIndex, gen.Filter, generated[i])
			if err != nil {
				return nil, fmt.Errorf("failed to filter generator %d on set %s: %w", genIndex, r.GetName(), err)
			}
		}

		for _, params := range generated {
			for _, param := range params {
				for _, template := range r.Spec.Templates {
					res, err := renderTemplateParams(mapper, index, template, param, *r)
					if err != nil {
						return nil, fmt.Errorf("failed to render template params for set %s: %w", r.GetName(), err)
					}
					requires := append([]string(nil), template.Requires...)
					for _, obj := range res {
						rendered = append(rendered, RenderedObject{
							Object:             obj,
							ElementKey:         elementKey,
							TemplateName:       template.Name,
							Requires:           requires,
							DeletionPolicy:     template.DeletionPolicy,
							ServiceAccountName: template.ServiceAccountName,
						})
					}
					index++
				}
				elementKey++
			}
		}
	}

	return rendered, nil
}

func validateTemplates(r *templatesv1.GitOpsSet) error {
	names := map[string]struct{}{}
	for _, template := range r.Spec.Templates {
		if template.Name == "" {
			continue
		}
		if _, ok := names[template.Name]; ok {
			return fmt.Errorf("duplicate template name %q", template.Name)
		}
		names[template.Name] = struct{}{}
	}
	for _, template := range r.Spec.Templates {
		switch template.DeletionPolicy {
		case "", templatesv1.DeletionPolicyDelete, templatesv1.DeletionPolicyOrphan:
		default:
			return fmt.Errorf("template %q deletionPolicy %q is invalid", template.Name, template.DeletionPolicy)
		}
		for _, required := range template.Requires {
			if _, ok := names[required]; !ok {
				label := template.Name
				if label == "" {
					label = "unnamed"
				}
				return fmt.Errorf("template %q requires unknown template %q", label, required)
			}
			if required == template.Name {
				return fmt.Errorf("template %q cannot require itself", template.Name)
			}
		}
	}
	return nil
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

	jp := jsonpath.New("repeat")
	err := jp.Parse(tmpl.Repeat)
	if err != nil {
		return nil, fmt.Errorf("failed to parse repeat on template %q: %w", tmpl.Repeat, err)
	}

	results, err := jp.FindResults(params)
	if err != nil {
		return nil, fmt.Errorf("failed to find results from expression %q: %w", tmpl.Repeat, err)
	}

	var repeated []any
	for _, result := range results {
		for _, v := range result {
			slice, ok := v.Interface().([]any)
			if ok {
				repeated = append(repeated, slice...)
				continue
			}

			if isNillable(v.Kind()) && v.IsNil() {
				continue
			}
			repeated = append(repeated, v.Interface())
		}
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

func isNillable(kind reflect.Kind) bool {
	switch kind {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return true
	default:
		return false
	}
}

func renderTemplateParams(mapper meta.RESTMapper, index int, tmpl templatesv1.GitOpsSetTemplate, params map[string]any, gs templatesv1.GitOpsSet) ([]*unstructured.Unstructured, error) {
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
			if metadata, ok := unstructuredMap["metadata"].(map[string]any); ok && metadata["creationTimestamp"] == nil {
				delete(metadata, "creationTimestamp")
			}
			uns := &unstructured.Unstructured{Object: unstructuredMap}

			namespaced, err := ObjectIsNamespaced(mapper, uns)
			if err != nil {
				return nil, err
			}
			if namespaced {
				if uns.GetNamespace() == "" {
					uns.SetNamespace(gs.GetNamespace())
				}
			} else if uns.GetNamespace() != "" {
				return nil, fmt.Errorf("%s is cluster-scoped and cannot set namespace %q", uns.GetKind(), uns.GetNamespace())
			}

			// Ownership labels are controller-managed so another GitOpsSet cannot
			// claim an object by rendering the same keys.
			labels := map[string]string{}
			for key, value := range uns.GetLabels() {
				labels[key] = value
			}
			labels["sets.gitops.pro/name"] = gs.GetName()
			labels["sets.gitops.pro/namespace"] = gs.GetNamespace()
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
		"env", "expandenv", "getHostByName",
		"genPrivateKey", "derivePassword", "genCA", "genSelfSignedCert", "genSignedCert",
		"bcrypt", "htpasswd",
		"uuidv4", "now", "date", "dateInZone", "dateModify", "ago", "unixEpoch",
		"randNumeric", "randAlpha", "randAlphaNum", "randAscii",
		"base", "dir", "ext", "clean", "isAbs", "osBase", "osDir", "osExt", "osClean", "osIsAbs",
	}

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
	f["toYaml"] = func(v interface{}) (string, error) {
		data, err := syaml.Marshal(v)
		if err != nil {
			return "", err
		}
		return strings.TrimSuffix(string(data), "\n"), nil
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
