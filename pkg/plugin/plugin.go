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
	gloov1 "github.com/solo-io/solo-apis/pkg/api/gateway.solo.io/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
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
	RouteTableSelector     *DumbObjectSelector `json:"routeTableSelector" protobuf:"bytes,1,name=routeTableSelector"`
	VirtualServiceSelector *DumbObjectSelector `json:"routeTableSelector" protobuf:"bytes,1,name=routeTableSelector"`
}

type DumbObjectSelector struct {
	Labels    map[string]string `json:"labels" protobuf:"bytes,1,name=labels"`
	Name      string            `json:"name" protobuf:"bytes,2,name=name"`
	Namespace string            `json:"namespace" protobuf:"bytes,3,name=namespace"`
}

type GlooMatchedRouteTable struct {
	// matched gloo platform route table
	RouteTable *gloov1.RouteTable
	// matched http routes within the routetable
	// HttpRoutes []*GlooMatchedHttpRoutes
	// // matched tcp routes within the routetable
	// TCPRoutes []*GlooMatchedTCPRoutes
	// // matched tls routes within the routetable
	// TLSRoutes []*GlooMatchedTLSRoutes
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

func (r *RpcPlugin) SetWeight(rollout *v1alpha1.Rollout, desiredWeight int32, additionalDestinations []v1alpha1.WeightDestination) pluginTypes.RpcError {
	// TODO: check rollout type
	ctx := context.TODO()
	glooPluginConfig, err := getPluginConfig(rollout)
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}

	if rollout.Spec.Strategy.Canary != nil {
		err = r.handleCanary(ctx, rollout, desiredWeight, additionalDestinations, glooPluginConfig)
		if err != nil {
			return pluginTypes.RpcError{
				ErrorString: fmt.Sprintf("failed canary rollout: %s", err),
			}
		}
	} else if rollout.Spec.Strategy.BlueGreen != nil {
		return r.handleBlueGreen(rollout, glooPluginConfig)
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
	return pluginTypes.Verified, pluginTypes.RpcError{}
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

func (r *RpcPlugin) getRouteTables(ctx context.Context, rollout *v1alpha1.Rollout, pluginConfig *GlooEdgeTrafficRouting) ([]*GlooMatchedRouteTable, error) {
	if pluginConfig.RouteTableSelector == nil {
		return nil, fmt.Errorf("routeTable selector is required")
	}

	if strings.EqualFold(pluginConfig.RouteTableSelector.Namespace, "") {
		r.LogCtx.Debugf("defaulting routeTableSelector namespace to Rollout namespace %s for rollout %s", rollout.Namespace, rollout.Name)
		pluginConfig.RouteTableSelector.Namespace = rollout.Namespace
	}

	var rts []*gloov1.RouteTable

	if !strings.EqualFold(pluginConfig.RouteTableSelector.Name, "") {
		r.LogCtx.Debugf("getRouteTables using ns:name ref %s:%s to get single table", pluginConfig.RouteTableSelector.Name, pluginConfig.RouteTableSelector.Namespace)
		result, err := r.Client.RouteTables().GetRouteTable(ctx,
			client.ObjectKey{Namespace: pluginConfig.RouteTableSelector.Namespace, Name: pluginConfig.RouteTableSelector.Name})
		if err != nil {
			return nil, err
		}

		r.LogCtx.Debugf("getRouteTables using ns:name ref %s:%s found 1 table", pluginConfig.RouteTableSelector.Name, pluginConfig.RouteTableSelector.Namespace)
		rts = append(rts, result)
	} else {
		opts := &k8sclient.ListOptions{}

		if pluginConfig.RouteTableSelector.Labels != nil {
			opts.LabelSelector = labels.SelectorFromSet(pluginConfig.RouteTableSelector.Labels)
		}
		if !strings.EqualFold(pluginConfig.RouteTableSelector.Namespace, "") {
			opts.Namespace = pluginConfig.RouteTableSelector.Namespace
		}

		r.LogCtx.Debugf("getRouteTables listing tables with opts %+v", opts)
		var err error

		rtl, err := r.Client.RouteTables().ListRouteTable(ctx, opts)
		if err != nil {
			return nil, err
		}
		r.LogCtx.Debugf("getRouteTables listing tables with opts %+v; found %d routeTables", opts, len(rts))

		for i := range rtl.Items {
			rts = append(rts, &rtl.Items[i])
		}
	}

	matched := []*GlooMatchedRouteTable{}

	for _, rt := range rts {
		matchedRt := &GlooMatchedRouteTable{
			RouteTable: rt,
		}
		// destination matching
		if err := matchedRt.matchRoutes(r.LogCtx, rollout, pluginConfig); err != nil {
			return nil, err
		}

		matched = append(matched, matchedRt)
	}

	return matched, nil
}

func (g *GlooMatchedRouteTable) matchRoutes(logCtx *logrus.Entry, rollout *v1alpha1.Rollout, pluginConfig *GlooEdgeTrafficRouting) error {
	if g.RouteTable == nil {
		return fmt.Errorf("matchRoutes called for nil RouteTable")
	}

	// // HTTP Routes
	// for _, httpRoute := range g.RouteTable.Spec.Http {
	// 	// find the destination that matches the stable svc
	// 	fw := httpRoute.GetForwardTo()
	// 	if fw == nil {
	// 		logCtx.Debugf("skipping route %s.%s because forwardTo is nil", g.RouteTable.Name, httpRoute.Name)
	// 		continue
	// 	}

	// 	// skip non-matching routes if RouteSelector provided
	// 	if trafficConfig.RouteSelector != nil {
	// 		// if name was provided, skip if route name doesn't match
	// 		if !strings.EqualFold(trafficConfig.RouteSelector.Name, "") && !strings.EqualFold(trafficConfig.RouteSelector.Name, httpRoute.Name) {
	// 			logCtx.Debugf("skipping route %s.%s because it doesn't match route name selector %s", g.RouteTable.Name, httpRoute.Name, trafficConfig.RouteSelector.Name)
	// 			continue
	// 		}
	// 		// if labels provided, skip if route labels do not contain all specified labels
	// 		if trafficConfig.RouteSelector.Labels != nil {
	// 			matchedLabels := func() bool {
	// 				for k, v := range trafficConfig.RouteSelector.Labels {
	// 					if vv, ok := httpRoute.Labels[k]; ok {
	// 						if !strings.EqualFold(v, vv) {
	// 							logCtx.Debugf("skipping route %s.%s because route labels do not contain %s=%s", g.RouteTable.Name, httpRoute.Name, k, v)
	// 							return false
	// 						}
	// 					}
	// 				}
	// 				return true
	// 			}()
	// 			if !matchedLabels {
	// 				continue
	// 			}
	// 		}
	// 		logCtx.Debugf("route %s.%s passed RouteSelector", g.RouteTable.Name, httpRoute.Name)
	// 	}

	// 	// find destinations
	// 	// var matchedDestinations []*GlooDestinations
	// 	var canary, stable *solov2.DestinationReference
	// 	for _, dest := range fw.Destinations {
	// 		ref := dest.GetRef()
	// 		if ref == nil {
	// 			logCtx.Debugf("skipping destination %s.%s because destination ref was nil; %+v", g.RouteTable.Name, httpRoute.Name, dest)
	// 			continue
	// 		}
	// 		if strings.EqualFold(ref.Name, rollout.Spec.Strategy.Canary.StableService) {
	// 			logCtx.Debugf("matched stable ref %s.%s.%s", g.RouteTable.Name, httpRoute.Name, ref.Name)
	// 			stable = dest
	// 			continue
	// 		}
	// 		if strings.EqualFold(ref.Name, rollout.Spec.Strategy.Canary.CanaryService) {
	// 			logCtx.Debugf("matched canary ref %s.%s.%s", g.RouteTable.Name, httpRoute.Name, ref.Name)
	// 			canary = dest
	// 			// bail if we found both stable and canary
	// 			if stable != nil {
	// 				break
	// 			}
	// 			continue
	// 		}
	// 	}

	// 	if stable != nil {
	// 		dest := &GlooMatchedHttpRoutes{
	// 			HttpRoute: httpRoute,
	// 			Destinations: &GlooDestinations{
	// 				StableOrActiveDestination:  stable,
	// 				CanaryOrPreviewDestination: canary,
	// 			},
	// 		}
	// 		logCtx.Debugf("adding destination %+v", dest)
	// 		g.HttpRoutes = append(g.HttpRoutes, dest)
	// 	}
	// } // end range httpRoutes

	return nil
}
