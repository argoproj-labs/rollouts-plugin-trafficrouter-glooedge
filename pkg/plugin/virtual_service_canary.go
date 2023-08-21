package plugin

import (
	"context"
	"fmt"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	gwv1 "github.com/solo-io/solo-apis/pkg/api/gateway.solo.io/v1"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *RpcPlugin) handleCanaryUsingVirtualService(
	ctx context.Context,
	rollout *v1alpha1.Rollout,
	desiredWeight int32,
	pluginConfig *GlooEdgeTrafficRouting) error {

	vs, err := r.getVirtualService(ctx, rollout, pluginConfig)
	if err != nil {
		return err
	}

	originalVs := &gwv1.VirtualService{}
	vs.DeepCopyInto(originalVs)

	allDestinations, err := r.getDestinationsInVirtualService(rollout, pluginConfig, vs)
	if err != nil {
		return err
	}

	r.maybeConvertSingleToMulti([]routeTableWithDestinations{{Destinations: allDestinations}})
	r.maybeCreateCanaryDestinations(
		[]routeTableWithDestinations{{Destinations: allDestinations}}, getCanaryServiceName(rollout))

	for _, dst := range allDestinations {
		dst.Stable.Weight = &wrapperspb.UInt32Value{Value: uint32(100 - desiredWeight)}
		dst.Canary.Weight = &wrapperspb.UInt32Value{Value: uint32(desiredWeight)}
	}

	if err = r.Client.VirtualServices().PatchVirtualService(ctx, vs, client.MergeFrom(originalVs)); err != nil {
		return err
	}

	return nil
}

func (r *RpcPlugin) getDestinationsInVirtualService(
	rollout *v1alpha1.Rollout,
	pluginConfig *GlooEdgeTrafficRouting,
	vs *gwv1.VirtualService) (ret []destinationPair, error error) {

	if vs.Spec.GetVirtualHost().GetRoutes() == nil {
		return nil, fmt.Errorf("no virtual host or empty routes in VirtualSevice %s:%s",
			pluginConfig.VirtualServiceSelector.Namespace, pluginConfig.VirtualServiceSelector.Name)
	}

	if len(vs.Spec.GetVirtualHost().GetRoutes()) > 1 && len(pluginConfig.Routes) == 0 {
		return nil, fmt.Errorf("virtual host has multiple routes but canary config doesn't specify which routes to use")
	}

	ret = r.getDestinationsInRoutes(vs.Spec.GetVirtualHost().GetRoutes(), rollout, pluginConfig)

	if len(pluginConfig.Routes) > 0 && len(ret) != len(pluginConfig.Routes) {
		return nil, fmt.Errorf("some/all routes specified in canary rollout configuration do not have stable upstreams")
	}

	if len(ret) == 0 {
		return nil, fmt.Errorf("couldn't find stable upstreams in VirtualService %s/%s, with route names in %v",
			pluginConfig.VirtualServiceSelector.Namespace, pluginConfig.VirtualServiceSelector.Name, pluginConfig.Routes)
	}

	return ret, nil
}

func (r *RpcPlugin) getVirtualService(ctx context.Context, rollout *v1alpha1.Rollout, pluginConfig *GlooEdgeTrafficRouting) (*gwv1.VirtualService, error) {
	vsNamespace := pluginConfig.VirtualServiceSelector.Namespace

	if vsNamespace == "" {
		r.LogCtx.Debugf("defaulting VirtualService selector namespace to Rollout namespace %s for rollout %s", rollout.Namespace, rollout.Name)
		vsNamespace = rollout.Namespace
	}

	if pluginConfig.VirtualServiceSelector.Name == "" {
		return nil, fmt.Errorf("must specify the name of the VirtualService")
	}

	vs, err := r.Client.VirtualServices().GetVirtualService(ctx,
		client.ObjectKey{Namespace: vsNamespace, Name: pluginConfig.VirtualServiceSelector.Name})
	if err != nil {
		return nil, err
	}

	return vs, nil
}
