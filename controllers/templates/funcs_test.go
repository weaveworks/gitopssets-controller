package templates

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
)

func TestTemplateFunctionsAreStable(t *testing.T) {
	banned := []string{
		"uuidv4",
		"now",
		"date",
		"dateInZone",
		"dateModify",
		"ago",
		"unixEpoch",
		"randNumeric",
		"randAlpha",
		"randAlphaNum",
		"randAscii",
		"genCA",
		"genSelfSignedCert",
		"genSignedCert",
		"genPrivateKey",
		"bcrypt",
		"htpasswd",
		"env",
		"expandenv",
		"getHostByName",
	}
	for _, name := range banned {
		if _, ok := templateFuncs[name]; ok {
			t.Errorf("template function %s is available", name)
		}
	}

	gs := templatesv1.GitOpsSet{ObjectMeta: metav1.ObjectMeta{Name: "set", Namespace: "demo"}}
	params := map[string]any{"Element": map[string]any{"name": "web"}}
	first, err := render([]byte("name: {{ .Element.name }}"), params, gs)
	if err != nil {
		t.Fatal(err)
	}
	second, err := render([]byte("name: {{ .Element.name }}"), params, gs)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("render changed between calls:\n%s\n%s", first, second)
	}
}
