package templates

import (
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
)

func TestIsNamespacedObject(t *testing.T) {
	nsTests := []struct {
		obj  runtime.Object
		want bool
	}{
		{
			obj:  makeTestNamespace("testing"),
			want: false,
		},
		{
			obj:  makeTestService(nsn("demo", "engineering-dev-demo1")),
			want: true,
		},
	}

	for _, tt := range nsTests {
		t.Run(fmt.Sprintf("Kind %s", kind(tt.obj)), func(t *testing.T) {
			if is := IsNamespacedObject(tt.obj); is != tt.want {
				t.Fatalf("IsNamespacedObject(%s) got %v, want %v", kind(tt.obj), is, tt.want)
			}
		})
	}
}
