package pullrequests

import (
	"context"
	"fmt"
	"testing"
	"time"

	templatesv1 "github.com/gitops-tools/gitopssets-controller/api/v1alpha1"
	"github.com/gitops-tools/gitopssets-controller/controllers/templates/generators"
	"github.com/gitops-tools/gitopssets-controller/test"
	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/jenkins-x/go-scm/scm"
	fakescm "github.com/jenkins-x/go-scm/scm/driver/fake"
	"github.com/jenkins-x/go-scm/scm/factory"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ generators.Generator = (*PullRequestGenerator)(nil)

func TestGenerate_with_no_generator(t *testing.T) {
	gen := GeneratorFactory(logr.Discard(), nil)
	_, err := gen.Generate(context.TODO(), nil, nil)

	if err != generators.ErrEmptyGitOpsSet {
		t.Errorf("got error %v", err)
	}
}

func TestGenerate_with_no_config(t *testing.T) {
	gen := GeneratorFactory(logr.Discard(), nil)
	got, err := gen.Generate(context.TODO(), &templatesv1.GitOpsSetGenerator{}, nil)

	if err != nil {
		t.Errorf("got an error with no pull requests: %s", err)
	}
	if got != nil {
		t.Errorf("got %v, want %v with no PullRequest generator", got, nil)
	}
}

func TestGenerate(t *testing.T) {
	testCases := []struct {
		name          string
		dataFunc      func(*fakescm.Data)
		initObjs      []runtime.Object
		secretRef     *corev1.LocalObjectReference
		labels        []string
		forks         bool
		clientFactory func(*scm.Client) clientFactoryFunc
		want          []map[string]any
	}{
		{
			name: "simple unfiltered PR",
			dataFunc: func(d *fakescm.Data) {
				d.PullRequests[1] = &scm.PullRequest{
					Number: 1,
					Base: scm.PullRequestBranch{
						Ref: "main",
						Repo: scm.Repository{
							FullName: "test-org/my-repo",
						},
					},
					Head: scm.PullRequestBranch{
						Ref: "new-topic",
						Sha: "6dcb09b5b57875f334f61aebed695e2e4193db5e",
						Repo: scm.Repository{
							CloneSSH: "git@github.com:test-org/my-repo.git",
							Clone:    "https://github.com/test-org/my-repo.git",
						},
					},
					Fork: "test-org/my-repo",
				}
			},
			forks:         false,
			clientFactory: defaultClientFactory,
			want: []map[string]any{
				{
					"Number":      "1",
					"Branch":      "new-topic",
					"HeadSHA":     "6dcb09b5b57875f334f61aebed695e2e4193db5e",
					"CloneSSHURL": "git@github.com:test-org/my-repo.git",
					"CloneURL":    "https://github.com/test-org/my-repo.git",
					"Fork":        false,
				},
			},
		},
		{
			name: "filtering by label",
			dataFunc: func(d *fakescm.Data) {
				d.PullRequests[1] = &scm.PullRequest{
					Number: 1,
					Base: scm.PullRequestBranch{
						Ref: "main",
						Repo: scm.Repository{
							FullName: "test-org/my-repo",
						},
					},
					Head: scm.PullRequestBranch{
						Ref: "old-topic",
						Sha: "564254f7170844f40a01315fc571ae45fb8665b7",
						Repo: scm.Repository{
							Clone:    "https://github.com/test-org/my-repo.git",
							CloneSSH: "git@github.com:test-org/my-repo.git",
						},
					},
					Fork: "test-org/my-repo",
				}
				d.PullRequests[2] = &scm.PullRequest{
					Number: 2,
					Base: scm.PullRequestBranch{
						Ref: "main",
						Repo: scm.Repository{
							FullName: "test-org/my-repo",
						},
					},
					Head: scm.PullRequestBranch{
						Ref: "new-topic",
						Sha: "6dcb09b5b57875f334f61aebed695e2e4193db5e",
						Repo: scm.Repository{
							Clone:    "https://github.com/test-org/my-repo.git",
							CloneSSH: "git@github.com:test-org/my-repo.git",
						},
					},
					Fork:   "test-org/my-repo",
					Labels: []*scm.Label{{Name: "testing"}},
				}
			},
			labels:        []string{"testing"},
			forks:         false,
			clientFactory: defaultClientFactory,
			want: []map[string]any{
				{
					"Number":      "2",
					"Branch":      "new-topic",
					"HeadSHA":     "6dcb09b5b57875f334f61aebed695e2e4193db5e",
					"CloneSSHURL": "git@github.com:test-org/my-repo.git",
					"CloneURL":    "https://github.com/test-org/my-repo.git",
					"Fork":        false,
				},
			},
		},
		{
			name: "generator with auth secret",
			initObjs: []runtime.Object{
				newSecret(types.NamespacedName{
					Name:      "test-secret",
					Namespace: "default",
				}),
			},
			forks: false,
			clientFactory: func(c *scm.Client) clientFactoryFunc {
				return func(_, _, auth string, opts ...factory.ClientOptionFunc) (*scm.Client, error) {
					if auth != "top-secret" {
						return nil, fmt.Errorf("got auth token %s", auth)
					}

					return c, nil
				}
			},
			dataFunc: func(d *fakescm.Data) {
				d.PullRequests[1] = &scm.PullRequest{
					Number: 1,
					Base: scm.PullRequestBranch{
						Ref: "main",
						Repo: scm.Repository{
							FullName: "test-org/my-repo",
						},
					},
					Head: scm.PullRequestBranch{
						Ref: "new-topic",
						Sha: "6dcb09b5b57875f334f61aebed695e2e4193db5e",
						Repo: scm.Repository{
							CloneSSH: "git@github.com:test-org/my-repo.git",
							Clone:    "https://github.com/test-org/my-repo.git",
						},
					},
					Fork: "test-org/my-repo",
				}
			},
			secretRef: &corev1.LocalObjectReference{
				Name: "test-secret",
			},
			want: []map[string]any{
				{
					"Number":      "1",
					"Branch":      "new-topic",
					"HeadSHA":     "6dcb09b5b57875f334f61aebed695e2e4193db5e",
					"CloneSSHURL": "git@github.com:test-org/my-repo.git",
					"CloneURL":    "https://github.com/test-org/my-repo.git",
					"Fork":        false,
				},
			},
		},
		{
			name: "filter to include if pull request is from a fork",
			dataFunc: func(d *fakescm.Data) {
				d.PullRequests[1] = &scm.PullRequest{
					Number: 1,
					Base: scm.PullRequestBranch{
						Ref: "main",
						Repo: scm.Repository{
							FullName: "test-org/my-repo",
						},
					},
					Head: scm.PullRequestBranch{
						Ref: "new-topic",
						Sha: "6dcb09b5b57875f334f61aebed695e2e4193db5e",
						Repo: scm.Repository{
							CloneSSH: "git@github.com:test-org/my-repo.git",
							Clone:    "https://github.com/test-org/my-repo.git",
						},
					},
					Fork: "test-org-2/my-repo-fork",
				}
			},
			forks:         true,
			clientFactory: defaultClientFactory,
			want: []map[string]any{
				{
					"Number":      "1",
					"Branch":      "new-topic",
					"HeadSHA":     "6dcb09b5b57875f334f61aebed695e2e4193db5e",
					"CloneSSHURL": "git@github.com:test-org/my-repo.git",
					"CloneURL":    "https://github.com/test-org/my-repo.git",
					"Fork":        true,
				},
			},
		},
		{
			name: "filter to exclude if pull request is from a fork",
			dataFunc: func(d *fakescm.Data) {
				d.PullRequests[1] = &scm.PullRequest{
					Number: 1,
					Base: scm.PullRequestBranch{
						Ref: "main",
						Repo: scm.Repository{
							FullName: "test-org/my-repo",
						},
					},
					Head: scm.PullRequestBranch{
						Ref: "old-topic",
						Sha: "564254f7170844f40a01315fc571ae45fb8665b7",
						Repo: scm.Repository{
							Clone:    "https://github.com/test-org/my-repo.git",
							CloneSSH: "git@github.com:test-org/my-repo.git",
						},
					},
					Fork: "test-org-2/my-repo-fork",
				}
				d.PullRequests[2] = &scm.PullRequest{
					Number: 2,
					Base: scm.PullRequestBranch{
						Ref: "main",
						Repo: scm.Repository{
							FullName: "test-org/my-repo",
						},
					},
					Head: scm.PullRequestBranch{
						Ref: "new-topic",
						Sha: "6dcb09b5b57875f334f61aebed695e2e4193db5e",
						Repo: scm.Repository{
							Clone:    "https://github.com/test-org/my-repo.git",
							CloneSSH: "git@github.com:test-org/my-repo.git",
						},
					},
					Fork:   "test-org/my-repo",
					Labels: []*scm.Label{{Name: "testing"}},
				}
			},
			forks:         false,
			clientFactory: defaultClientFactory,
			want: []map[string]any{
				{
					"Number":      "2",
					"Branch":      "new-topic",
					"HeadSHA":     "6dcb09b5b57875f334f61aebed695e2e4193db5e",
					"CloneSSHURL": "git@github.com:test-org/my-repo.git",
					"CloneURL":    "https://github.com/test-org/my-repo.git",
					"Fork":        false,
				},
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			gen := NewGenerator(logr.Discard(), fake.NewFakeClient(tt.initObjs...))
			client, data := fakescm.NewDefault()
			tt.dataFunc(data)
			gen.clientFactory = tt.clientFactory(client)

			gsg := templatesv1.GitOpsSetGenerator{
				PullRequests: &templatesv1.PullRequestGenerator{
					Driver:    "fake",
					ServerURL: "https://example.com",
					Repo:      "test-org/my-repo",
					SecretRef: tt.secretRef,
					Labels:    tt.labels,
					Forks:     tt.forks,
				},
			}

			got, err := gen.Generate(context.TODO(), &gsg,
				&templatesv1.GitOpsSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "demo-set",
						Namespace: "default",
					},
					Spec: templatesv1.GitOpsSetSpec{
						Generators: []templatesv1.GitOpsSetGenerator{
							gsg,
						},
					},
				})

			test.AssertNoError(t, err)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Fatalf("failed to generate pull requests:\n%s", diff)
			}
		})
	}
}

