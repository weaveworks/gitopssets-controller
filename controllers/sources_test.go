package controllers

import (
	"context"
	"testing"

	"github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/pkg/generators"
	"github.com/gitops-tools/gitopssets-controller/pkg/generators/config"
)

func TestReconcileSkipsUnchangedSecretAndRerendersWhenItChanges(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := templatesv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: "demo"},
		Data:       map[string][]byte{"password": []byte("first")},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(secret).Build()
	if err := cl.Get(t.Context(), types.NamespacedName{Namespace: "demo", Name: "creds"}, secret); err != nil {
		t.Fatal(err)
	}

	gs := &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{Name: "set", Namespace: "demo", Generation: 4},
		Spec: templatesv1.GitOpsSetSpec{
			Generators: []templatesv1.GitOpsSetGenerator{{
				Config: &templatesv1.ConfigGenerator{Kind: "Secret", Name: "creds"},
			}},
			Templates: []templatesv1.GitOpsSetTemplate{{
				Content: runtime.RawExtension{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"app","namespace":"demo"},"data":{"password":"{{ .Element.password }}"}}`)},
			}},
		},
	}
	templatesv1.SetGitOpsSetReadiness(gs, &templatesv1.ResourceInventory{Entries: []templatesv1.ResourceRef{{ID: "demo_app__ConfigMap", Version: "v1"}}}, metav1.ConditionTrue, templatesv1.ReconciliationSucceededReason, "1 resources created")
	gs.Status.LastAppliedSources = []templatesv1.AppliedSource{{Kind: "Secret", Name: "creds", ResourceVersion: "stale"}}

	var calls int
	reconciler := &GitOpsSetReconciler{Generators: map[string]generators.GeneratorFactory{
		"Config": func(l logr.Logger, c client.Reader) generators.Generator {
			return &countingGenerator{Generator: config.NewGenerator(l, c), calls: &calls}
		},
	}}

	inventory, _, err := reconciler.reconcileResources(t.Context(), cl, gs)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("generate calls = %d, want 1 after the secret version changed", calls)
	}
	if len(gs.Status.LastAppliedSources) != 1 || gs.Status.LastAppliedSources[0].ResourceVersion != secret.ResourceVersion {
		t.Fatalf("sources = %#v, secret version %s", gs.Status.LastAppliedSources, secret.ResourceVersion)
	}
	if len(inventory.Entries) != 1 || inventory.Entries[0].SourceRevision != "Secret/creds@"+secret.ResourceVersion {
		t.Fatalf("inventory = %#v", inventory.Entries)
	}

	gs.Status.ObservedGeneration = gs.Generation
	templatesv1.SetGitOpsSetReadiness(gs, inventory, metav1.ConditionTrue, templatesv1.ReconciliationSucceededReason, "1 resources created")
	if _, _, err := reconciler.reconcileResources(t.Context(), cl, gs); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("generate calls = %d, want the unchanged secret to skip rendering", calls)
	}
}

func TestSnapshotRecordsGitDigestAndAPIInputs(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := templatesv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := sourcev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	repo := &sourcev1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "demo"},
		Status: sourcev1.GitRepositoryStatus{Artifact: &meta.Artifact{
			Digest:   "sha256:abc",
			Revision: "main@abc",
		}},
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "ca", Namespace: "demo"}}
	headers := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "headers", Namespace: "demo"}}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(repo).WithRuntimeObjects(repo, secret, headers).Build()
	if err := cl.Status().Update(t.Context(), repo); err != nil {
		t.Fatal(err)
	}

	gs := &templatesv1.GitOpsSet{
		ObjectMeta: metav1.ObjectMeta{Name: "set", Namespace: "demo"},
		Spec: templatesv1.GitOpsSetSpec{Generators: []templatesv1.GitOpsSetGenerator{{
			GitRepository: &templatesv1.GitRepositoryGenerator{RepositoryRef: "app"},
			APIClient: &templatesv1.APIClientGenerator{
				SecretRef:  &templatesv1.LocalObjectReference{Name: "ca"},
				HeadersRef: &templatesv1.HeadersReference{Kind: "ConfigMap", Name: "headers"},
			},
		}}},
	}
	sources, complete, err := (&GitOpsSetReconciler{}).snapshotSources(t.Context(), cl, gs)
	if err != nil || !complete {
		t.Fatalf("sources=%#v complete=%v err=%v", sources, complete, err)
	}
	if len(sources) != 3 || sources[0].Digest != "sha256:abc" || sources[0].Revision != "main@abc" || sources[1].Name != "ca" || sources[2].Name != "headers" {
		t.Fatalf("sources = %#v", sources)
	}

	repo.Status.Artifact = nil
	if err := cl.Status().Update(t.Context(), repo); err != nil {
		t.Fatal(err)
	}
	_, complete, err = (&GitOpsSetReconciler{}).snapshotSources(t.Context(), cl, gs)
	if err != nil || complete {
		t.Fatalf("missing artifact complete=%v err=%v", complete, err)
	}
}

func TestCanSkipApplyRequiresSuccessfulReadyGeneration(t *testing.T) {
	sources := []templatesv1.AppliedSource{{Kind: "GitRepository", Name: "app", Digest: "sha256:abc", Revision: "main@abc"}}
	gs := &templatesv1.GitOpsSet{ObjectMeta: metav1.ObjectMeta{Generation: 2}}
	gs.Status.ObservedGeneration = 2
	gs.Status.Inventory = &templatesv1.ResourceInventory{Entries: []templatesv1.ResourceRef{{ID: "demo_app__ConfigMap", Version: "v1"}}}
	gs.Status.LastAppliedSources = sources
	templatesv1.SetGitOpsSetReadiness(gs, gs.Status.Inventory, metav1.ConditionFalse, templatesv1.ReconciliationFailedReason, "failed")
	if canSkipApply(gs, sources, true) {
		t.Fatal("a failed apply should be retried")
	}
	templatesv1.SetGitOpsSetReadiness(gs, gs.Status.Inventory, metav1.ConditionTrue, templatesv1.ReconciliationSucceededReason, "ok")
	gs.Generation = 3
	if canSkipApply(gs, sources, true) {
		t.Fatal("a spec change should be rendered")
	}
}

type countingGenerator struct {
	generators.Generator
	calls *int
}

func (g *countingGenerator) Generate(ctx context.Context, sg *templatesv1.GitOpsSetGenerator, ks *templatesv1.GitOpsSet) ([]map[string]any, error) {
	*g.calls++
	return g.Generator.Generate(ctx, sg, ks)
}
