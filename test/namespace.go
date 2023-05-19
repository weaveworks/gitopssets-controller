package test

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NewNamespace creates a new namespace with the provided name.
func NewNamespace(name string, opts ...func(*corev1.Namespace)) *corev1.Namespace {
	n := corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	for _, o := range opts {
		o(&n)
	}

	return &n
}
