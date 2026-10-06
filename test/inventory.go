package test

import (
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/runtime"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
)

// AssertInventoryHasItems will ensure that each of the provided objects is
// listed in the Inventory of the provided GitOpsSet.
func AssertInventoryHasItems(t *testing.T, gs *templatesv1.GitOpsSet, objs ...runtime.Object) {
	t.Helper()
	if l := len(gs.Status.Inventory.Entries); l != len(objs) {
		t.Errorf("expected %d items, got %v", len(objs), l)
	}
	entries := []templatesv1.ResourceRef{}
	for _, obj := range objs {
		ref, err := templatesv1.ResourceRefFromObject(obj)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, ref)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ID < entries[j].ID
	})
	want := &templatesv1.ResourceInventory{Entries: entries}
	if diff := cmp.Diff(want, gs.Status.Inventory); diff != "" {
		t.Errorf("failed to get inventory:\n%s", diff)
	}
}
