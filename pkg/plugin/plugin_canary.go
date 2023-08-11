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

	if (pluginConfig.VirtualServiceSelector == nil && pluginConfig.RouteTableSelector == nil) ||
		(pluginConfig.VirtualServiceSelector != nil && pluginConfig.RouteTableSelector != nil) {
		return fmt.Errorf("one of routeTable or virtualService must be configured")
	}

	if pluginConfig.RouteTableSelector != nil {
		return r.handleCanaryUsingRouteTables(ctx, rollout, desiredWeight, additionalDestinations, pluginConfig)
	}
	return r.handleCanaryUsingVirtualService(ctx, rollout, desiredWeight, additionalDestinations, pluginConfig)
}

func (r *RpcPlugin) handleCanaryUsingVirtualService(
	ctx context.Context,
	rollout *v1alpha1.Rollout,
	desiredWeight int32,
	additionalDestinations []v1alpha1.WeightDestination,
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

	for _, dst := range allDestinations {
		dst.Stable.Weight = &wrapperspb.UInt32Value{Value: uint32(100 - desiredWeight)}
		dst.Canary.Weight = &wrapperspb.UInt32Value{Value: uint32(desiredWeight)}
	}

	if err = r.Client.VirtualServices().PatchVirtualService(ctx, vs, client.MergeFrom(originalVs)); err != nil {
		return err
	}

	return nil
}

func (r *RpcPlugin) handleCanaryUsingRouteTables(
	ctx context.Context,
	rollout *v1alpha1.Rollout,
	desiredWeight int32,
	additionalDestinations []v1alpha1.WeightDestination,
	pluginConfig *GlooEdgeTrafficRouting) error {

	rts, err := r.getRouteTables(ctx, rollout, pluginConfig)
	if err != nil {
		return err
	}

	allRouteTablesForCanary, err := r.getDestinationsInRouteTables(rollout, pluginConfig, rts)
	if err != nil {
		return err
	}

	for _, rt := range allRouteTablesForCanary {
		originalRt := &gwv1.RouteTable{}
		rt.RouteTable.DeepCopyInto(originalRt)

		for _, dst := range rt.Destinations {
			dst.Stable.Weight = &wrapperspb.UInt32Value{Value: uint32(100 - desiredWeight)}
			dst.Canary.Weight = &wrapperspb.UInt32Value{Value: uint32(desiredWeight)}
		}

		if err = r.Client.RouteTables().PatchRouteTable(ctx, rt.RouteTable, client.MergeFrom(originalRt)); err != nil {
			return err
		}
	}

	return nil
}

func (r *RpcPlugin) getDestinationsInVirtualService(
	rollout *v1alpha1.Rollout,
	pluginConfig *GlooEdgeTrafficRouting,
	vs *gwv1.VirtualService) (ret []destinationPair, error error) {

	if vs.Spec.GetVirtualHost() == nil || vs.Spec.GetVirtualHost().GetRoutes() == nil {
		return nil, fmt.Errorf("no virtual host or empty routes in VirtualSevice %s:%s",
			pluginConfig.VirtualServiceSelector.Namespace, pluginConfig.VirtualServiceSelector.Name)
	}

	if len(vs.Spec.GetVirtualHost().GetRoutes()) > 1 && len(pluginConfig.Routes) == 0 {
		return nil, fmt.Errorf("virtual host has multiple routes but canary config doesn't specify which routes to use")
	}

	ret = r.getDestinationsInRoutes(vs.Spec.GetVirtualHost().GetRoutes(), rollout, pluginConfig)

	if len(pluginConfig.Routes) > 0 && len(ret) != len(pluginConfig.Routes) {
		return nil, fmt.Errorf("some/all routes specified in canary rollout configuration do not have stable/canary services")
	}

	if len(ret) == 0 {
		return nil, fmt.Errorf("couldn't find stable and/or canary upstream in VirtualService %s/%s, with route names in %v",
			pluginConfig.VirtualServiceSelector.Namespace, pluginConfig.VirtualServiceSelector.Name, pluginConfig.Routes)
	}

	return ret, nil
}

