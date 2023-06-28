# Prometheus based dynamic process scaling

Contributed by Steve Fraser `<steve.fraser@weave.works>`

This uses the APIClient generator.

```yaml
  generators:
    - apiClient:
        interval: 24h
        endpoint: http://weaveworks-prometheus-prometheus.prometheus:9090/api/v1/query?query=max%28max_over_time%28container_memory_usage_bytes%7Bcontainer%3D%22podinfod%22%2Cpod%3D~%22podinfo-.%2A%22%7D%5B30d%5D%29%29by%28container%29%2F1024
        singleElement: true
```

The `singleElement: true` part means that the result is returned as a single element for input into the template rendering.

This is [querying](https://prometheus.io/docs/prometheus/latest/querying/api/#instant-queries) an in-cluster Prometheus server.

The embedded query is `query=max(max_over_time(container_memory_usage_bytes{container="podinfod",pod=~"podinfo-.*"}[30d]))by(container)/1024`

The result would look like:

```json
{
   "status" : "success",
   "data" : {
      "resultType" : "vector",
      "result" : [
         {
            "metric" : {
               "__name__" : "max",
            },
            "value": [ 1687950455.230, "300" ]
         },
      ]
   }
}
```

It's getting the maximum amount of memory used by the `podinfo` container over a 30d period, using [max_over_time](https://prometheus.io/docs/prometheus/latest/querying/functions/#aggregation_over_time), and then divides this by 1024 to calculate the number of KiloBytes used.

It creates a `Kustomization` which deploys [podinfo](https://github.com/stefanprodan/podinfo) and applies patches to the [`Deployment`](https://github.com/stefanprodan/podinfo/blob/master/kustomize/deployment.yaml) to set the [memory limits](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/) on the Deployment.

```yaml
  templates:
    - content:
        kind: Kustomization
        apiVersion: kustomize.toolkit.fluxcd.io/v1beta2
        metadata:
          name: podinfo
          namespace: flux-system
        spec:
          patches:
          - patch: |-
              - op: replace
                path: /spec/template/spec/containers/0/resources/limits/memory
                value: {{ with index .Element.data.result 0}}{{ with index .value 1 }}{{ round (mulf . 1.20) 0 }}{{ end }}{{ end }}Ki
              - op: replace
                path: /spec/template/spec/containers/0/resources/requests/memory
                value: {{ with index .Element.data.result 0}}{{ round (index .value 1) 0 }}{{ end }}Ki
            target:
              kind: Deployment
          interval: 5m
          path: ./apps/podinfo
          targetNamespace: flux-system
          prune: true
          sourceRef:
            kind: GitRepository
            name: flux-system
            namespace: flux-system
```
The templating is digging into the returned JSON using the templating [actions](https://pkg.go.dev/text/template#hdr-Actions), `{{ with index .Element.data.result 0}}{{ with index .value 1 }}{{ round (mulf . 1.20) 0 }}{{ end }}{{ end }}`

Breaking this down, `{{ with index .Element.data.result 0 }}` this queries the first `result` from the `data` field in the response.

This `{{ with index .value 1 }}` takes the second element from the `value` key which is `[ 1687950455.230, "300" ]` i.e. the value `300`

The `mulf` function comes from the Sprig library, and is documented [here](http://masterminds.github.io/sprig/mathf.html).

The same element is queried twice.

The `resources/limits/memory` is `1.20 *  the result` i.e. _360_ and the `resources/requests/memory` is just the queried value i.e. _300_, this means that the limit is padded by 20% on top of the requested limit.

This is run once a day, and will update the Deployment to limit its memory based on the recent consumption.
