package templates

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
)

func TestToYaml(t *testing.T) {
	fn := templateFuncs["toYaml"].(func(any) (string, error))
	got, err := fn(map[string]any{"name": "web", "count": 2})
	if err != nil {
		t.Fatal(err)
	}
	want := "count: 2\nname: web"
	if got != want {
		t.Fatalf("toYaml() = %q, want %q", got, want)
	}

	gs := templatesv1.GitOpsSet{ObjectMeta: metav1.ObjectMeta{Name: "set", Namespace: "demo"}}
	_, err = render([]byte("value: {{ toYaml .Bad }}"), map[string]any{"Bad": make(chan int)}, gs)
	if err == nil || !strings.Contains(err.Error(), "failed to render template") {
		t.Fatalf("error = %v, want a render error from toYaml", err)
	}
}
