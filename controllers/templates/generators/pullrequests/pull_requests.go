package pullrequests

import (
	"context"
	"fmt"
	"strconv"
	"time"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/controllers/templates/generators"
	"github.com/go-logr/logr"
	"github.com/jenkins-x/go-scm/scm"
	"github.com/jenkins-x/go-scm/scm/factory"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type clientFactoryFunc func(driver, serverURL, oauthToken string, opts ...factory.ClientOptionFunc) (*scm.Client, error)

// GeneratorFactory is a function for creating per-reconciliation generators for
// the GitRepositoryGenerator.
func GeneratorFactory(l logr.Logger, c client.Reader) generators.Generator {
	return NewGenerator(l, c)
}

// PullRequestGenerator generates from the open pull requests in a repository.
type PullRequestGenerator struct {
	Client        client.Reader
	clientFactory clientFactoryFunc
	logr.Logger
}

// NewGenerator creates and returns a new pull request generator.
func NewGenerator(l logr.Logger, c client.Reader) *PullRequestGenerator {
	return &PullRequestGenerator{
		Client:        c,
		Logger:        l,
		clientFactory: factory.NewClient,
	}
}

func (g *PullRequestGenerator) Generate(ctx context.Context, sg *templatesv1.GitOpsSetGenerator, ks *templatesv1.GitOpsSet) ([]map[string]any, error) {
	if sg == nil {
		g.Logger.Info("no generator provided")
		return nil, generators.ErrEmptyGitOpsSet
	}

	if sg.PullRequests == nil {
		g.Logger.Info("pull request configuration is nil")
		return nil, nil
	}

	g.Logger.Info("generating params from PullRequest generator", "repo", sg.PullRequests.Repo)
	authToken := ""
	if sg.PullRequests.SecretRef != nil {
		secretName := types.NamespacedName{
			Namespace: ks.GetNamespace(),
			Name:      sg.PullRequests.SecretRef.Name,
		}

		var secret corev1.Secret
		if err := g.Client.Get(ctx, secretName, &secret); err != nil {
			return nil, fmt.Errorf("failed to load repository generator credentials: %w", err)
		}
		// See https://github.com/fluxcd/source-controller/blob/main/pkg/git/options.go#L100
		// for details of the standard flux Git repository secret.
		data, ok := secret.Data["password"]
		if !ok {
			return nil, fmt.Errorf("secret %s does not contain required field 'password'", secretName)
		}

		authToken = string(data)
	}

	g.Logger.Info("querying pull requests", "repo", sg.PullRequests.Repo, "driver", sg.PullRequests.Driver, "serverURL", sg.PullRequests.ServerURL)

	scmClient, err := g.clientFactory(sg.PullRequests.Driver, sg.PullRequests.ServerURL, authToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	prs, _, err := scmClient.PullRequests.List(ctx, sg.PullRequests.Repo, listOptionsFromConfig(sg.PullRequests))
	if err != nil {
		return nil, fmt.Errorf("failed to list pull requests: %w", err)
	}

	g.Logger.Info("queried pull requests", "repo", sg.PullRequests.Repo, "count", len(prs))
	res := []map[string]any{}
	for _, pr := range prs {
		if !prMatchesLabels(pr, sg.PullRequests.Labels) {
			continue
		}

		// if Forks flag is set to false, and repo is a fork, don't include it
		isFork := sg.PullRequests.Repo != pr.Fork
		if !sg.PullRequests.Forks && isFork {
			continue
		}
		// TODO: This should provide additional fields, including the
		// destination branch ...etc.
		// It should also sanitise the branches, for example, a branch can
		// contain a `/` or do we delegate this to the `sanitize` function in
		// the template rendering?
		res = append(res, map[string]any{
			"Number":      strconv.Itoa(pr.Number),
			"Branch":      pr.Head.Ref,
			"HeadSHA":     pr.Head.Sha,
			"CloneURL":    pr.Head.Repo.Clone,
			"CloneSSHURL": pr.Head.Repo.CloneSSH,
			"Fork":        isFork,
		})
	}

	return res, nil
}

// Interval is an implementation of the Generator interface.
func (g *PullRequestGenerator) Interval(sg *templatesv1.GitOpsSetGenerator) time.Duration {
	return sg.PullRequests.Interval.Duration
}

// label filtering is only supported by GitLab (that I'm aware of)
// The fetched PRs are filtered on labels across all providers, but providing
// the labels optimises the load from GitLab.
//
// TODO: How should we apply pagination/limiting of fetched PRs?
func listOptionsFromConfig(c *templatesv1.PullRequestGenerator) *scm.PullRequestListOptions {
	return &scm.PullRequestListOptions{
		Size:   20,
		Labels: c.Labels,
		Open:   true,
	}
}

func prMatchesLabels(pr *scm.PullRequest, labels []string) bool {
	if len(labels) == 0 {
		return true
	}

	for _, v := range labels {
		if !containsString(v, pr.Labels) {
			return false
		}
	}

	return true
}

func containsString(s1 string, s2 []*scm.Label) bool {
	for _, s := range s2 {
		if s1 == s.Name {
			return true
		}
	}

	return false
}
