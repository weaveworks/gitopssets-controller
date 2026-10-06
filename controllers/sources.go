package controllers

import (
	"context"
	"fmt"
	"strings"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
)

func (r *GitOpsSetReconciler) snapshotSources(ctx context.Context, k8sClient client.Client, gitOpsSet *templatesv1.GitOpsSet) ([]templatesv1.AppliedSource, bool, error) {
	var sources []templatesv1.AppliedSource
	complete := true
	record := func(source templatesv1.AppliedSource, sourceComplete bool, err error) error {
		if err != nil {
			return err
		}
		if source.Name == "" {
			return nil
		}
		sources = append(sources, source)
		if !sourceComplete {
			complete = false
		}
		return nil
	}

	for _, gen := range gitOpsSet.Spec.Generators {
		if err := record(r.snapshotGit(ctx, k8sClient, gitOpsSet.Namespace, gen.GitRepository)); err != nil {
			return nil, false, err
		}
		if err := record(r.snapshotOCI(ctx, k8sClient, gitOpsSet.Namespace, gen.OCIRepository)); err != nil {
			return nil, false, err
		}
		if err := record(r.snapshotPullRequest(ctx, k8sClient, gitOpsSet.Namespace, gen.PullRequests)); err != nil {
			return nil, false, err
		}
		if err := r.recordAPISources(ctx, k8sClient, gitOpsSet.Namespace, gen.APIClient, record); err != nil {
			return nil, false, err
		}
		if err := record(r.snapshotConfig(ctx, k8sClient, gitOpsSet.Namespace, gen.Config)); err != nil {
			return nil, false, err
		}
		if gen.Matrix == nil {
			continue
		}
		for _, nested := range gen.Matrix.Generators {
			if err := record(r.snapshotGit(ctx, k8sClient, gitOpsSet.Namespace, nested.GitRepository)); err != nil {
				return nil, false, err
			}
			if err := record(r.snapshotOCI(ctx, k8sClient, gitOpsSet.Namespace, nested.OCIRepository)); err != nil {
				return nil, false, err
			}
			if err := record(r.snapshotPullRequest(ctx, k8sClient, gitOpsSet.Namespace, nested.PullRequests)); err != nil {
				return nil, false, err
			}
			if err := r.recordAPISources(ctx, k8sClient, gitOpsSet.Namespace, nested.APIClient, record); err != nil {
				return nil, false, err
			}
			if err := record(r.snapshotConfig(ctx, k8sClient, gitOpsSet.Namespace, nested.Config)); err != nil {
				return nil, false, err
			}
		}
	}
	return sources, complete, nil
}

func canSkipApply(gitOpsSet *templatesv1.GitOpsSet, sources []templatesv1.AppliedSource, complete bool) bool {
	if !complete || len(sources) == 0 || gitOpsSet.Status.Inventory == nil {
		return false
	}
	if gitOpsSet.Generation != gitOpsSet.Status.ObservedGeneration {
		return false
	}
	if templatesv1.GetGitOpsSetReadiness(gitOpsSet) != metav1.ConditionTrue {
		return false
	}
	return sourcesEqual(gitOpsSet.Status.LastAppliedSources, sources)
}

func sourcesEqual(left, right []templatesv1.AppliedSource) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func appliedSourceRevision(sources []templatesv1.AppliedSource) string {
	parts := make([]string, 0, len(sources))
	for _, source := range sources {
		token := source.Digest
		if token == "" {
			token = source.ResourceVersion
		}
		if token == "" {
			continue
		}
		parts = append(parts, source.Kind+"/"+source.Name+"@"+token)
	}
	return strings.Join(parts, ",")
}

