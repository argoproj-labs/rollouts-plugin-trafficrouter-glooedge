package gloo

import (
	"github.com/argoproj-labs/rollouts-plugin-trafficrouter-glooedge/pkg/util"
	gloov1 "github.com/solo-io/solo-apis/pkg/api/gateway.solo.io/v1"
)

type GlooV1ClientSet interface {
	RouteTables() gloov1.RouteTableClient
	VirtualServices() gloov1.VirtualServiceClient
}

func NewGlooV1ClientSet() (GlooV1ClientSet, error) {
	cfg, err := util.GetKubeConfig()
	if err != nil {
		return nil, err
	}

	clientset, err := gloov1.NewClientsetFromConfig(cfg)
	if err != nil {
		return nil, err
	}

	return clientset, nil
}
