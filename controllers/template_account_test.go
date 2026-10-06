package controllers

import (
	"strings"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/pkg/generators"
	"github.com/gitops-tools/gitopssets-controller/pkg/generators/list"
)

func TestTemplateServiceAccountAppliesAndDeletesSeparately(t *testing.T) {
	fluxClient, _ := rolloutClient(t)
	runtimeClient, _ := rolloutClient(t)
	gs := runtimeGitOpsSet()
	gs.Spec.Generators[0].List.Elements = gs.Spec.Generators[0].List.Elements[:1]
	gs.Spec.ServiceAccountName = "gitopssets-flux"
	gs.Spec.Templates[0].ServiceAccountName = "gitopssets-runtime"
	reconciler := &GitOpsSetReconciler{
		impersonationClient: func(namespace, serviceAccountName string) (client.Client, error) {
			if serviceAccountName != "gitopssets-runtime" {
				t.Fatalf("impersonated %s", serviceAccountName)
			}
			return runtimeClient, nil
		},
	}
	_, gens := rolloutClient(t)

	inventory, err := reconciler.renderAndReconcile(t.Context(), logr.Discard(), fluxClient, gs, gens)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtimeClient.Get(t.Context(), types.NamespacedName{Namespace: "crossplane-system", Name: "pp"}, &corev1.ConfigMap{}); err != nil {
		t.Fatalf("claim on runtime account: %v", err)
	}
	if err := fluxClient.Get(t.Context(), types.NamespacedName{Namespace: "crossplane-system", Name: "pp"}, &corev1.ConfigMap{}); !apierrors.IsNotFound(err) {
		t.Fatalf("claim on flux account: %v", err)
	}
	if err := fluxClient.Get(t.Context(), types.NamespacedName{Namespace: "flux-system", Name: "runtime-pp-kubeconfig"}, &corev1.ConfigMap{}); err != nil {
		t.Fatalf("kubeconfig on flux account: %v", err)
	}
	if !objectExists(t, fluxClient, "flux-system", "runtime-pp", "kustomize.toolkit.fluxcd.io/v1beta2", "Kustomization") {
		t.Fatal("kustomization was not applied as gitopssets-flux")
	}
	if objectExists(t, runtimeClient, "flux-system", "runtime-pp", "kustomize.toolkit.fluxcd.io/v1beta2", "Kustomization") {
		t.Fatal("kustomization was applied as gitopssets-runtime")
	}
	for _, entry := range inventory.Entries {
		if strings.Contains(entry.ID, "crossplane-system_pp_") && entry.ServiceAccountName != "gitopssets-runtime" {
			t.Fatalf("claim account = %s", entry.ServiceAccountName)
		}
		if strings.Contains(entry.ID, "runtime-pp_") && entry.ServiceAccountName != "gitopssets-flux" {
			t.Fatalf("flux object account = %s on %s", entry.ServiceAccountName, entry.ID)
		}
	}

	gs = gitOpsSetWithInventory(gs, inventory)
	gs.Spec.Generators[0].List.Elements = nil
	if _, err := reconciler.renderAndReconcile(t.Context(), logr.Discard(), fluxClient, gs, gens); err != nil {
		t.Fatal(err)
	}
	if err := runtimeClient.Get(t.Context(), types.NamespacedName{Namespace: "crossplane-system", Name: "pp"}, &corev1.ConfigMap{}); !apierrors.IsNotFound(err) {
		t.Fatalf("claim delete: %v", err)
	}
	if objectExists(t, fluxClient, "flux-system", "runtime-pp", "kustomize.toolkit.fluxcd.io/v1beta2", "Kustomization") {
		t.Fatal("kustomization was not deleted as gitopssets-flux")
	}
}

func TestHealthReadUsesTheStoredServiceAccount(t *testing.T) {
	fluxClient, _ := rolloutClient(t)
	runtimeClient, _ := rolloutClient(t)
	claim := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kustomize.toolkit.fluxcd.io/v1beta2",
		"kind":       "Kustomization",
		"metadata":   map[string]any{"name": "pp", "namespace": "crossplane-system"},
		"status":     map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "False"}}},
	}}
	claim.SetGroupVersionKind(schema.GroupVersionKind{Group: "kustomize.toolkit.fluxcd.io", Version: "v1beta2", Kind: "Kustomization"})
	if err := runtimeClient.Create(t.Context(), claim); err != nil {
		t.Fatal(err)
	}
	ref := mustResourceRef(t, claim)
	ref.ServiceAccountName = "gitopssets-runtime"
	gs := &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{Name: "runtime", Namespace: "flux-system"},
		Spec: templatesv1.GitOpsSetSpec{
			ServiceAccountName: "gitopssets-flux",
			HealthCheck:        &templatesv1.HealthCheck{Kinds: []string{"Kustomization"}},
		},
	}
	reconciler := &GitOpsSetReconciler{
		impersonationClient: func(namespace, serviceAccountName string) (client.Client, error) {
			if serviceAccountName != "gitopssets-runtime" {
				t.Fatalf("impersonated %s", serviceAccountName)
			}
			return runtimeClient, nil
		},
	}
	inventory := &templatesv1.ResourceInventory{Entries: []templatesv1.ResourceRef{ref}}
	after := reconciler.observeHealth(t.Context(), fluxClient, gs, inventory)
	if after != healthRequeueInterval {
		t.Fatalf("requeue = %s", after)
	}
	condition := apimeta.FindStatusCondition(gs.Status.Conditions, templatesv1.HealthyCondition)
	if condition == nil || condition.Reason != templatesv1.HealthProgressingReason || !strings.Contains(condition.Message, ref.ID) {
		t.Fatalf("healthy = %#v", condition)
	}
}

func TestGeneratorsKeepTheSetServiceAccount(t *testing.T) {
	controllerClient, _ := rolloutClient(t)
	impersonated, _ := rolloutClient(t)
	templateClient, _ := rolloutClient(t)
	var seen client.Reader
	reconciler := &GitOpsSetReconciler{
		Client: controllerClient,
		Generators: map[string]generators.GeneratorFactory{
			"List": func(l logr.Logger, c client.Reader) generators.Generator {
				seen = c
				return list.NewGenerator(l)
			},
		},
		impersonationClient: func(namespace, serviceAccountName string) (client.Client, error) {
			return templateClient, nil
		},
	}
	gs := gitOpsSetRendering("pp")
	gs.Spec.ServiceAccountName = "gitopssets-flux"
	gs.Spec.Templates[0].ServiceAccountName = "gitopssets-runtime"
	if _, _, err := reconciler.reconcileResources(t.Context(), impersonated, gs); err != nil {
		t.Fatal(err)
	}
	if seen != impersonated {
		t.Fatal("generators did not receive the set service account client")
	}
}
