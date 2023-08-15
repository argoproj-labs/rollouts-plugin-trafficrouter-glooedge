package plugin

import (
	"context"
	"fmt"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	gwv1 "github.com/solo-io/solo-apis/pkg/api/gateway.solo.io/v1"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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

	r.maybeConvertSingleToMulti(allRouteTablesForCanary)
	r.maybeCreateCanaryDestinations(allRouteTablesForCanary, getCanaryServiceName(rollout))

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
		return nil, fmt.Errorf("couldn't find stable services in RouteTables selected with Name: '%s', Namespace: '%s', Labels: %v, with route names in %v",
			pluginConfig.RouteTableSelector.Name, pluginConfig.RouteTableSelector.Namespace, pluginConfig.RouteTableSelector.Labels, pluginConfig.Routes)
	}

	return ret, nil
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
