package v1alpha1

import (
	"github.com/fluxcd/pkg/apis/meta"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// GitOpsSetFinalizer is the finalizer added to GitOpsSets to allow us to clean
// up resources.
const GitOpsSetFinalizer = "finalizers.sets.gitops.pro"

const (
	// DeletionPolicyDelete removes rendered resources when they leave the set
	// and when the GitOpsSet itself is deleted.
	DeletionPolicyDelete = "Delete"
	// DeletionPolicyOrphan leaves rendered resources in the cluster and drops
	// them from the inventory.
	DeletionPolicyOrphan = "Orphan"
)

// LocalObjectReference contains enough information to locate the referenced Kubernetes resource object.
type LocalObjectReference struct {
	// Name of the referent.
	// +required
	Name string `json:"name"`
}

// GitOpsSetTemplate describes a resource to create.
// Content is required. The API schema cannot measure the size of the raw
// object, so an explicit empty object is rejected by the controller render.
type GitOpsSetTemplate struct {
	// Name identifies this template so another template can require it.
	// Optional unless another template lists it in requires. Names are unique
	// within the GitOpsSet.
	// +optional
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`

	// Requires lists template names. For the same generated element, objects
	// from this template are not applied for the first time until every object
	// rendered from those templates exists and is ready. Objects already in the
	// inventory are still applied. Readiness is the same check the Healthy
	// condition uses. A ConfigMap has no Ready condition, so it never satisfies
	// requires.
	// +optional
	// +kubebuilder:validation:MaxItems=16
	Requires []string `json:"requires,omitempty"`

	// DeletionPolicy overrides spec.deletionPolicy for objects rendered from
	// this template. Empty means spec.deletionPolicy, and an empty spec value
	// means Delete.
	// +kubebuilder:validation:Enum=Delete;Orphan
	// +optional
	DeletionPolicy string `json:"deletionPolicy,omitempty"`

	// ServiceAccountName is the account to impersonate when applying, deleting,
	// and reading this template's objects. Empty means spec.serviceAccountName.
	// Generators keep using spec.serviceAccountName.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

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
	SecretRef *LocalObjectReference `json:"secretRef,omitempty"`

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
	SecretRef *LocalObjectReference `json:"secretRef,omitempty"`

	// AllowClusterNetwork permits loopback and in-cluster DNS names such as
	// *.svc and *.cluster.local. Link-local, unspecified, and multicast
	// addresses are refused even when this is true.
	// +optional
	AllowClusterNetwork bool `json:"allowClusterNetwork,omitempty"`
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
	// +kubebuilder:validation:MaxItems=16
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
// +kubebuilder:validation:XValidation:rule="(has(self.list) ? 1 : 0) + (has(self.pullRequests) ? 1 : 0) + (has(self.gitRepository) ? 1 : 0) + (has(self.ociRepository) ? 1 : 0) + (has(self.cluster) ? 1 : 0) + (has(self.apiClient) ? 1 : 0) + (has(self.imagePolicy) ? 1 : 0) + (has(self.config) ? 1 : 0) == 1",message="exactly one generator type must be set"
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
// +kubebuilder:validation:XValidation:rule="(has(self.list) ? 1 : 0) + (has(self.pullRequests) ? 1 : 0) + (has(self.gitRepository) ? 1 : 0) + (has(self.ociRepository) ? 1 : 0) + (has(self.matrix) ? 1 : 0) + (has(self.cluster) ? 1 : 0) + (has(self.apiClient) ? 1 : 0) + (has(self.imagePolicy) ? 1 : 0) + (has(self.config) ? 1 : 0) == 1",message="exactly one generator type must be set"
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

	// Filter is a CEL expression evaluated against each generated element.
	// The element map is available as the variable `element`. The expression
	// must return a boolean. An empty filter keeps every element. For a matrix
	// generator the filter runs after the cartesian product.
	// +optional
	Filter string `json:"filter,omitempty"`
}

// Rollout controls how many rendered objects are applied per reconcile.
type Rollout struct {
	// MaxResources is the maximum number of new objects to apply in one reconcile.
	// Objects already in the inventory are reapplied and do not consume this budget.
	// Zero, or an omitted rollout, applies every rendered object.
	// +kubebuilder:validation:Minimum=0
	// +optional
	MaxResources *int `json:"maxResources,omitempty"`
}

// GitOpsSetSpec defines the desired state of GitOpsSet
type GitOpsSetSpec struct {
	// Suspend tells the controller to suspend the reconciliation of this
	// GitOpsSet.
	// +optional
	Suspend bool `json:"suspend,omitempty"`

	// DeletionPolicy controls whether rendered resources are deleted when they
	// are no longer produced, and when the GitOpsSet is deleted.
	//
	// Delete removes those resources. Orphan leaves them in the cluster and
	// removes them from the inventory. An empty value means Delete.
	// +kubebuilder:validation:Enum=Delete;Orphan
	// +kubebuilder:default=Delete
	// +optional
	DeletionPolicy string `json:"deletionPolicy,omitempty"`

	// Generators generate the data to be inserted into the provided templates.
	// +required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=32
	Generators []GitOpsSetGenerator `json:"generators"`

	// Templates are a set of YAML templates that are rendered into resources
	// from the data supplied by the generators.
	// +required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=32
	Templates []GitOpsSetTemplate `json:"templates"`

	// Rollout limits how many rendered objects are applied in one reconcile.
	// When omitted, every rendered object is applied.
	// +optional
	Rollout *Rollout `json:"rollout,omitempty"`

	// The name of the Kubernetes service account to impersonate
	// when reconciling this Kustomization.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// HealthCheck controls whether applied resources are checked for readiness.
	// Checks are enabled when this is omitted.
	// +optional
	HealthCheck *HealthCheck `json:"healthCheck,omitempty"`

	// Force acquires ownership of conflicting fields during server-side apply.
	// Without this, a field owned by another manager is left unchanged and the
	// apply returns a conflict.
	// +optional
	Force bool `json:"force,omitempty"`
}

// HealthCheck configures the Healthy condition.
type HealthCheck struct {
	// Enabled turns health checks on or off. Nil means enabled.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Kinds replaces the default health check kinds when non-empty.
	// The default is Kustomization, HelmRelease, Deployment, DaemonSet, and
	// StatefulSet. Entries are Kubernetes kinds, for example Kustomization or
	// RuntimeEnvironment.
	// +optional
	// +kubebuilder:validation:MaxItems=16
	Kinds []string `json:"kinds,omitempty"`
}

// HealthChecksEnabled reports whether this set should check resource health.
func (gs *GitOpsSet) HealthChecksEnabled() bool {
	if gs.Spec.HealthCheck == nil || gs.Spec.HealthCheck.Enabled == nil {
		return true
	}
	return *gs.Spec.HealthCheck.Enabled
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

	// LastAppliedSources records the source digests and resource versions used
	// for the last successful apply.
	// +optional
	LastAppliedSources []AppliedSource `json:"lastAppliedSources,omitempty"`
}

// AppliedSource identifies a source input that was current when resources were applied.
type AppliedSource struct {
	// Kind is GitRepository, OCIRepository, Secret, or ConfigMap.
	Kind string `json:"kind"`
	// Name is the source object name.
	Name string `json:"name"`
	// Digest is the artifact digest for a GitRepository or OCIRepository.
	// +optional
	Digest string `json:"digest,omitempty"`
	// Revision is the artifact revision for a GitRepository or OCIRepository.
	// +optional
	Revision string `json:"revision,omitempty"`
	// ResourceVersion is the Secret or ConfigMap resourceVersion.
	// +optional
	ResourceVersion string `json:"resourceVersion,omitempty"`
}

//+genclient
//+genclient:Namespaced
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:shortName="gs"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description=""
//+kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description=""
//+kubebuilder:printcolumn:name="Healthy",type="string",JSONPath=".status.conditions[?(@.type==\"Healthy\")].status",description=""
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
