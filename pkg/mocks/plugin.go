package mocks

import (
	"context"

	"github.com/argoproj-labs/rollouts-plugin-trafficrouter-glooedge/pkg/gloo"
	gloov1 "github.com/solo-io/solo-apis/pkg/api/gateway.solo.io/v1"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	RouteTableName       = "mock"
	RouteTableNamespace  = "mock"
	DestinationKind      = "SERVICE"
	DestinationNamespace = "mock"
	StableService        = "stable"
	CanaryService        = "canary"
	RolloutNamespace     = "mock"
	RolloutName          = "mock"
)

func NewGlooMockClient() gloo.GlooV1ClientSet {
	return &GlooMockClient{
		rtClient: &glooMockRouteTableClient{},
		vsClient: &glooMockVirtualServiceClient{},
	}
}

type GlooMockClient struct {
	rtClient *glooMockRouteTableClient
	vsClient *glooMockVirtualServiceClient
}

func (c GlooMockClient) RouteTables() gloo.RouteTableClient {
	panic("not impl")
}

func (c GlooMockClient) VirtualServices() gloo.VirtualServiceClient {
	panic("not impl")
}

type glooMockRouteTableClient struct {
}

func (c glooMockRouteTableClient) GetRouteTable(ctx context.Context, name string, namespace string) *gloov1.RouteTable {
	panic("not impl")
}

func (c glooMockRouteTableClient) PatchRouteTable(ctx context.Context, obj *gloov1.RouteTable, patch k8sclient.Patch, opts ...k8sclient.PatchOption) *gloov1.RouteTable {
	panic("not impl")
}

type glooMockVirtualServiceClient struct {
}

func (c glooMockVirtualServiceClient) GetVirtualService(ctx context.Context, name string, namespace string) *gloov1.VirtualService {
	panic("not impl")
}

func (c glooMockVirtualServiceClient) PatchVirtualService(ctx context.Context, obj *gloov1.VirtualService, patch k8sclient.Patch, opts ...k8sclient.PatchOption) *gloov1.VirtualService {
	panic("not impl")
}

// var RouteTable = networkv2.RouteTable{
// 	ObjectMeta: metav1.ObjectMeta{
// 		Name:      RouteTableName,
// 		Namespace: RouteTableNamespace,
// 	},
// 	Spec: networkv2.RouteTableSpec{
// 		Hosts: []string{"*"},

// 		Http: []*networkv2.HTTPRoute{
// 			{
// 				Name: RouteTableName,
// 				ActionType: &networkv2.HTTPRoute_ForwardTo{
// 					ForwardTo: &networkv2.ForwardToAction{
// 						Destinations: []*commonv2.DestinationReference{
// 							{
// 								Kind: commonv2.DestinationKind_SERVICE,
// 								Port: &commonv2.PortSelector{
// 									Specifier: &commonv2.PortSelector_Number{
// 										Number: 8000,
// 									},
// 								},
// 								RefKind: &commonv2.DestinationReference_Ref{
// 									Ref: &commonv2.ObjectReference{
// 										Name:      StableService,
// 										Namespace: DestinationNamespace,
// 									},
// 								},
// 							},
// 						},
// 					},
// 				},
// 			},
// 		},
// 	},
// }
