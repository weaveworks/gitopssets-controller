package main

import (
	"fmt"

	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"
	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	yamlserializer "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

func bytesToGitOpsSet(scheme *runtime.Scheme, b []byte) (*templatesv1.GitOpsSet, error) {
	m, _, err := yamlserializer.NewDecodingSerializer(unstructured.UnstructuredJSONScheme).Decode(b, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decode rendered template: %w", err)
	}

	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(m)
	if err != nil {
		return nil, err
	}

	u := &unstructured.Unstructured{Object: raw}
	newObj, err := scheme.New(u.GetObjectKind().GroupVersionKind())
	if err != nil {
		return nil, err
	}

	return newObj.(*templatesv1.GitOpsSet), scheme.Convert(u, newObj, nil)
}

func buildScheme() (*runtime.Scheme, error) {
	scheme := runtime.NewScheme()

	builder := runtime.SchemeBuilder{
		clientgoscheme.AddToScheme,
		templatesv1.AddToScheme,
		sourcev1.AddToScheme,
	}

	err := builder.AddToScheme(scheme)

	return scheme, err
}
