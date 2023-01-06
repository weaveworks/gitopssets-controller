/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// // ©itOpsSetTemplate describes a resource to create
type GitOpsSetTemplate struct {
	runtime.RawExtension `json:",inline"`
}

// ListGenerator generates from a hard-coded list.
type ListGenerator struct {
	Elements []apiextensionsv1.JSON `json:"elements"`
}

// GitRepositoryGeneratorDirectoryItem defines a path to be parsed (or excluded from) for
// files.
type GitRepositoryGeneratorDirectoryItem struct {
	Path    string `json:"path"`
	Exclude bool   `json:"exclude,omitempty"`
}

// GitRepositoryGenerator generates from files in a Flux GitRepository resource.
type GitRepositoryGenerator struct {
	// RepositoryRef is the name of a GitRepository resource to be generated from.
	RepositoryRef string `json:"repositoryRef"`

	// Directories is a set of rules for identifying directories to be parsed.
	Directories []GitRepositoryGeneratorDirectoryItem `json:"directories,omitempty"`
}

// GitOpsSet describes the configured generators.
type GitOpsSetGenerator struct {
	List          *ListGenerator          `json:"list,omitempty"`
	GitRepository *GitRepositoryGenerator `json:"gitRepository,omitempty"`
}

// GitOpsSetSpec defines the desired state of GitOpsSet
type GitOpsSetSpec struct {
	Generators []GitOpsSetGenerator `json:"generators,omit"`
	Template   GitOpsSetTemplate    `json:"template"`
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
