package ocirepository

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

// OCIRepositoryGenerator extracts files from Flux OCIRepository resources.
type OCIRepositoryGenerator struct {
	Client client.Reader
	logr.Logger

	Fetcher parser.ArchiveFetcher
}

// GeneratorFactory is a function for creating per-reconciliation generators for
// the OCIRepositoryGenerator.
func GeneratorFactory(fetcher parser.ArchiveFetcher) generators.GeneratorFactory {
	return func(l logr.Logger, c client.Reader) generators.Generator {
		return NewGenerator(l, c, fetcher)
	}
}

// NewGenerator creates and returns a new OCIRepository generator.
func NewGenerator(l logr.Logger, c client.Reader, fetcher parser.ArchiveFetcher) *OCIRepositoryGenerator {
	return &OCIRepositoryGenerator{
		Client:  c,
		Logger:  l,
		Fetcher: fetcher,
	}
}

// Generate is an implementation of the Generator interface.
//
// If the OCIRepository generator generates from a list of files, each file is
// parsed and returned as a generated element.
func (g *OCIRepositoryGenerator) Generate(ctx context.Context, sg *templatesv1.GitOpsSetGenerator, ks *templatesv1.GitOpsSet) ([]map[string]any, error) {
	if sg == nil {
		return nil, generators.ErrEmptyGitOpsSet
	}
	if sg.OCIRepository == nil {
		return nil, nil
	}

	g.Logger.Info("generating params from OCIRepository generator", "repo", sg.OCIRepository.RepositoryRef)

	if sg.OCIRepository.Files != nil {
		return g.generateParamsFromOCIFiles(ctx, sg, ks)
	}

	if sg.OCIRepository.Directories != nil {
		return g.generateParamsFromOCIDirectories(ctx, sg, ks)
	}

	return nil, generators.ErrEmptyGitOpsSet
}

func (g *OCIRepositoryGenerator) generateParamsFromOCIFiles(ctx context.Context, sg *templatesv1.GitOpsSetGenerator, ks *templatesv1.GitOpsSet) ([]map[string]any, error) {
	repo, err := g.loadOCIRepository(ctx, sg.OCIRepository, ks)
	if err != nil {
		return nil, err
	}

	g.Logger.Info("fetching archive URL", "repoURL", repo.Spec.URL, "artifactURL", repo.Status.Artifact.URL,
		"digest", repo.Status.Artifact.Digest, "revision", repo.Status.Artifact.Revision)

	parser := parser.NewRepositoryParser(g.Logger, g.Fetcher)

	return parser.GenerateFromFiles(ctx, repo.Status.Artifact.URL, repo.Status.Artifact.Digest, sg.OCIRepository.Files)
}

func (g *OCIRepositoryGenerator) generateParamsFromOCIDirectories(ctx context.Context, sg *templatesv1.GitOpsSetGenerator, ks *templatesv1.GitOpsSet) ([]map[string]any, error) {
	repo, err := g.loadOCIRepository(ctx, sg.OCIRepository, ks)
	if err != nil {
		return nil, err
	}

	g.Logger.Info("fetching archive URL", "repoURL", repo.Spec.URL, "artifactURL", repo.Status.Artifact.URL,
		"digest", repo.Status.Artifact.Digest, "revision", repo.Status.Artifact.Revision)

	parser := parser.NewRepositoryParser(g.Logger, g.Fetcher)

	return parser.GenerateFromDirectories(ctx, repo.Status.Artifact.URL, repo.Status.Artifact.Digest, sg.OCIRepository.Directories)
}

func (g *OCIRepositoryGenerator) loadOCIRepository(ctx context.Context, gen *templatesv1.OCIRepositoryGenerator, ks *templatesv1.GitOpsSet) (*sourcev1.OCIRepository, error) {
	repoName := client.ObjectKey{Name: gen.RepositoryRef, Namespace: ks.GetNamespace()}

	var or sourcev1.OCIRepository
	if err := g.Client.Get(ctx, repoName, &or); err != nil {
		return nil, fmt.Errorf("could not load OCIRepository: %w", err)
	}

	// No artifact? nothing to generate...
	if or.Status.Artifact == nil {
		g.Logger.Info("OCIRepository does not have an artifact", "repository", repoName)
		return nil, generators.ArtifactError("OCIRepository", repoName)
	}

	return &or, nil
}

// Interval is an implementation of the Generator interface.
//
// OCIRepositoryGenerator is driven by watching a Flux OCIRepository resource.
func (g *OCIRepositoryGenerator) Interval(sg *templatesv1.GitOpsSetGenerator) time.Duration {
	return generators.NoRequeueInterval
}
