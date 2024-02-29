package v1alpha1

import (
	"github.com/fluxcd/pkg/apis/meta"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// GitOpsSetFinalizer is the finalizer added to GitOpsSets to allow us to clean
// up resources.
const GitOpsSetFinalizer = "finalizers.sets.gitops.pro"

// GitOpsSetTemplate describes a resource to create
type GitOpsSetTemplate struct {
	// Repeat is a JSONPath string defining that the template content should be
	// repeated for each of the matching elements in the JSONPath expression.
	// https://kubernetes.io/docs/reference/kubectl/jsonpath/
	Repeat string `json:"repeat,omitempty"`
	// Content is the YAML to be templated and generated.
	Content runtime.RawExtension `json:"content"`
}

// ClusterGenerator defines a generator that queries the cluster API for
// relevant clusters.
type ClusterGenerator struct {
	// Selector is used to filter the clusters that you want to target.
	//
	// If no selector is provided, no clusters will be matched.
	// +optional
	Selector metav1.LabelSelector `json:"selector,omitempty"`
}

// ConfigGenerator loads a referenced ConfigMap or
// Secret from the Cluster and makes it available as a resource.
type ConfigGenerator struct {
	// Kind of the referent.
	// +kubebuilder:validation:Enum=ConfigMap;Secret
	// +required
	Kind string `json:"kind"`

	// Name of the referent.
	// +required
	Name string `json:"name"`
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

	// Fork is used to filter out forks from the target PRs if false,
	// or to include forks if  true
	// +optional
	Forks bool `json:"forks,omitempty"`
}

// APIClientGenerator defines a generator that queries an API endpoint and uses
// that to generate data.
type APIClientGenerator struct {
	// The interval at which to poll the API endpoint.
	// +required
	Interval metav1.Duration `json:"interval"`

	// This is the API endpoint to use.
	// +kubebuilder:validation:Pattern="^(http|https)://"
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// Method defines the HTTP method to use to talk to the endpoint.
	// +kubebuilder:default="GET"
	// +kubebuilder:validation:Enum=GET;POST
	Method string `json:"method,omitempty"`

	// JSONPath is string that is used to modify the result of the API
	// call.
	//
	// This can be used to extract a repeating element from a response.
	// https://kubernetes.io/docs/reference/kubectl/jsonpath/
	JSONPath string `json:"jsonPath,omitempty"`

	// HeadersRef allows optional configuration of a Secret or ConfigMap to add
	// additional headers to an outgoing request.
	//
	// For example, a Secret with a key Authorization: Bearer abc123 could be
	// used to configure an authorization header.
	//
	// +optional
	HeadersRef *HeadersReference `json:"headersRef,omitempty"`

	// Body is set as the body in a POST request.
	//
	// If set, this will configure the Method to be POST automatically.
	// +optional
	Body *apiextensionsv1.JSON `json:"body,omitempty"`

	// SingleElement means generate a single element with the result of the API
	// call.
	//
	// When true, the response must be a JSON object and will be returned as a
	// single element, i.e. only one element will be generated containing the
	// entire object.
	//
	// +optional
	SingleElement bool `json:"singleElement,omitempty"`

	// Reference to Secret in same namespace with a field "caFile" which
	// provides the Certificate Authority to trust when making API calls.
	SecretRef *corev1.LocalObjectReference `json:"secretRef,omitempty"`
}

// HeadersReference references either a Secret or ConfigMap to be used for
// additional request headers.
type HeadersReference struct {
	// The resource kind to get headers from.
	// +kubebuilder:validation:Enum=Secret;ConfigMap
	Kind string `json:"kind"`
	// Name of the resource in the same namespace to apply headers from.
	Name string `json:"name"`
}

// RepositoryGeneratorFileItem defines a path to a file to be parsed when generating.
type RepositoryGeneratorFileItem struct {
	// Path is the name of a file to read and generate from can be JSON or YAML.
	Path string `json:"path"`
}

// RepositoryGeneratorDirectoryItem stores the information about a specific
// directory to be generated from.
type RepositoryGeneratorDirectoryItem struct {
	Path    string `json:"path"`
	Exclude bool   `json:"exclude,omitempty"`
}

// GitRepositoryGenerator generates from files in a Flux GitRepository resource.
type GitRepositoryGenerator struct {
	// RepositoryRef is the name of a GitRepository resource to be generated from.
	RepositoryRef string `json:"repositoryRef,omitempty"`

	// Files is a set of rules for identifying files to be parsed.
	Files []RepositoryGeneratorFileItem `json:"files,omitempty"`

	// Directories is a set of rules for identifying directories to be
	// generated.
	Directories []RepositoryGeneratorDirectoryItem `json:"directories,omitempty"`
}

// OCIRepositoryGenerator generates from files in a Flux OCIRepository resource.
type OCIRepositoryGenerator struct {
	// RepositoryRef is the name of a OCIRepository resource to be generated from.
	RepositoryRef string `json:"repositoryRef,omitempty"`

	// Files is a set of rules for identifying files to be parsed.
	Files []RepositoryGeneratorFileItem `json:"files,omitempty"`

	// Directories is a set of rules for identifying directories to be
	// generated.
	Directories []RepositoryGeneratorDirectoryItem `json:"directories,omitempty"`
}

// MatrixGenerator defines a matrix that combines generators.
// The matrix is a cartesian product of the generators.
type MatrixGenerator struct {
	// Generators is a list of generators to be combined.
	Generators []GitOpsSetNestedGenerator `json:"generators,omitempty"`

	// SingleElement means generate a single element with the result of the
	// merged generator elements.
	//
	// When true, the matrix elements will be merged to a single element, with
	// whatever prefixes they have.
	// It's recommended that you use the Name field to separate out elements.
	//
	// +optional
	SingleElement bool `json:"singleElement,omitempty"`
}

// GitOpsSetNestedGenerator describes the generators usable by the MatrixGenerator.
// This is a subset of the generators allowed by the GitOpsSetGenerator because the CRD format doesn't support recursive declarations.
type GitOpsSetNestedGenerator struct {
	// Name is an optional field that will be used to prefix the values generated
	// by the nested generators, this allows multiple generators of the same
	// type in a single Matrix generator.
	// +optional
	Name string `json:"name,omitempty"`

	List          *ListGenerator          `json:"list,omitempty"`
	GitRepository *GitRepositoryGenerator `json:"gitRepository,omitempty"`
	OCIRepository *OCIRepositoryGenerator `json:"ociRepository,omitempty"`
	PullRequests  *PullRequestGenerator   `json:"pullRequests,omitempty"`
	Cluster       *ClusterGenerator       `json:"cluster,omitempty"`
	APIClient     *APIClientGenerator     `json:"apiClient,omitempty"`
	ImagePolicy   *ImagePolicyGenerator   `json:"imagePolicy,omitempty"`
	Config        *ConfigGenerator        `json:"config,omitempty"`
}

// ImagePolicyGenerator generates from the ImagePolicy.
type ImagePolicyGenerator struct {
	// PolicyRef is the name of a ImagePolicy resource to be generated from.
	PolicyRef string `json:"policyRef,omitempty"`
}

// GitOpsSetGenerator is the top-level set of generators for this GitOpsSet.
type GitOpsSetGenerator struct {
	List          *ListGenerator          `json:"list,omitempty"`
	PullRequests  *PullRequestGenerator   `json:"pullRequests,omitempty"`
	GitRepository *GitRepositoryGenerator `json:"gitRepository,omitempty"`
	OCIRepository *OCIRepositoryGenerator `json:"ociRepository,omitempty"`
	Matrix        *MatrixGenerator        `json:"matrix,omitempty"`
	Cluster       *ClusterGenerator       `json:"cluster,omitempty"`
	APIClient     *APIClientGenerator     `json:"apiClient,omitempty"`
	ImagePolicy   *ImagePolicyGenerator   `json:"imagePolicy,omitempty"`
	Config        *ConfigGenerator        `json:"config,omitempty"`
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

	// The name of the Kubernetes service account to impersonate
	// when reconciling this Kustomization.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`
}

// GitOpsSetStatus defines the observed state of GitOpsSet
type GitOpsSetStatus struct {
	meta.ReconcileRequestStatus `json:",inline"`

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

//+genclient
//+genclient:Namespaced
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:shortName="gs"
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

// GetConditions returns the status conditions of the object.
func (in GitOpsSet) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

// SetConditions sets the status conditions on the object.
func (in *GitOpsSet) SetConditions(conditions []metav1.Condition) {
	in.Status.Conditions = conditions
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
