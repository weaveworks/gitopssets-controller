package ocirepository

import (
	"context"
	"fmt"
	"time"

	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/go-logr/logr"
	templatesv1 "github.com/weaveworks/gitopssets-controller/api/v1alpha1"
	"github.com/weaveworks/gitopssets-controller/controllers/templates/generators"
	"github.com/weaveworks/gitopssets-controller/pkg/parser"
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
	var gr sourcev1.OCIRepository
	repoName := client.ObjectKey{Name: sg.OCIRepository.RepositoryRef, Namespace: ks.GetNamespace()}
	if err := g.Client.Get(ctx, repoName, &gr); err != nil {
		return nil, fmt.Errorf("could not load OCIRepository: %w", err)
	}

	// No artifact? nothing to generate...
	if gr.Status.Artifact == nil {
		g.Logger.Info("OCIRepository does not have an artifact", "repository", repoName)
		return []map[string]any{}, nil
	}

	g.Logger.Info("fetching archive URL", "repoURL", gr.Spec.URL, "artifactURL", gr.Status.Artifact.URL,
		"digest", gr.Status.Artifact.Digest, "revision", gr.Status.Artifact.Revision)

	parser := parser.NewRepositoryParser(g.Logger, g.Fetcher)

	return parser.GenerateFromFiles(ctx, gr.Status.Artifact.URL, gr.Status.Artifact.Digest, sg.OCIRepository.Files)
}

func (g *OCIRepositoryGenerator) generateParamsFromOCIDirectories(ctx context.Context, sg *templatesv1.GitOpsSetGenerator, ks *templatesv1.GitOpsSet) ([]map[string]any, error) {
	var gr sourcev1.OCIRepository
	repoName := client.ObjectKey{Name: sg.OCIRepository.RepositoryRef, Namespace: ks.GetNamespace()}
	if err := g.Client.Get(ctx, repoName, &gr); err != nil {
		return nil, fmt.Errorf("could not load OCIRepository: %w", err)
	}

	// No artifact? nothing to generate...
	if gr.Status.Artifact == nil {
		g.Logger.Info("OCIRepository does not have an artifact", "repository", repoName)
		return []map[string]any{}, nil
	}

	g.Logger.Info("fetching archive URL", "repoURL", gr.Spec.URL, "artifactURL", gr.Status.Artifact.URL,
		"digest", gr.Status.Artifact.Digest, "revision", gr.Status.Artifact.Revision)

	parser := parser.NewRepositoryParser(g.Logger, g.Fetcher)

	return parser.GenerateFromDirectories(ctx, gr.Status.Artifact.URL, gr.Status.Artifact.Digest, sg.OCIRepository.Directories)
}

// Interval is an implementation of the Generator interface.
//
// OCIRepositoryGenerator is driven by watching a Flux OCIRepository resource.
func (g *OCIRepositoryGenerator) Interval(sg *templatesv1.GitOpsSetGenerator) time.Duration {
	return generators.NoRequeueInterval
}
