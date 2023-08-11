package plugin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/argoproj-labs/rollouts-plugin-trafficrouter-glooedge/pkg/gloo"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	pluginTypes "github.com/argoproj/argo-rollouts/utils/plugin/types"
	"github.com/sirupsen/logrus"
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

	if rollout.Spec.Strategy.Canary != nil {
		err = r.handleCanary(ctx, rollout, desiredWeight, additionalDestinations, glooPluginConfig)
		if err != nil {
			return pluginTypes.RpcError{
				ErrorString: fmt.Sprintf("failed canary rollout: %s", err),
			}
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
