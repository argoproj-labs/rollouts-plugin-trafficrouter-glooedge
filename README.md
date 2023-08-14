# Argo Rollout Gloo Edge Plugin

An Argo Rollouts plugin for [Gloo Edge](https://www.solo.io/products/gloo-edge/). Documentation for Argo Rollouts that covers canary rollouts can be found [here](https://argo-rollouts.readthedocs.io/en/stable/features/canary/#canary-deployment-strategy).

## VirtualService based Canary Rollouts

A snippet of of a rollout configuration that contains Gloo Edge plugin configuration for VirtualService-based rollouts:
```
  strategy:
    canary:
      canaryService: echo-v2 
      stableService: echo-v1
      trafficRouting:
        plugins:
          solo-io/glooedge:
            virtualService:
              name: echo
              namespace: gloo-system
            routes:
              - route-1
              - route-2
      steps:
        - setWeight: 10
        - pause: {}
```

`stableService` field contains the name of the k8s Service pointing to a stable release. `canaryService` field contains the name of the service pointing to a canary release.

Everything under `plugins:solo-io/glooedge` is a configuration for the Gloo Edge plugin. `virtualService` specifies which VirtualService is used to route traffic to stable and canary versions of the service. The plugin will update weights in the selected VirtualService according to rollout steps in the rollout configuration. 

If there are multiple routes present in the VirtualHost of a VirtualService, the routes where weights will need to be updated during the rollout must be listed under `routes`. Otherwise this setting is optional. All routes listed under `routes` must exist.

An example VirtualService configuration:
```
apiVersion: gateway.solo.io/v1
kind: VirtualService
metadata:
  name: echo
  namespace: gloo-system
spec:
  virtualHost:
    domains:
    - '*'
    routes:
    - matchers:
      - prefix: /
      routeAction:
        multi:
          destinations:
          - destination:
              upstream:
                name: echo-v1
                namespace: echo
            weight: 100
```

Note that only `multi` routeActions are supported. It's ok to define a destination for a stable release only. The names for stable and canary `Upstream`s are expected to match the name of the services (and `stableService` and `canaryService` fields of the plugin configuration).

A complete example of a VirtualService-based canary rollout can be found in examples/canaries-with-vs.

## RouteTable based Canary Rollouts
A snippet of of a rollout configuration that contains Gloo Edge plugin configuration for RouteTable-based rollouts:
```
  strategy:
    canary:
      canaryService: echo-v2 
      stableService: echo-v1
      trafficRouting:
        plugins:
          solo-io/glooedge:
            routeTable:
              name: echo
              namespace: gloo-system
              labels:
                test-label: label-1
            routes:
              - route-1
              - route-2
      steps:
        - setWeight: 10
        - pause: {}
```

`stableService` field contains the name of the k8s Service pointing to a stable release. `canaryService` field contains the name of the service pointing to a canary release.

Everything under `plugins:solo-io/glooedge` is a configuration for the Gloo Edge plugin. `routeTable` specifies which routeTables are used to route traffic to stable and canary versions of the service. When a RouteTable name is specified, only a single RouteTable with this name will be selected, the labels will be ignored. When the `name` field is empty, all route tables with labels matching the `labels` field will be selected.  

If there are multiple routes present in the VirtualHost of a VirtualService, the routes where weights will need to be updated during the rollout must be listed under `routes`. Otherwise this setting is optional. All routes listed under `routes` must exist.

Just like with VirtualService-based rollouts, only `multi` RouteActions are supported.

Complete examples of RouteTable-based canary rollouts can be found in examples/canaries-with-single-routetable/ examples/canaries-with-multiple-routetables/ directories.
