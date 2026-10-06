package controllers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/pkg/generators"
	"github.com/gitops-tools/gitopssets-controller/pkg/generators/list"
	"github.com/gitops-tools/gitopssets-controller/test"
)

func TestUnchangedRuntimeObjectsAreNotAppliedAgain(t *testing.T) {
	base, gens := rolloutClient(t)
	counter := &countingPatchClient{Client: base}
	gs := runtimeGitOpsSet()
	gs.Spec.Force = true
	reconciler := &GitOpsSetReconciler{}

	inventory, err := reconciler.renderAndReconcile(t.Context(), logr.Discard(), counter, gs, gens)
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Entries) != 6 {
		t.Fatalf("entries = %d", len(inventory.Entries))
	}
	if counter.patches != 6 {
		t.Fatalf("patches = %d %v", counter.patches, counter.names)
	}
	for _, entry := range inventory.Entries {
		if entry.RenderHash == "" {
			t.Fatalf("missing render hash on %s", entry.ID)
		}
	}

	gs = gitOpsSetWithInventory(gs, inventory)
	if _, err := reconciler.renderAndReconcile(t.Context(), logr.Discard(), counter, gs, gens); err != nil {
		t.Fatal(err)
	}
	if counter.patches != 6 {
		t.Fatalf("second reconcile patches = %d %v", counter.patches, counter.names)
	}

	raw, err := json.Marshal(map[string]string{"env": "prod", "project": "pw-runtime-prod", "region": "us-central1"})
	if err != nil {
		t.Fatal(err)
	}
	gs.Spec.Generators[0].List.Elements[1] = apiextensionsv1.JSON{Raw: raw}
	if _, err := reconciler.renderAndReconcile(t.Context(), logr.Discard(), counter, gs, gens); err != nil {
		t.Fatal(err)
	}
	if counter.patches != 7 {
		t.Fatalf("patches after region change = %d %v", counter.patches, counter.names)
	}
	if counter.names[6] != "flux-system/runtime-prod-kubeconfig" {
		t.Fatalf("patched %s", counter.names[6])
	}
}

func TestMissingInventoryEntryIsApplied(t *testing.T) {
	base, gens := rolloutClient(t)
	counter := &countingPatchClient{Client: base}
	gs := runtimeGitOpsSet()
	reconciler := &GitOpsSetReconciler{}
	inventory, err := reconciler.renderAndReconcile(t.Context(), logr.Discard(), counter, gs, gens)
	if err != nil {
		t.Fatal(err)
	}
	var kept []templatesv1.ResourceRef
	for _, entry := range inventory.Entries {
		if strings.Contains(entry.ID, "crossplane-system_pp_") {
			continue
		}
		kept = append(kept, entry)
	}
	gs = gitOpsSetWithInventory(gs, &templatesv1.ResourceInventory{Entries: kept})
	if _, err := reconciler.renderAndReconcile(t.Context(), logr.Discard(), counter, gs, gens); err != nil {
		t.Fatal(err)
	}
	if counter.patches != 7 {
		t.Fatalf("patches = %d %v", counter.patches, counter.names)
	}
}

func TestLastErrorRetriesAnUnchangedObject(t *testing.T) {
	base, gens := rolloutClient(t)
	counter := &countingPatchClient{Client: base}
	gs := runtimeGitOpsSet()
	reconciler := &GitOpsSetReconciler{}
	inventory, err := reconciler.renderAndReconcile(t.Context(), logr.Discard(), counter, gs, gens)
	if err != nil {
		t.Fatal(err)
	}
	for i := range inventory.Entries {
		if strings.Contains(inventory.Entries[i].ID, "runtime-prod-kubeconfig") {
			inventory.Entries[i].LastError = "conflict"
		}
	}
	gs = gitOpsSetWithInventory(gs, inventory)
	if _, err := reconciler.renderAndReconcile(t.Context(), logr.Discard(), counter, gs, gens); err != nil {
		t.Fatal(err)
	}
	if counter.patches != 7 || counter.names[6] != "flux-system/runtime-prod-kubeconfig" {
		t.Fatalf("patches = %v", counter.names)
	}
}

