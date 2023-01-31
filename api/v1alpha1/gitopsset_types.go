package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// GitOpsSetTemplate describes a resource to create
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

// PullRequestGenerator defines a generator that queries a Git hosting service
// for relevant PRs.
type PullRequestGenerator struct {
	// The interval at which to check for repository updates.
	// +required
	Interval metav1.Duration `json:"interval"`
	// TODO: Fill this out with the rest of the elements from
	// https://github.com/jenkins-x/go-scm/blob/main/scm/factory/factory.go

	// Determines which git-api protocol to use.
	// +kubebuilder:validation:Enum=github;gitlab;bitbucketserver
	Driver string `json:"driver"`
	// This is the API endpoint to use.
	// +kubebuilder:validation:Pattern="^https://"
	// +optional
	ServerURL string `json:"serverURL,omitempty"`
	// This should be the Repo you want to query.
	// e.g. my-org/my-repo
	// +required
	Repo string `json:"repo"`
	// Reference to Secret in same namespace with a field "password" which is an
	// auth token that can query the Git Provider API.
	SecretRef *corev1.LocalObjectReference `json:"secretRef,omitempty"`

	// Labels is used to filter the PRs that you want to target.
	// This may be applied on the server.
	// +optional
	Labels []string `json:"labels,omitempty"`
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

// MatrixGenerator defines a matrix that combines generators.
// The matrix is a cartesian product of the generators.
type MatrixGenerator struct {
	// Generators is a list of generators to be combined.
	Generators []GitOpsSetNestedGenerator `json:"generators,omitempty"`
}

// GitOpsSetNestedGenerator describes the generators usable by the MatrixGenerator.
// This is a subset of the generators allowed by the GitOpsSetGenerator because the CRD format doesn't support recursive declarations.
type GitOpsSetNestedGenerator struct {
	List          *ListGenerator          `json:"list,omitempty"`
	GitRepository *GitRepositoryGenerator `json:"gitRepository,omitempty"`
	PullRequests  *PullRequestGenerator   `json:"pullRequests,omitempty"`
}

// GitOpsSetGenerator is the top-level set of generators for this GitOpsSet.
type GitOpsSetGenerator struct {
	List          *ListGenerator          `json:"list,omitempty"`
	PullRequests  *PullRequestGenerator   `json:"pullRequests,omitempty"`
	GitRepository *GitRepositoryGenerator `json:"gitRepository,omitempty"`
	Matrix        *MatrixGenerator        `json:"matrix,omitempty"`
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
