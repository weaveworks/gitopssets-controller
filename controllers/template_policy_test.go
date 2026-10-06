package controllers

import (
	"strings"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
)

func TestTemplateDeletionPolicySplitsClaimFromFluxObjects(t *testing.T) {
	cl, gens := rolloutClient(t)
	gs := runtimeGitOpsSet()
	gs.Spec.Templates[0].DeletionPolicy = templatesv1.DeletionPolicyOrphan
	gs.Spec.Templates[1].DeletionPolicy = templatesv1.DeletionPolicyDelete
	gs.Spec.Templates[2].DeletionPolicy = templatesv1.DeletionPolicyDelete
	reconciler := &GitOpsSetReconciler{Client: cl}

	inventory, err := reconciler.renderAndReconcile(t.Context(), logr.Discard(), cl, gs, gens)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range inventory.Entries {
		if entry.ID == "" || entry.DeletionPolicy == "" {
			t.Fatalf("entry = %#v", entry)
		}
	}

	gs = gitOpsSetWithInventory(gs, inventory)
	gs.Spec.Generators[0].List.Elements = gs.Spec.Generators[0].List.Elements[:1]
	inventory, err = reconciler.renderAndReconcile(t.Context(), logr.Discard(), cl, gs, gens)
	if err != nil {
		t.Fatal(err)
	}
	if err := cl.Get(t.Context(), types.NamespacedName{Namespace: "crossplane-system", Name: "prod"}, &corev1.ConfigMap{}); err != nil {
		t.Fatalf("orphaned claim: %v", err)
	}
	if err := cl.Get(t.Context(), types.NamespacedName{Namespace: "flux-system", Name: "runtime-prod-kubeconfig"}, &corev1.ConfigMap{}); !apierrors.IsNotFound(err) {
		t.Fatalf("kubeconfig get = %v", err)
	}
	if objectExists(t, cl, "flux-system", "runtime-prod", "kustomize.toolkit.fluxcd.io/v1beta2", "Kustomization") {
		t.Fatal("prod kustomization was not deleted")
	}
	for _, entry := range inventory.Entries {
		if strings.Contains(entry.ID, "prod") {
			t.Fatalf("prod remained in inventory: %s", entry.ID)
		}
	}

	gs = gitOpsSetWithInventory(gs, inventory)
	gs.Finalizers = []string{templatesv1.GitOpsSetFinalizer}
	if err := cl.Create(t.Context(), gs); err != nil {
		t.Fatal(err)
	}
	if _, err := reconciler.finalize(t.Context(), gs, cl); err != nil {
		t.Fatal(err)
	}
	if err := cl.Get(t.Context(), types.NamespacedName{Namespace: "crossplane-system", Name: "pp"}, &corev1.ConfigMap{}); err != nil {
		t.Fatalf("pp claim after finalize: %v", err)
	}
	if err := cl.Get(t.Context(), types.NamespacedName{Namespace: "flux-system", Name: "runtime-pp-kubeconfig"}, &corev1.ConfigMap{}); !apierrors.IsNotFound(err) {
		t.Fatalf("pp kubeconfig after finalize: %v", err)
	}
}

func TestSetOrphanDoesNotKeepATemplateThatDeletes(t *testing.T) {
	cl, gens := rolloutClient(t)
	gs := gitOpsSetRendering("pp")
	gs.Spec.DeletionPolicy = templatesv1.DeletionPolicyOrphan
	gs.Spec.Templates[0].DeletionPolicy = templatesv1.DeletionPolicyDelete
	reconciler := &GitOpsSetReconciler{}
	inventory, err := reconciler.renderAndReconcile(t.Context(), logr.Discard(), cl, gs, gens)
	if err != nil {
		t.Fatal(err)
	}
	gs = gitOpsSetWithInventory(gs, inventory)
	gs.Spec.Generators[0].List.Elements = nil
	if _, err := reconciler.renderAndReconcile(t.Context(), logr.Discard(), cl, gs, gens); err != nil {
		t.Fatal(err)
	}
	if err := cl.Get(t.Context(), types.NamespacedName{Namespace: "demo", Name: "pp"}, &corev1.ConfigMap{}); !apierrors.IsNotFound(err) {
		t.Fatalf("configmap get = %v", err)
	}
}

func TestDeletionPolicyChangeTakesEffectOnTheNextDrop(t *testing.T) {
	cl, gens := rolloutClient(t)
	gs := gitOpsSetRendering("pp")
	gs.Spec.Templates[0].DeletionPolicy = templatesv1.DeletionPolicyOrphan
	reconciler := &GitOpsSetReconciler{}
	inventory, err := reconciler.renderAndReconcile(t.Context(), logr.Discard(), cl, gs, gens)
	if err != nil {
		t.Fatal(err)
	}
	gs = gitOpsSetWithInventory(gs, inventory)
	gs.Spec.Templates[0].DeletionPolicy = templatesv1.DeletionPolicyDelete
	inventory, err = reconciler.renderAndReconcile(t.Context(), logr.Discard(), cl, gs, gens)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Entries[0].DeletionPolicy != templatesv1.DeletionPolicyDelete {
		t.Fatalf("stored policy = %s", inventory.Entries[0].DeletionPolicy)
	}
	gs = gitOpsSetWithInventory(gs, inventory)
	gs.Spec.Generators[0].List.Elements = nil
	if _, err := reconciler.renderAndReconcile(t.Context(), logr.Discard(), cl, gs, gens); err != nil {
		t.Fatal(err)
	}
	if err := cl.Get(t.Context(), types.NamespacedName{Namespace: "demo", Name: "pp"}, &corev1.ConfigMap{}); !apierrors.IsNotFound(err) {
		t.Fatalf("configmap get = %v", err)
	}
}
