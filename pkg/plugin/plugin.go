package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/argoproj-labs/rollouts-plugin-trafficrouter-glooedge/pkg/gloo"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	pluginTypes "github.com/argoproj/argo-rollouts/utils/plugin/types"
	"github.com/sirupsen/logrus"
	gwv1 "github.com/solo-io/solo-apis/pkg/api/gateway.solo.io/v1"
	v1 "github.com/solo-io/solo-apis/pkg/api/gloo.solo.io/v1"
	"golang.org/x/exp/slices"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	Type                = "GlooEdgeAPI"
	GlooEdgeUpdateError = "GlooEdgeUpdateError"
	PluginName          = "solo-io/glooedge"
)

type RpcPlugin struct {
	IsTest bool
	LogCtx *logrus.Entry
	Client gloo.GlooV1ClientSet
}

type GlooEdgeTrafficRouting struct {
	RouteTableSelector     *DumbObjectSelector `json:"routeTable" protobuf:"bytes,1,name=routeTable"`
	VirtualServiceSelector *DumbObjectSelector `json:"virtualService" protobuf:"bytes,2,name=virtualService"`
	Routes                 []string            `json:"routes" protobuf:"bytes,3,name=routes"`
}

type DumbObjectSelector struct {
	Labels    map[string]string `json:"labels" protobuf:"bytes,1,name=labels"`
	Name      string            `json:"name" protobuf:"bytes,2,name=name"`
	Namespace string            `json:"namespace" protobuf:"bytes,3,name=namespace"`
}

type destinationPair struct {
	DestinationsParent *v1.RouteAction
	Canary             *v1.WeightedDestination
	Stable             *v1.WeightedDestination
}

type routeTableWithDestinations struct {
	RouteTable   *gwv1.RouteTable
	Destinations []destinationPair
}

func (r *RpcPlugin) InitPlugin() pluginTypes.RpcError {
	if r.IsTest {
		return pluginTypes.RpcError{}
	}
	client, err := gloo.NewGlooV1ClientSet()
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}
	r.Client = client
	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) UpdateHash(rollout *v1alpha1.Rollout, canaryHash, stableHash string, additionalDestinations []v1alpha1.WeightDestination) pluginTypes.RpcError {
	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) SetWeight(
	rollout *v1alpha1.Rollout,
	desiredWeight int32,
	additionalDestinations []v1alpha1.WeightDestination) pluginTypes.RpcError {

	ctx := context.TODO()
	glooPluginConfig, err := getPluginConfig(rollout)
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}

	if (glooPluginConfig.VirtualServiceSelector == nil && glooPluginConfig.RouteTableSelector == nil) ||
		(glooPluginConfig.VirtualServiceSelector != nil && glooPluginConfig.RouteTableSelector != nil) {
		return pluginTypes.RpcError{
			ErrorString: "one of virtualService or routeTable selectors must be set in solo-io/glooedge plugin configuration",
		}
	}

	if rollout.Spec.Strategy.Canary != nil && glooPluginConfig.VirtualServiceSelector != nil {
		err = r.handleCanaryUsingVirtualService(ctx, rollout, desiredWeight, additionalDestinations, glooPluginConfig)
	} else {
		err = r.handleCanaryUsingRouteTables(ctx, rollout, desiredWeight, additionalDestinations, glooPluginConfig)
	}

	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: fmt.Sprintf("failed canary rollout: %s", err),
		}
	}

	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) SetHeaderRoute(rollout *v1alpha1.Rollout, headerRouting *v1alpha1.SetHeaderRoute) pluginTypes.RpcError {
	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) SetMirrorRoute(rollout *v1alpha1.Rollout, setMirrorRoute *v1alpha1.SetMirrorRoute) pluginTypes.RpcError {
	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) VerifyWeight(rollout *v1alpha1.Rollout, desiredWeight int32, additionalDestinations []v1alpha1.WeightDestination) (pluginTypes.RpcVerified, pluginTypes.RpcError) {
	return pluginTypes.NotImplemented, pluginTypes.RpcError{}
}

func (r *RpcPlugin) RemoveManagedRoutes(rollout *v1alpha1.Rollout) pluginTypes.RpcError {
	// we could remove the canary destination, but not required since it will have 0 weight at the end of rollout
	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) Type() string {
	return Type
}

func getPluginConfig(rollout *v1alpha1.Rollout) (*GlooEdgeTrafficRouting, error) {
	glooplatformConfig := GlooEdgeTrafficRouting{}

	err := json.Unmarshal(rollout.Spec.Strategy.Canary.TrafficRouting.Plugins[PluginName], &glooplatformConfig)
	if err != nil {
		return nil, err
	}

	return &glooplatformConfig, nil
}

