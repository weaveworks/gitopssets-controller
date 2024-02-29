package gitrepository

import (
	"context"
	"fmt"
	"time"

	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"
	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/controllers/templates/generators"
	"github.com/gitops-tools/gitopssets-controller/pkg/parser"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GitRepositoryGenerator extracts files from Flux GitRepository resources.
type GitRepositoryGenerator struct {
	Client client.Reader
	logr.Logger

	Fetcher parser.ArchiveFetcher
}

// GeneratorFactory is a function for creating per-reconciliation generators for
// the GitRepositoryGenerator.
func GeneratorFactory(fetcher parser.ArchiveFetcher) generators.GeneratorFactory {
	return func(l logr.Logger, c client.Reader) generators.Generator {
		return NewGenerator(l, c, fetcher)
	}
}

// NewGenerator creates and returns a new GitRepository generator.
func NewGenerator(l logr.Logger, c client.Reader, fetcher parser.ArchiveFetcher) *GitRepositoryGenerator {
	return &GitRepositoryGenerator{
		Client:  c,
		Logger:  l,
		Fetcher: fetcher,
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
	repo, err := g.loadGitRepository(ctx, sg.GitRepository, ks)
	if err != nil {
		return nil, err
	}

	g.Logger.Info("fetching archive URL", "repoURL", repo.Spec.URL, "artifactURL", repo.Status.Artifact.URL,
		"digest", repo.Status.Artifact.Digest, "revision", repo.Status.Artifact.Revision)

	parser := parser.NewRepositoryParser(g.Logger, g.Fetcher)

	return parser.GenerateFromFiles(ctx, repo.Status.Artifact.URL, repo.Status.Artifact.Digest, sg.GitRepository.Files)
}

func (g *GitRepositoryGenerator) generateParamsFromGitDirectories(ctx context.Context, sg *templatesv1.GitOpsSetGenerator, ks *templatesv1.GitOpsSet) ([]map[string]any, error) {
	repo, err := g.loadGitRepository(ctx, sg.GitRepository, ks)
	if err != nil {
		return nil, err
	}

	g.Logger.Info("fetching archive URL", "repoURL", repo.Spec.URL, "artifactURL", repo.Status.Artifact.URL,
		"digest", repo.Status.Artifact.Digest, "revision", repo.Status.Artifact.Revision)

	parser := parser.NewRepositoryParser(g.Logger, g.Fetcher)

	return parser.GenerateFromDirectories(ctx, repo.Status.Artifact.URL, repo.Status.Artifact.Digest, sg.GitRepository.Directories)
}

// Interval is an implementation of the Generator interface.
//
// GitRepositoryGenerator is driven by watching a Flux GitRepository resource.
func (g *GitRepositoryGenerator) Interval(sg *templatesv1.GitOpsSetGenerator) time.Duration {
	return generators.NoRequeueInterval
}

func (g *GitRepositoryGenerator) loadGitRepository(ctx context.Context, gen *templatesv1.GitRepositoryGenerator, ks *templatesv1.GitOpsSet) (*sourcev1.GitRepository, error) {
	repoName := client.ObjectKey{Name: gen.RepositoryRef, Namespace: ks.GetNamespace()}

	var gr sourcev1.GitRepository
	if err := g.Client.Get(ctx, repoName, &gr); err != nil {
		return nil, fmt.Errorf("could not load GitRepository: %w", err)
	}

	// No artifact? nothing to generate...
	if gr.Status.Artifact == nil {
		g.Logger.Info("GitRepository does not have an artifact", "repository", repoName)
		return nil, generators.ArtifactError("GitRepository", repoName)
	}

	return &gr, nil
}
