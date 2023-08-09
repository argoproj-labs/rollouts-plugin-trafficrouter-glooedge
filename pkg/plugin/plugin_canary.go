package plugin

import (
	"context"
	"fmt"
	"strings"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	gwv1 "github.com/solo-io/solo-apis/pkg/api/gateway.solo.io/v1"
	v1 "github.com/solo-io/solo-apis/pkg/api/gloo.solo.io/v1"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *RpcPlugin) handleCanary(
	ctx context.Context,
	rollout *v1alpha1.Rollout,
	desiredWeight int32,
	additionalDestinations []v1alpha1.WeightDestination,
	pluginConfig *GlooEdgeTrafficRouting) error {

	vs, err := r.getVS(ctx, rollout, pluginConfig)
	if err != nil {
		return err
	}

	originalVs := &gwv1.VirtualService{}
	vs.DeepCopyInto(originalVs)

	stable, canary, err := r.getDestinations(ctx, rollout, pluginConfig, vs)
	if err != nil {
		return err
	}

	if stable == nil || canary == nil {
		return fmt.Errorf("couldn't find stable or canary subsets in VirtualService %s:%s",
			pluginConfig.VirtualServiceSelector.Namespace, pluginConfig.VirtualServiceSelector.Name)
	}

	stable.Weight = &wrapperspb.UInt32Value{Value: uint32(100 - desiredWeight)}
	canary.Weight = &wrapperspb.UInt32Value{Value: uint32(desiredWeight)}

	if err = r.Client.VirtualServices().PatchVirtualService(ctx, vs, client.MergeFrom(originalVs)); err != nil {
		return err
	}

	return nil
}

func (r *RpcPlugin) getDestinations(
	ctx context.Context,
	rollout *v1alpha1.Rollout,
	pluginConfig *GlooEdgeTrafficRouting,
	vs *gwv1.VirtualService) (stable *v1.WeightedDestination, canary *v1.WeightedDestination, err error) {

	if vs.Spec.GetVirtualHost() == nil || vs.Spec.GetVirtualHost().GetRoutes() == nil {
		return nil, nil, fmt.Errorf("no virtual host or empty routes in VirtualSevice %s:%s",
			pluginConfig.VirtualServiceSelector.Namespace, pluginConfig.VirtualServiceSelector.Name)
	}

	for _, r := range vs.Spec.GetVirtualHost().GetRoutes() {
		if r.GetRouteAction() == nil || r.GetRouteAction().GetMulti() == nil ||
			r.GetRouteAction().GetMulti().GetDestinations() == nil {
			continue
		}

		for _, dst := range r.GetRouteAction().GetMulti().GetDestinations() {
			if dst.GetDestination() == nil || dst.GetDestination().GetUpstream() == nil ||
				dst.GetDestination().GetUpstream().GetName() == "" {
				continue
			}
			name := dst.GetDestination().GetUpstream().GetName()
			if strings.EqualFold(rollout.Spec.Strategy.Canary.CanaryService, name) {
				canary = dst
			} else if strings.EqualFold(rollout.Spec.Strategy.Canary.StableService, name) {
				stable = dst
			}
		}
	}

	return stable, canary, nil
}

func (r *RpcPlugin) getVS(ctx context.Context, rollout *v1alpha1.Rollout, pluginConfig *GlooEdgeTrafficRouting) (*gwv1.VirtualService, error) {
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
