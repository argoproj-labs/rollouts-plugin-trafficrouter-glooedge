package plugin

import (
	"context"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	pluginTypes "github.com/argoproj/argo-rollouts/utils/plugin/types"
)

func (r *RpcPlugin) handleCanary(ctx context.Context, rollout *v1alpha1.Rollout, desiredWeight int32, additionalDestinations []v1alpha1.WeightDestination, pluginConfig *GlooEdgeTrafficRouting) pluginTypes.RpcError {
	panic("not impl")
	return pluginTypes.RpcError{}
}