func getStableServiceName(rollout *v1alpha1.Rollout) string {
	return rollout.Spec.Strategy.Canary.StableService
}

func getCanaryServiceName(rollout *v1alpha1.Rollout) string {
	return rollout.Spec.Strategy.Canary.CanaryService
}

func (r *RpcPlugin) maybeConvertSingleToMulti(routeTables []routeTableWithDestinations) {
	for i := range routeTables {
		for j := range routeTables[i].Destinations {
			if routeTables[i].Destinations[j].DestinationsParent.GetMulti() != nil {
				continue
			}
			routeTables[i].Destinations[j].DestinationsParent.Destination = &v1.RouteAction_Multi{
				Multi: &v1.MultiDestination{
					Destinations: []*v1.WeightedDestination{
						routeTables[i].Destinations[j].Stable,
					},
				},
			}
		}
	}
}

func (r *RpcPlugin) maybeCreateCanaryDestinations(
	routeTables []routeTableWithDestinations, canaryName string) {

	for i := range routeTables {
		for j := range routeTables[i].Destinations {
			if routeTables[i].Destinations[j].Canary != nil {
				continue
			}
			routeTables[i].Destinations[j].Canary =
				r.newCanaryDestination(routeTables[i].Destinations[j].Stable, canaryName)
			routeTables[i].Destinations[j].DestinationsParent.GetMulti().Destinations =
				append(routeTables[i].Destinations[j].DestinationsParent.GetMulti().GetDestinations(), routeTables[i].Destinations[j].Canary)
		}
	}
}

func (r *RpcPlugin) newCanaryDestination(stableDst *v1.WeightedDestination, canaryName string) *v1.WeightedDestination {
	ret := stableDst.Clone().(*v1.WeightedDestination)
	ret.GetDestination().GetUpstream().Name = canaryName
	ret.Weight = &wrapperspb.UInt32Value{Value: uint32(0)}
	return ret
}

func (r *RpcPlugin) getDestinationsInRoutes(
	routes []*gwv1.Route,
	rollout *v1alpha1.Rollout,
	pluginConfig *GlooEdgeTrafficRouting) (ret []destinationPair) {

	for _, route := range routes {
		if len(pluginConfig.Routes) > 0 && !slices.Contains(pluginConfig.Routes, route.GetName()) {
			continue
		}

		if route.GetRouteAction() == nil ||
			(route.GetRouteAction().GetMulti() == nil && route.GetRouteAction().GetSingle() == nil) {
			continue
		}

		if route.GetRouteAction().GetSingle() != nil {
			ret = append(ret, r.getDestinationInSingle(route, rollout)...)
			continue
		}

		if route.GetRouteAction().GetMulti().GetDestinations() == nil {
			continue
		}
		ret = append(ret, r.getDestinationsInMulti(route, rollout)...)
	}

	return ret
}

func (r *RpcPlugin) getDestinationsInMulti(route *gwv1.Route, rollout *v1alpha1.Rollout) (ret []destinationPair) {
	var stable, canary *v1.WeightedDestination
	for _, dst := range route.GetRouteAction().GetMulti().GetDestinations() {
		if dst.GetDestination() == nil || dst.GetDestination().GetUpstream() == nil ||
			dst.GetDestination().GetUpstream().GetName() == "" {
			continue
		}
		name := dst.GetDestination().GetUpstream().GetName()
		if strings.EqualFold(getCanaryServiceName(rollout), name) {
			canary = dst
		} else if strings.EqualFold(getStableServiceName(rollout), name) {
			stable = dst
		}
	}
	if stable != nil {
		ret = append(ret, destinationPair{DestinationsParent: route.GetRouteAction(), Stable: stable, Canary: canary})
	}

	return ret
}

// We will be converting `single` RouteAction to a `multi` one that will use WeightedDestinations created here
func (r *RpcPlugin) getDestinationInSingle(route *gwv1.Route, rollout *v1alpha1.Rollout) (ret []destinationPair) {
	var stable *v1.WeightedDestination

	dst := route.GetRouteAction().GetSingle()
	if dst.GetUpstream() == nil || dst.GetUpstream().GetName() == "" {
		return ret
	}

	if strings.EqualFold(getStableServiceName(rollout), dst.GetUpstream().GetName()) {
		stable = &v1.WeightedDestination{
			Destination: dst,
		}
		ret = append(ret, destinationPair{DestinationsParent: route.GetRouteAction(), Stable: stable})
	}

	return ret
}