func TestGenerate_errors(t *testing.T) {
	testCases := []struct {
		name          string
		initObjs      []runtime.Object
		secretRef     *corev1.LocalObjectReference
		clientFactory func(*scm.Client) clientFactoryFunc
		wantErr       string
	}{
		{
			name:          "generator with missing secret",
			clientFactory: defaultClientFactory,
			secretRef: &corev1.LocalObjectReference{
				Name: "test-secret",
			},
			wantErr: `failed to load repository generator credentials: secrets "test-secret" not found`,
		},
		{
			name:          "generator with missing key in secret",
			clientFactory: defaultClientFactory,
			initObjs: []runtime.Object{newSecret(types.NamespacedName{
				Name:      "test-secret",
				Namespace: "default",
			}, func(c *corev1.Secret) {
				c.Data = map[string][]byte{}
			})},
			secretRef: &corev1.LocalObjectReference{
				Name: "test-secret",
			},
			wantErr: `secret default/test-secret does not contain required field 'password'`,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			gen := NewGenerator(logr.Discard(), fake.NewFakeClient(tt.initObjs...))
			client, _ := fakescm.NewDefault()
			gen.clientFactory = tt.clientFactory(client)

			gsg := templatesv1.GitOpsSetGenerator{
				PullRequests: &templatesv1.PullRequestGenerator{
					Driver:    "fake",
					ServerURL: "https://example.com",
					Repo:      "test-org/my-repo",
					SecretRef: tt.secretRef,
				},
			}

			_, err := gen.Generate(context.TODO(), &gsg,
				&templatesv1.GitOpsSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "demo-set",
						Namespace: "default",
					},
					Spec: templatesv1.GitOpsSetSpec{
						Generators: []templatesv1.GitOpsSetGenerator{
							gsg,
						},
					},
				})

			test.AssertErrorMatch(t, tt.wantErr, err)
		})
	}
}

func TestPullRequestGenerator_GetInterval(t *testing.T) {
	interval := time.Minute * 10
	gen := NewGenerator(logr.Discard(), fake.NewFakeClient())
	sg := &templatesv1.GitOpsSetGenerator{
		PullRequests: &templatesv1.PullRequestGenerator{
			Driver:    "fake",
			ServerURL: "https://example.com",
			Repo:      "test-org/my-repo",
			Interval:  metav1.Duration{Duration: interval},
		},
	}

	d := gen.Interval(sg)

	if d != interval {
		t.Fatalf("got %#v want %#v", d, interval)
	}
}

func newSecret(name types.NamespacedName, opts ...func(*corev1.Secret)) *corev1.Secret {
	s := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name.Name,
			Namespace: name.Namespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"password": []byte("top-secret"),
		},
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

func defaultClientFactory(c *scm.Client) clientFactoryFunc {
	return func(_, _, _ string, opts ...factory.ClientOptionFunc) (*scm.Client, error) {
		return c, nil
	}
}
