package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// // ©itOpsSetTemplate describes a resource to create
type GitOpsSetTemplate struct {
	// Repeat is a JSONPath string defining that the template content should be
	// repeated for each of the matching elements in the JSONPath expression.
	// https://kubernetes.io/docs/reference/kubectl/jsonpath/
	Repeat string `json:"repeat,omitempty"`
	// Content is the YAML to be templated and generated.
	Content runtime.RawExtension `json:"content"`
}

// ListGenerator generates from a hard-coded list.
type ListGenerator struct {
	Elements []apiextensionsv1.JSON `json:"elements,omitempty"`
}

// GitRepositoryGeneratorFileItemm defines a path to a file to be parsed when generating.
type GitRepositoryGeneratorFileItem struct {
	// Path is the name of a file to read and generate from can be JSON or YAML.
	Path string `json:"path"`
}

// GitRepositoryGenerator generates from files in a Flux GitRepository resource.
type GitRepositoryGenerator struct {
	// RepositoryRef is the name of a GitRepository resource to be generated from.
	RepositoryRef string `json:"repositoryRef,omitempty"`

	// Files is a set of rules for identifying files to be parsed.
	Files []GitRepositoryGeneratorFileItem `json:"files,omitempty"`
}

// GitOpsSet describes the configured generators.
type GitOpsSetGenerator struct {
	List          *ListGenerator          `json:"list,omitempty"`
	GitRepository *GitRepositoryGenerator `json:"gitRepository,omitempty"`
}

// GitOpsSetSpec defines the desired state of GitOpsSet
type GitOpsSetSpec struct {
	// Suspend tells the controller to suspend the reconciliation of this
	// GitOpsSet.
	// +optional
	Suspend bool `json:"suspend,omitempty"`

	// Generators generate the data to be inserted into the provided templates.
	Generators []GitOpsSetGenerator `json:"generators,omitempty"`

	// Templates are a set of YAML templates that are rendered into resources
	// from the data supplied by the generators.
	Templates []GitOpsSetTemplate `json:"templates,omitempty"`
}

// GitOpsSetStatus defines the observed state of GitOpsSet
type GitOpsSetStatus struct {
	// ObservedGeneration is the last observed generation of the HelmRepository
	// object.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions holds the conditions for the GitOpsSet
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Inventory contains the list of Kubernetes resource object references that
	// have been successfully applied
	// +optional
	Inventory *ResourceInventory `json:"inventory,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description=""
//+kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description=""
//+kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].message",description=""

// GitOpsSet is the Schema for the gitopssets API
type GitOpsSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GitOpsSetSpec   `json:"spec,omitempty"`
	Status GitOpsSetStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// GitOpsSetList contains a list of GitOpsSet
type GitOpsSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GitOpsSet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GitOpsSet{}, &GitOpsSetList{})
}