func (r *GitOpsSetReconciler) snapshotGit(ctx context.Context, k8sClient client.Client, namespace string, gen *templatesv1.GitRepositoryGenerator) (templatesv1.AppliedSource, bool, error) {
	if gen == nil || gen.RepositoryRef == "" {
		return templatesv1.AppliedSource{}, true, nil
	}
	var repo sourcev1.GitRepository
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: gen.RepositoryRef}, &repo); err != nil {
		return templatesv1.AppliedSource{}, false, fmt.Errorf("failed to read GitRepository %s: %w", gen.RepositoryRef, err)
	}
	source := templatesv1.AppliedSource{Kind: "GitRepository", Name: repo.Name, ResourceVersion: repo.ResourceVersion}
	if repo.Status.Artifact == nil {
		return source, false, nil
	}
	source.Digest = repo.Status.Artifact.Digest
	source.Revision = repo.Status.Artifact.Revision
	return source, source.Digest != "", nil
}

func (r *GitOpsSetReconciler) snapshotOCI(ctx context.Context, k8sClient client.Client, namespace string, gen *templatesv1.OCIRepositoryGenerator) (templatesv1.AppliedSource, bool, error) {
	if gen == nil || gen.RepositoryRef == "" {
		return templatesv1.AppliedSource{}, true, nil
	}
	var repo sourcev1.OCIRepository
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: gen.RepositoryRef}, &repo); err != nil {
		return templatesv1.AppliedSource{}, false, fmt.Errorf("failed to read OCIRepository %s: %w", gen.RepositoryRef, err)
	}
	source := templatesv1.AppliedSource{Kind: "OCIRepository", Name: repo.Name, ResourceVersion: repo.ResourceVersion}
	if repo.Status.Artifact == nil {
		return source, false, nil
	}
	source.Digest = repo.Status.Artifact.Digest
	source.Revision = repo.Status.Artifact.Revision
	return source, source.Digest != "", nil
}

func (r *GitOpsSetReconciler) snapshotPullRequest(ctx context.Context, k8sClient client.Client, namespace string, gen *templatesv1.PullRequestGenerator) (templatesv1.AppliedSource, bool, error) {
	if gen == nil || gen.SecretRef == nil {
		return templatesv1.AppliedSource{}, true, nil
	}
	return snapshotObjectVersion(ctx, k8sClient, namespace, "Secret", gen.SecretRef.Name)
}

func (r *GitOpsSetReconciler) recordAPISources(ctx context.Context, k8sClient client.Client, namespace string, gen *templatesv1.APIClientGenerator, record func(templatesv1.AppliedSource, bool, error) error) error {
	if gen == nil {
		return nil
	}
	if gen.SecretRef != nil {
		if err := record(snapshotObjectVersion(ctx, k8sClient, namespace, "Secret", gen.SecretRef.Name)); err != nil {
			return err
		}
	}
	if gen.HeadersRef != nil {
		if err := record(snapshotObjectVersion(ctx, k8sClient, namespace, gen.HeadersRef.Kind, gen.HeadersRef.Name)); err != nil {
			return err
		}
	}
	return nil
}

func (r *GitOpsSetReconciler) snapshotConfig(ctx context.Context, k8sClient client.Client, namespace string, gen *templatesv1.ConfigGenerator) (templatesv1.AppliedSource, bool, error) {
	if gen == nil {
		return templatesv1.AppliedSource{}, true, nil
	}
	return snapshotObjectVersion(ctx, k8sClient, namespace, gen.Kind, gen.Name)
}

func snapshotObjectVersion(ctx context.Context, k8sClient client.Client, namespace, kind, name string) (templatesv1.AppliedSource, bool, error) {
	var obj client.Object
	switch kind {
	case "Secret":
		obj = &corev1.Secret{}
	case "ConfigMap":
		obj = &corev1.ConfigMap{}
	default:
		return templatesv1.AppliedSource{}, false, fmt.Errorf("unsupported source kind %s", kind)
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, obj); err != nil {
		return templatesv1.AppliedSource{}, false, fmt.Errorf("failed to read %s %s: %w", kind, name, err)
	}
	return templatesv1.AppliedSource{Kind: kind, Name: name, ResourceVersion: obj.GetResourceVersion()}, true, nil
}
