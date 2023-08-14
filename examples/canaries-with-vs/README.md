# VirtualService-based Canary Rollouts
This example demonstrates a canary rollout with only a VirtualService used to route traffic to stable and canary versions of the service. A k8s cluster with a Gloo Edge gateway installed are required for this example.

To install Argo Rollouts with a Gloo Edge plugin:
```kubectl apply -k deploy/```

To create k8s Services, Upstreams, the VirtualSerice and initial Deployment used in the example:
```kubectl apply -f examples/canaries-with-vs/setup.yaml```

To create an Argo rollout (creates a rollout, ReplicaSets, starts routing traffic to the stable rs):
```kubectl apply -f examples/canaries-with-vs/create-rollout.yaml```

To start forwarding traffic to the canary:
```kubectl apply -f examples/canaries-with-vs/start-canary-traffic-routing.yaml```

To check the status of the rollout; requires argo plugin installed, see [here](https://github.com/argoproj/argo-rollouts/blob/master/docs/installation.md#kubectl-plugin-installation):
```kubectl argo rollouts get rollout echo-rollouts -n echo -w```

To promote the rollout between steps:
```kubectl argo rollouts promote echo-rollouts -n echo```
