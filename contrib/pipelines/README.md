# Dynamic Pipelines based upon labels

Contributed by Steve Fraser `<steve.fraser@weave.works>`

This GitOpsSet uses the cluster generator to groups clusters into different environments

```yaml
  generators:
    - matrix:
        singleElement: true
        generators:
        - name: dev
          cluster:
            selector:
              matchLabels:
                env: dev
        - name: stage
          cluster:
            selector:
              matchLabels:
                env: stage
        - name: prdgroup1
          cluster:
            selector:
              matchLabels:
                env: prdgroup1
        - name: prdgroup2
          cluster:
            selector:
              matchLabels:
                env: prdgroup2
```

These clusters match on the label `env`

For example, here is a staging cluster with a label `env: stage`

```yaml
apiVersion: gitops.weave.works/v1alpha1
kind: GitopsCluster
metadata:
  generation: 1
  labels:
    env: stage
  name: cluster-1-stage
  namespace: default
spec:
  capiClusterRef:
    name: cluster-1-stage
```
The `singleElement: true` value pulls all of the generators in the matrix element into a single context

The GitOpsSet creates a kustomization and patches in each cluster

```yaml
      apiVersion: kustomize.toolkit.fluxcd.io/v1beta2
      kind: Kustomization
      metadata:
        name: 'podinfo-app-test'
      spec:
        interval: 5m
        path: './kustomize/pipeline'
        prune: true
        patches:
        - patch: |-
            apiVersion: pipelines.weave.works/v1alpha1
            kind: Pipeline
            metadata:
              name: not-used
            spec:
              environments:
              
              {{- if .Element.dev -}}
              - name: dev
                targets:
                {{ range .Element.dev }}
                - clusterRef:
                    kind: GitopsCluster
                    name: {{ .ClusterName }}
                  namespace: app-podinfo-dev
                {{ end }}
              {{- end -}}
              - name: stage
                targets:
                {{ range .Element.stage }}
                - clusterRef:
                    kind: GitopsCluster
                    name: {{ .ClusterName }}
                  namespace: app-podinfo-stage
                {{ end }}
              - name: prdgroup1
                targets:
                {{ range .Element.prdgroup1 }}
                - clusterRef:
                    kind: GitopsCluster
                    name: {{ .ClusterName }}
                  namespace: app-podinfo-prd
                {{ end }}
              - name: prdgroup2
                targets:
                {{ range .Element.prdgroup2 }}
                - clusterRef:
                    kind: GitopsCluster
                    name: {{ .ClusterName }}
                  namespace: app-podinfo-prd
                {{ end }}
          target:
            kind: Pipeline
        sourceRef:
          kind: GitRepository
          name: flux-system
```

The Kustomization applies the following manifest
```yaml
apiVersion: pipelines.weave.works/v1alpha1
kind: Pipeline
metadata:
  name: podinfo-app-test
  namespace: default
spec:
  appRef:
    apiVersion: helm.toolkit.fluxcd.io/v2beta1
    kind: HelmRelease
    name: podinfo
  promotion:
    manual: false
    strategy:
      pull-request:
        baseBranch: main
        secretRef:
          name: promotion-credentials
        type: github
        url: https://github.com/weavegitops-trials/azure-weaveworks-webinar
```