type destinationPair struct {
	Canary *v1.WeightedDestination
	Stable *v1.WeightedDestination
}

type routeTableWithDestinations struct {
	RouteTable   *gwv1.RouteTable
	Destinations []destinationPair
}

func (r *RpcPlugin) getDestinationsInRouteTables(
	rollout *v1alpha1.Rollout,
	pluginConfig *GlooEdgeTrafficRouting,
	routeTables []*gwv1.RouteTable) (ret []routeTableWithDestinations, err error) {

	for _, rt := range routeTables {
		if rt.Spec.GetRoutes() == nil {
			continue
		}

		if len(rt.Spec.GetRoutes()) > 1 && len(pluginConfig.Routes) == 0 {
			return nil,
				fmt.Errorf("route table %s/%s has multiple routes but canary config doesn't specify which routes to use", rt.GetNamespace(), rt.GetName())
		}

		dsts := r.getDestinationsInRoutes(rt.Spec.GetRoutes(), rollout, pluginConfig)
		if len(dsts) == 0 {
			continue
		}

		ret = append(ret, routeTableWithDestinations{RouteTable: rt, Destinations: dsts})
	}

	if len(ret) == 0 {
		return nil, fmt.Errorf("couldn't find stable and/or canary services in RouteTables selected with Name: '%s', Namespace: '%s', Labels: %v, with route names in %v",
			pluginConfig.RouteTableSelector.Name, pluginConfig.RouteTableSelector.Namespace, pluginConfig.RouteTableSelector.Labels, pluginConfig.Routes)
	}

	return ret, nil
}

func (r *RpcPlugin) getDestinationsInRoutes(
	routes []*gwv1.Route,
	rollout *v1alpha1.Rollout,
	pluginConfig *GlooEdgeTrafficRouting) (ret []destinationPair) {

	for _, r := range routes {
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
			ret = append(ret, destinationPair{Stable: stable, Canary: canary})
		}
	}

	return ret
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

func (r *RpcPlugin) getRouteTables(ctx context.Context, rollout *v1alpha1.Rollout, pluginConfig *GlooEdgeTrafficRouting) ([]*gwv1.RouteTable, error) {
	namespace := pluginConfig.RouteTableSelector.Namespace

	if namespace == "" {
		r.LogCtx.Debugf("defaulting RouteTable selector namespace to Rollout namespace %s for rollout %s", rollout.Namespace, rollout.Name)
		namespace = rollout.Namespace
	}

	if pluginConfig.RouteTableSelector.Name == "" && len(pluginConfig.RouteTableSelector.Labels) == 0 {
		return nil, fmt.Errorf("name or labels field must be set in RouteTable selector")
	}

	if pluginConfig.RouteTableSelector.Name != "" {
		return r.getRouteTable(ctx, namespace, pluginConfig.RouteTableSelector.Name)
	}

	return r.listRouteTables(ctx, namespace, pluginConfig)
}

func (r *RpcPlugin) getRouteTable(ctx context.Context, ns, name string) ([]*gwv1.RouteTable, error) {
	rt, err := r.Client.RouteTables().GetRouteTable(ctx,
		client.ObjectKey{Namespace: ns, Name: name})
	if err != nil {
		return nil, err
	}
	return []*gwv1.RouteTable{rt}, nil
}

func (r *RpcPlugin) listRouteTables(ctx context.Context, ns string, pluginConfig *GlooEdgeTrafficRouting) ([]*gwv1.RouteTable, error) {
	rts, err := r.Client.RouteTables().ListRouteTable(ctx,
		client.MatchingLabels(pluginConfig.RouteTableSelector.Labels),
		client.InNamespace(ns))
	if err != nil {
		return nil, err
	}

	ret := make([]*gwv1.RouteTable, len(rts.Items))
	for i := range rts.Items {
		ret[i] = &rts.Items[i]
	}

	return ret, nil
}
