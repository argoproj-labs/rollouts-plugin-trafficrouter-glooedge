package gloo

import (
	"context"

	"github.com/argoproj-labs/rollouts-plugin-trafficrouter-glooedge/util"
	gloov1 "github.com/solo-io/solo-apis/pkg/api/gateway.solo.io/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type glooV1Client struct {
	rtClient *routeTableClient
	vsClient *virtualServiceClient
}

type GlooV1ClientSet interface {
	RouteTables() RouteTableClient
	VirtualServices() VirtualServiceClient
}

type RouteTableClient interface {
	RouteTableReader
	RouteTableWriter
}

type routeTableClient struct {
	client k8sclient.Client
}

type VirtualServiceClient interface {
	VirtualServiceReader
	VirtualServiceWriter
}

type virtualServiceClient struct {
	client k8sclient.Client
}

type RouteTableReader interface {
	GetRouteTable(ctx context.Context, name string, namespace string) *gloov1.RouteTable
}

type RouteTableWriter interface {
	PatchRouteTable(ctx context.Context, obj *gloov1.RouteTable, patch k8sclient.Patch, opts ...k8sclient.PatchOption) *gloov1.RouteTable
}

type VirtualServiceReader interface {
	GetVirtualService(ctx context.Context, name string, namespace string) *gloov1.VirtualService
}

type VirtualServiceWriter interface {
	PatchVirtualService(ctx context.Context, obj *gloov1.VirtualService, patch k8sclient.Patch, opts ...k8sclient.PatchOption) *gloov1.VirtualService
}

func NewGlooV1ClientSet() (GlooV1ClientSet, error) {
	cfg, err := util.GetKubeConfig()
	if err != nil {
		return nil, err
	}

	scheme := runtime.NewScheme()
	gloov1.AddToScheme(scheme)

	c, err := k8sclient.New(cfg, k8sclient.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, err
	}

	return glooV1Client{
		rtClient: &routeTableClient{
			client: c,
		},
		vsClient: &virtualServiceClient{
			client: c,
		},
	}, nil
}

func (c glooV1Client) RouteTables() RouteTableClient {
	return c.rtClient
}

func (c glooV1Client) VirtualServices() VirtualServiceClient {
	return c.vsClient
}

func (c routeTableClient) GetRouteTable(ctx context.Context, name string, namespace string) *gloov1.RouteTable {
	panic("not impl")
}

func (c routeTableClient) PatchRouteTable(ctx context.Context, obj *gloov1.RouteTable, patch k8sclient.Patch, opts ...k8sclient.PatchOption) *gloov1.RouteTable {
	panic("not impl")
}

func (c virtualServiceClient) GetVirtualService(ctx context.Context, name string, namespace string) *gloov1.VirtualService {
	panic("not impl")
}

func (c virtualServiceClient) PatchVirtualService(ctx context.Context, obj *gloov1.VirtualService, patch k8sclient.Patch, opts ...k8sclient.PatchOption) *gloov1.VirtualService {
	panic("not impl")
}
