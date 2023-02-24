package gitrepository

import (
	"context"
	"fmt"
	"time"

	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/go-logr/logr"
	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators"
	git "github.com/weaveworks/gitopssets-controller/controllers/templates/generators/gitrepository/parser"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GitRepositoryGenerator extracts files from Flux GitRepository resources.
type GitRepositoryGenerator struct {
	client.Client
	logr.Logger
}

// GeneratorFactory is a function for creating per-reconciliation generators for
// the GitRepositoryGenerator.
func GeneratorFactory(l logr.Logger, c client.Client) generators.Generator {
	return NewGenerator(l, c)
}

// NewGenerator creates and returns a new GitRepository generator.
func NewGenerator(l logr.Logger, c client.Client) *GitRepositoryGenerator {
	return &GitRepositoryGenerator{
		Client: c,
		Logger: l,
	}
}

// Generate is an implementation of the Generator interface.
//
// If the GitRepository generator generates from a list of files, each file is
// parsed and returned as a generated element.
func (g *GitRepositoryGenerator) Generate(ctx context.Context, sg *templatesv1.GitOpsSetGenerator, ks *templatesv1.GitOpsSet) ([]map[string]any, error) {
	if sg == nil {
		return nil, generators.ErrEmptyGitOpsSet
	}
	if sg.GitRepository == nil {
		return nil, nil
	}

	g.Logger.Info("generating params from GitRepository generator", "repo", sg.GitRepository.RepositoryRef)

	if sg.GitRepository.Files != nil {
		return g.generateParamsFromGitFiles(ctx, sg, ks)
	}

	if sg.GitRepository.Directories != nil {
		return g.generateParamsFromGitDirectories(ctx, sg, ks)
	}

	return nil, generators.ErrEmptyGitOpsSet
}

func (g *GitRepositoryGenerator) generateParamsFromGitFiles(ctx context.Context, sg *templatesv1.GitOpsSetGenerator, ks *templatesv1.GitOpsSet) ([]map[string]any, error) {
	var gr sourcev1.GitRepository
	repoName := client.ObjectKey{Name: sg.GitRepository.RepositoryRef, Namespace: ks.GetNamespace()}
	if err := g.Client.Get(ctx, repoName, &gr); err != nil {
		return nil, fmt.Errorf("could not load GitRepository: %w", err)
	}

	// No artifact? nothing to generate...
	if gr.Status.Artifact == nil {
		g.Logger.Info("GitRepository does not have an artifact", "repository", repoName)
		return []map[string]any{}, nil
	}

	g.Logger.Info("fetching archive URL", "repoURL", gr.Spec.URL, "artifactURL", gr.Status.Artifact.URL,
		"checksum", gr.Status.Artifact.Checksum, "revision", gr.Status.Artifact.Revision)

	parser := git.NewRepositoryParser(g.Logger)

	return parser.GenerateFromFiles(ctx, gr.Status.Artifact.URL, gr.Status.Artifact.Checksum, sg.GitRepository.Files)
}

func (g *GitRepositoryGenerator) generateParamsFromGitDirectories(ctx context.Context, sg *templatesv1.GitOpsSetGenerator, ks *templatesv1.GitOpsSet) ([]map[string]any, error) {
	var gr sourcev1.GitRepository
	repoName := client.ObjectKey{Name: sg.GitRepository.RepositoryRef, Namespace: ks.GetNamespace()}
	if err := g.Client.Get(ctx, repoName, &gr); err != nil {
		return nil, fmt.Errorf("could not load GitRepository: %w", err)
	}

	// No artifact? nothing to generate...
	if gr.Status.Artifact == nil {
		g.Logger.Info("GitRepository does not have an artifact", "repository", repoName)
		return []map[string]any{}, nil
	}

	g.Logger.Info("fetching archive URL", "repoURL", gr.Spec.URL, "artifactURL", gr.Status.Artifact.URL,
		"checksum", gr.Status.Artifact.Checksum, "revision", gr.Status.Artifact.Revision)

	parser := git.NewRepositoryParser(g.Logger)

	return parser.GenerateFromDirectories(ctx, gr.Status.Artifact.URL, gr.Status.Artifact.Checksum, sg.GitRepository.Directories)
}

// Interval is an implementation of the Generator interface.
//
// GitRepositoryGenerator is driven by watching a Flux GitRepository resource.
func (g *GitRepositoryGenerator) Interval(sg *templatesv1.GitOpsSetGenerator) time.Duration {
	return generators.NoRequeueInterval
}
