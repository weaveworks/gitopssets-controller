package test

import (
	imagev1 "github.com/fluxcd/image-reflector-controller/api/v1beta2"
	sourcev1beta2 "github.com/fluxcd/source-controller/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const testNamespace = "default"

// NewImagePolicy creates and returns a new Flux ImagePolicy.
func NewImagePolicy(opts ...func(*imagev1.ImagePolicy)) *imagev1.ImagePolicy {
	ip := &imagev1.ImagePolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "image.toolkit.fluxcd.io/v1beta2",
			Kind:       "ImagePolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-policy",
			Namespace: testNamespace,
		},
	}

	for _, opt := range opts {
		opt(ip)
	}

	return ip
}

// NewConfigMap creates and returns a new ConfigMap.
func NewConfigMap(opts ...func(*corev1.ConfigMap)) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-cm",
			Namespace: "default",
		},
		Data: map[string]string{
			"testing": "test",
		},
	}

	for _, o := range opts {
		o(cm)
	}

	return cm
}

// NewSecret creates and returns a new Secret.
func NewSecret(opts ...func(*corev1.Secret)) *corev1.Secret {
	cm := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"testing": []byte("test"),
		},
	}

	for _, o := range opts {
		o(cm)
	}

	return cm
}

// NewGitRepository creates and returns a new GitRepository.
func NewGitRepository(opts ...func(*sourcev1beta2.GitRepository)) *sourcev1beta2.GitRepository {
	gr := &sourcev1beta2.GitRepository{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "source.toolkit.fluxcd.io/v1beta2",
			Kind:       "GitRepository",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-repository",
			Namespace: testNamespace,
		},
		Spec: sourcev1beta2.GitRepositorySpec{
			URL: "https://github.com/gitops-tools/gitopssets-controller",
		},
	}

	for _, opt := range opts {
		opt(gr)
	}

	return gr
}
