# Locally developing GitOpsSets

As gitopssets generate resources directly into the cluster its important to be able to test them locally before pushing them to the cluster. This document describes how to do that.

## Prerequisites

The `gitops` cli tool is required to locally evaluate gitopssets. See installation instructions in the weave-gitops [user-guide](https://docs.gitops.weave.works/docs/installation/weave-gitops-enterprise/#7-install-the-cli).

## Locally evaluating gitopssets

Gitopssets can generate from different sources and so have different gitopssets have different requirements for local evaluation.

### List generator

Lets start with the simplest case, the list generator.

The list generator generates from a list defined in the gitopsset spec. It requires no additional input from other sources so we can evaluate it in isolation.

```yaml
apiVersion: templates.weave.works/v1alpha1
kind: GitOpsSet
metadata:
  name: dev-teams
spec:
  generators:
    - list:
        elements:
          - env: dev
            team: dev-team
          - env: staging
            team: staging-team
  templates:
    - content:
        kind: ConfigMap
        apiVersion: v1
        metadata:
          name: "{{ .Element.env }}-demo"
        data:
          team: "{{ .Element.team }}"
```

Eval with:

```bash
gitops gitopsset evaluate ./gitopsset.yaml
```

This will output the generated resources to stdout

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: dev-demo
data:
  team: dev-team
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: staging-demo
data:
  team: staging-team
```

### GitRepository generator

The gitrepository generator generates from a Flux [`GitRepository`](https://toolkit.fluxcd.io/components/source/gitrepositories/) resource. It requires the gitrepository to be present in the cluster before it can be evaluated.

Either the gitopsset's `serviceAccountName` or the gitopssets-controller **service account** must have access to the git repo CR to generate the resources. So for the user to do this locally they need to have access to the git repo CR too. **The gitopssets-controller service-account and the gitopsset's serviceAccountName are not used at all for local evaluation.**

In practice a cluster's RBAC may often be setup so that users will be able to read the GitRepo CRs. The user can then view their reconcilation status and any errors etc. However some RBAC setups may be more restrictive and so the user may not have access to the GitRepo CRs. **In this case the user cannot locally evaluate the gitopsset.**

> **Note**
>
> In the future we may add a flag to read the gitrepo files and directories from the local filesystem, but this is not currently supported. It may look like this:
>
> ```bash
> gitops gitopsset evaluate ./gitopsset.yaml --local-gitrepo ./my-repo
> ```
>
> this would avoid the requirement to have RBAC to read the GitRepo CR and would instead read the files and directories from the local filesystem.

_The two main ways to use the gitrepo generator are to read either **file** or **directory** contents from the git repository. However they both have the same requirements for local evaluation._

```yaml
apiVersion: templates.weave.works/v1alpha1
kind: GitOpsSet
metadata:
  name: repository-sample
spec:
  generators:
    - gitRepository:
        repositoryRef: go-demo-repo
        directories:
          - path: examples/kustomize/environments/*
  templates:
    - content:
        kind: Kustomization
        apiVersion: kustomize.toolkit.fluxcd.io/v1beta2
        metadata:
          name: "{{ .Element.Base }}-demo"
        spec:
          interval: 5m
          path: "{{ .Element.Directory }}"
          prune: true
          sourceRef:
            kind: GitRepository
            name: go-demo-repo
```

Make sure your local kubectl context is pointing to the cluster you want to evaluate the gitopsset on.

```bash
gitops gitopsset evaluate ./gitopsset.yaml
```

This will output the generated resources to stdout

```yaml
apiVersion: kustomize.toolkit.fluxcd.io/v1beta2
kind: Kustomization
metadata:
  name: dev-demo
spec:
  interval: 5m
  path: examples/kustomize/environments/dev
  prune: true
  sourceRef:
    kind: GitRepository
    name: go-demo-repo
---
apiVersion: kustomize.toolkit.fluxcd.io/v1beta2
kind: Kustomization
metadata:
  name: staging-demo
spec:
  interval: 5m
  path: examples/kustomize/environments/staging
  prune: true
  sourceRef:
    kind: GitRepository
    name: go-demo-repo
```

> **Warning**
> When the gitopssets-controller actually evaluates the gitopsset, it will use either the `serviceAccountName` or the gitopssets-controller **service account** to read the GitRepo CR. If the user has RBAC access to a GitRepo but the gitopssets-controller does not, then the local `gitops gitopsset evaluate` command will work, but the gitopssets-controller will fail to evaluate the gitopsset with a permission error.

### Pull request generator

The pull request generator generates from a list of pull requests from any git repository supported by go-scm.

It does not watch a Flux gitrepo CR, but instead polls the api endpoints of a "git provider" (github, gitlab, bitbucket etc) for the open Pull requests (or "merge requests" for gitlab).

Whether the git repository is private or public, the git provider api will usually require an access token to be able to access the api endpoints. The token is stored in a secret and the secret name is passed to the gitopsset via the `secretRef` field.

The user will require RBAC access to read the secret mentioned in the gitopsset.

> **Warning**
> If a user can access the secret, they can access the auth token. This means they can do anything the token can do. So if the token has access to many repositories, then the user can do anything to those repositories. You should try and create a limited access token if possible.

```yaml
apiVersion: templates.weave.works/v1alpha1
kind: GitOpsSet
metadata:
  name: pull-requests-sample
spec:
  generators:
    - pullRequests:
        interval: 5m
        driver: github
        repo: bigkevmcd/go-demo
        secretRef:
          name: github-secret
```

> **Note**
>
> In the future we may add a flag to provide the auth token via the command line, but this is not currently supported. It may look like this:
>
> ```bash
> gitops gitopsset evaluate ./gitopsset.yaml --token $GITHUB_TOKEN
> ```
>
> this would avoid the requirement to have RBAC to read the secret from the cluster

### apiClient generator

The apiClient generator generates from a list of resources returned from an api endpoint.

- If the endpoint is public then no auth is required.
- If the endpoint is private then the user will require RBAC access to read the secret mentioned in the gitopsset.

```yaml
apiVersion: templates.weave.works/v1alpha1
kind: GitOpsSet
metadata:
  name: pull-requests-sample
spec:
  generators:
    - apiClient:
        interval: 5m
        endpoint: https://api.github.com/repos/bigkevmcd/go-demo/pulls
        headersRef:
          name: github-secret
          kind: Secret
```

> **Warning**
>
> the auth headers can potentially have access to many other resources in the api. You should try and create a limited access token if possible.

> **Note**
>
> In the future we may add a flag to provide the headers via the command line, but this is not currently supported. It may look like this:
>
> ```bash
> gitops gitopsset evaluate ./gitopsset.yaml --header "Authorization: Bearer $MY_TOKEN" --header "Accept: application/vnd.github.v3+json"
> ```
>
> this would avoid the requirement to have RBAC to read the secret from the cluster
>
> We may also provide a way to access cluster services via a `--with-kube-proxy` flag, but this is not currently supported. This would allow testing of apiClient generators that access services via internal urls like `http://my-service.default.svc.cluster.local:8080`

#### Tip!

In some cases it may be easier to simulate the apiClient generator by using a list generator at first instead.

If your api endpoint returns:

```json
[
  {
    "id": 1,
    "title": "My first pull request"
  },
  {
    "id": 2,
    "title": "My second pull request"
  }
]
```

The following gitopssets will generate the same resources:

As an apiClient generator:

```yaml
apiVersion: templates.weave.works/v1alpha1
kind: GitOpsSet
metadata:
  name: pull-requests-sample
spec:
  generators:
    - apiClient:
        interval: 5m
        endpoint: https://my.api.endpoint
        headersRef:
          name: github-secret
          kind: Secret
```

As a list generator:

```yaml
apiVersion: templates.weave.works/v1alpha1
kind: GitOpsSet
metadata:
  name: pull-requests-sample
spec:
  generators:
    - list:
        elements:
          - id: 1
            title: My first pull request
          - id: 2
            title: My second pull request
```

If you want to use the `singleElement` field in the apiClient generator you can nest your expected data in a single element list. (note the extra `-` in the list elements)

```yaml
apiVersion: templates.weave.works/v1alpha1
kind: GitOpsSet
metadata:
  name: pull-requests-sample
spec:
  generators:
    - list:
        elements:
          - - id: 1
              title: My first pull request
            - id: 2
              title: My second pull request
```

### Clusters generator

The clusters generator generates from a the matching GitopsClusters in the cluster.

```yaml
apiVersion: templates.weave.works/v1alpha1
kind: GitOpsSet
metadata:
  name: cluster-sample
spec:
  generators:
    - cluster:
        selector:
          matchLabels:
            env: dev
            team: dev-team
```

In this case the user will require RBAC access to `list` the `GitopsClusters` at the cluster scope.
