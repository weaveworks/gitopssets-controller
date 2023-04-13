package test

import (
	"time"

	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func MakeTestKustomization(name types.NamespacedName, opts ...func(*kustomizev1.Kustomization)) *kustomizev1.Kustomization {
	k := &kustomizev1.Kustomization{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Kustomization",
			APIVersion: "kustomize.toolkit.fluxcd.io/v1beta2",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name.Name,
			Namespace: name.Namespace,
		},
		Spec: kustomizev1.KustomizationSpec{
			Interval: metav1.Duration{Duration: 5 * time.Minute},
			Path:     "./examples/kustomize/environments/dev",
			Prune:    true,
			SourceRef: kustomizev1.CrossNamespaceSourceReference{
				Kind: "GitRepository",
				Name: "demo-repo",
			},
		},
	}

	for _, opt := range opts {
		opt(k)
	}

	return k
}
