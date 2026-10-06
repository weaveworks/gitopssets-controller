package controllers

import (
	"testing"

	"github.com/fluxcd/pkg/apis/meta"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
)

func TestObserveHealthKeepsReadyWhileADeploymentProgresses(t *testing.T) {
	deploy := deployment("demo", "app", 1, 0)
	cl := fakeClientWith(t, deploy)
	gs := &templatesv1.GitOpsSet{ObjectMeta: metav1.ObjectMeta{Name: "set", Namespace: "demo", Generation: 3}}
	inventory := &templatesv1.ResourceInventory{Entries: []templatesv1.ResourceRef{mustResourceRef(t, deploy)}}
	templatesv1.SetGitOpsSetReadiness(gs, inventory, metav1.ConditionTrue, templatesv1.ReconciliationSucceededReason, "1 resources created")

	after := (&GitOpsSetReconciler{}).observeHealth(t.Context(), cl, gs, inventory)
	if after != healthRequeueInterval {
		t.Fatalf("requeue = %s", after)
	}
	assertCondition(t, gs, meta.ReadyCondition, metav1.ConditionTrue, "1 resources created")
	assertCondition(t, gs, templatesv1.HealthyCondition, metav1.ConditionFalse, "1 resources are not ready")

	ready := deployment("demo", "app", 1, 1)
	cl = fakeClientWith(t, ready)
	after = (&GitOpsSetReconciler{}).observeHealth(t.Context(), cl, gs, &templatesv1.ResourceInventory{Entries: []templatesv1.ResourceRef{mustResourceRef(t, ready)}})
	if after != 0 {
		t.Fatalf("requeue = %s", after)
	}
	assertCondition(t, gs, templatesv1.HealthyCondition, metav1.ConditionTrue, "checked resources are ready")
	assertCondition(t, gs, meta.ReadyCondition, metav1.ConditionTrue, "1 resources created")
}

func TestObserveHealthDisabled(t *testing.T) {
	enabled := false
	gs := &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{Generation: 1},
		Spec:       templatesv1.GitOpsSetSpec{HealthCheck: &templatesv1.HealthCheck{Enabled: &enabled}},
	}
	templatesv1.SetGitOpsSetReadiness(gs, nil, metav1.ConditionTrue, templatesv1.ReconciliationSucceededReason, "0 resources created")
	if after := (&GitOpsSetReconciler{}).observeHealth(t.Context(), nil, gs, nil); after != 0 {
		t.Fatalf("requeue = %s", after)
	}
	assertCondition(t, gs, templatesv1.HealthyCondition, metav1.ConditionTrue, "health checks are disabled")
}

func TestObjectIsHealthyForFluxAndWorkloads(t *testing.T) {
	if objectIsHealthy(deployment("demo", "app", 2, 2)) != true {
		t.Fatal("deployment should be healthy")
	}
	daemon := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "DaemonSet",
		"status":     map[string]any{"numberReady": int64(2), "desiredNumberScheduled": int64(2)},
	}}
	if !objectIsHealthy(daemon) {
		t.Fatal("daemonset should be healthy")
	}
	kustomization := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kustomize.toolkit.fluxcd.io/v1",
		"kind":       "Kustomization",
		"status": map[string]any{
			"conditions": []any{map[string]any{"type": "Ready", "status": "True"}},
		},
	}}
	if !objectIsHealthy(kustomization) {
		t.Fatal("kustomization should be healthy")
	}
	if objectIsHealthy(&unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kustomize.toolkit.fluxcd.io/v1",
		"kind":       "Kustomization",
		"status":     map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "False"}}},
	}}) {
		t.Fatal("unready kustomization should not be healthy")
	}
}

func deployment(namespace, name string, replicas, available int64) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
		"spec":   map[string]any{"replicas": replicas},
		"status": map[string]any{"availableReplicas": available},
	}}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"})
	return obj
}

func fakeClientWith(t *testing.T, objs ...runtime.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()
}

func assertCondition(t *testing.T, gs *templatesv1.GitOpsSet, conditionType string, status metav1.ConditionStatus, message string) {
	t.Helper()
	condition := apimeta.FindStatusCondition(gs.Status.Conditions, conditionType)
	if condition == nil || condition.Status != status || condition.Message != message {
		t.Fatalf("%s = %#v", conditionType, condition)
	}
}
