package plugin

import (
	"context"
	"fmt"
	"strings"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	gwv1 "github.com/solo-io/solo-apis/pkg/api/gateway.solo.io/v1"
	v1 "github.com/solo-io/solo-apis/pkg/api/gloo.solo.io/v1"
	"golang.org/x/exp/slices"
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

	allDestinations, err := r.getDestinations(ctx, rollout, pluginConfig, vs)
	if err != nil {
		return err
	}

	if len(allDestinations) == 0 {
		return fmt.Errorf("couldn't find stable and/or canary service in VirtualService %s:%s",
			pluginConfig.VirtualServiceSelector.Namespace, pluginConfig.VirtualServiceSelector.Name)
	}

	for _, dsts := range allDestinations {
		stable, canary := dsts[0], dsts[1]
		stable.Weight = &wrapperspb.UInt32Value{Value: uint32(100 - desiredWeight)}
		canary.Weight = &wrapperspb.UInt32Value{Value: uint32(desiredWeight)}
	}

	if err = r.Client.VirtualServices().PatchVirtualService(ctx, vs, client.MergeFrom(originalVs)); err != nil {
		return err
	}

	return nil
}

// returns an array of tuples {stable *v1.WeightedDestination, canary *v1.WeightedDestination}
func (r *RpcPlugin) getDestinations(
	ctx context.Context,
	rollout *v1alpha1.Rollout,
	pluginConfig *GlooEdgeTrafficRouting,
	vs *gwv1.VirtualService) (ret [][]*v1.WeightedDestination, error error) {

	if vs.Spec.GetVirtualHost() == nil || vs.Spec.GetVirtualHost().GetRoutes() == nil {
		return nil, fmt.Errorf("no virtual host or empty routes in VirtualSevice %s:%s",
			pluginConfig.VirtualServiceSelector.Namespace, pluginConfig.VirtualServiceSelector.Name)
	}

	if len(vs.Spec.GetVirtualHost().GetRoutes()) > 1 && len(pluginConfig.Routes) == 0 {
		return nil, fmt.Errorf("virtual host has multiple routes but canary config doesn't specify which routes to use")
	}

	for _, r := range vs.Spec.GetVirtualHost().GetRoutes() {
		if len(pluginConfig.Routes) > 0 && !slices.Contains(pluginConfig.Routes, r.GetName()) {
			continue
		}

		if r.GetRouteAction() == nil || r.GetRouteAction().GetMulti() == nil ||
			r.GetRouteAction().GetMulti().GetDestinations() == nil {
			continue
		}

		var stable, canary *v1.WeightedDestination
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
		if stable != nil && canary != nil {
			ret = append(ret, []*v1.WeightedDestination{stable, canary})
		}
	}

	if len(pluginConfig.Routes) > 0 && len(ret) != len(pluginConfig.Routes) {
		return nil, fmt.Errorf("some/all routes specified in canary rollout configuration do not have stable/canary services")
	}

	return ret, nil
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