func TestReconcileSkipsTheSuccessEventWhenNothingChanged(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := templatesv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	gs := runtimeGitOpsSet()
	gs.Finalizers = []string{templatesv1.GitOpsSetFinalizer}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&templatesv1.GitOpsSet{}).WithObjects(gs).Build()
	recorder := &test.FakeEventRecorder{}
	reconciler := &GitOpsSetReconciler{
		Client:        cl,
		EventRecorder: recorder,
		Generators: map[string]generators.GeneratorFactory{
			"List": list.GeneratorFactory,
		},
	}

	if _, err := reconciler.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)}); err != nil {
		t.Fatal(err)
	}
	if !sawEvent(recorder, "Reconciliation finished") {
		t.Fatalf("events = %#v", eventMessages(recorder))
	}
	recorder.Reset()
	if _, err := reconciler.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gs)}); err != nil {
		t.Fatal(err)
	}
	if sawEvent(recorder, "Reconciliation finished") {
		t.Fatalf("events = %#v", eventMessages(recorder))
	}
}

func runtimeGitOpsSet() *templatesv1.GitOpsSet {
	elements := []map[string]string{
		{"env": "pp", "project": "pw-runtime-pp", "region": "us-east4"},
		{"env": "prod", "project": "pw-runtime-prod", "region": "us-east4"},
	}
	raws := make([]apiextensionsv1.JSON, 0, len(elements))
	for _, element := range elements {
		raw, err := json.Marshal(element)
		if err != nil {
			panic(err)
		}
		raws = append(raws, apiextensionsv1.JSON{Raw: raw})
	}
	return &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{Name: "runtime", Namespace: "flux-system"},
		Spec: templatesv1.GitOpsSetSpec{
			Generators: []templatesv1.GitOpsSetGenerator{{
				List: &templatesv1.ListGenerator{Elements: raws},
			}},
			Templates: []templatesv1.GitOpsSetTemplate{
				{Name: "claim", Content: runtime.RawExtension{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"{{ .Element.env }}","namespace":"crossplane-system"},"data":{"environment":"{{ .Element.env }}"}}`)}},
				{Name: "kubeconfig", Content: runtime.RawExtension{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"runtime-{{ .Element.env }}-kubeconfig","namespace":"flux-system"},"data":{"provider":"gcp","cluster":"projects/{{ .Element.project }}/locations/{{ .Element.region }}/clusters/runtime-{{ .Element.env }}"}}`)}},
				{Name: "apply", Content: runtime.RawExtension{Raw: []byte(`{"apiVersion":"kustomize.toolkit.fluxcd.io/v1beta2","kind":"Kustomization","metadata":{"name":"runtime-{{ .Element.env }}","namespace":"flux-system"},"spec":{"interval":"10m","path":"./clusters/runtime/{{ .Element.env }}","prune":true,"wait":true,"sourceRef":{"kind":"GitRepository","name":"flux-system"},"kubeConfig":{"configMapRef":{"name":"runtime-{{ .Element.env }}-kubeconfig"}}}}`)}},
			},
		},
	}
}

func sawEvent(recorder *test.FakeEventRecorder, fragment string) bool {
	for _, event := range recorder.Events {
		if strings.Contains(event.Message, fragment) {
			return true
		}
	}
	return false
}

func eventMessages(recorder *test.FakeEventRecorder) []string {
	messages := make([]string, 0, len(recorder.Events))
	for _, event := range recorder.Events {
		messages = append(messages, event.Message)
	}
	return messages
}

type countingPatchClient struct {
	client.Client
	patches int
	names   []string
}

func (c *countingPatchClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	c.patches++
	c.names = append(c.names, obj.GetNamespace()+"/"+obj.GetName())
	return c.Client.Patch(ctx, obj, patch, opts...)
}
