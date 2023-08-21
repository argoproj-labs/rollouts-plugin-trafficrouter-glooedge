package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/golang/mock/gomock"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gwv1 "github.com/solo-io/solo-apis/pkg/api/gateway.solo.io/v1"
	gloov1 "github.com/solo-io/solo-apis/pkg/api/gateway.solo.io/v1/mocks"
	v1 "github.com/solo-io/solo-apis/pkg/api/gloo.solo.io/v1"
	"github.com/solo-io/solo-kit/pkg/api/v1/resources/core"
)

type RouteTableCanarySuite struct {
	suite.Suite
	plugin     *RpcPlugin
	ctrl       *gomock.Controller
	ctx        context.Context
	gwclient   *gloov1.MockClientset
	rtclient   *gloov1.MockRouteTableClient
	loggerHook *test.Hook
}

func (s *RouteTableCanarySuite) SetupTest() {
	s.ctx = context.TODO()
	s.ctrl = gomock.NewController(s.T())
	s.gwclient = gloov1.NewMockClientset(s.ctrl)
	s.rtclient = gloov1.NewMockRouteTableClient(s.ctrl)
	var testLogger *logrus.Logger
	// see https://github.com/mpchadwick/dbanon/blob/v0.6.0/src/provider_test.go#L39-L42
	// for example of how to use the hook in tests
	testLogger, s.loggerHook = test.NewNullLogger()
	s.plugin = &RpcPlugin{Client: s.gwclient, LogCtx: testLogger.WithContext(s.ctx)}
}

func TestRouteTableCanarySuite(t *testing.T) {
	suite.Run(t, new(RouteTableCanarySuite))
}

func (s *RouteTableCanarySuite) Test_handleCanary_UsingRouteTables() {

	testns := "testns"
	canarysvc := "canarysvc"
	stablesvc := "stablesvc"
	desiredWeight := int32(40)
	route1 := "route-1"
	route2 := "route-2"
	route3 := "route-3"
	labels := map[string]string{"label": "test-label"}

	routeTableList := &gwv1.RouteTableList{
		Items: []gwv1.RouteTable{
			{
				Spec: gwv1.RouteTableSpec{
					Routes: []*gwv1.Route{
						{
							Name: route1,
							Action: &gwv1.Route_RouteAction{
								RouteAction: &v1.RouteAction{
									Destination: &v1.RouteAction_Multi{
										Multi: &v1.MultiDestination{
											Destinations: []*v1.WeightedDestination{
												{
													Destination: &v1.Destination{
														DestinationType: &v1.Destination_Upstream{
															Upstream: &core.ResourceRef{
																Name: stablesvc,
															},
														},
													},
													Weight: wrapperspb.UInt32(uint32(90)),
												},
												// canary destination will be created
											},
										},
									},
								},
							},
						},
						{
							Name: route2,
							Action: &gwv1.Route_RouteAction{
								RouteAction: &v1.RouteAction{
									Destination: &v1.RouteAction_Multi{
										Multi: &v1.MultiDestination{
											Destinations: []*v1.WeightedDestination{
												{
													Destination: &v1.Destination{
														DestinationType: &v1.Destination_Upstream{
															Upstream: &core.ResourceRef{
																Name: stablesvc,
															},
														},
													},
													Weight: wrapperspb.UInt32(uint32(80)),
												},
												{
													Destination: &v1.Destination{
														DestinationType: &v1.Destination_Upstream{
															Upstream: &core.ResourceRef{
																Name: canarysvc,
															},
														},
													},
													Weight: wrapperspb.UInt32(uint32(20)),
												},
											},
										},
									},
								},
							},
						},
						{
							// single destination will be converted to multi
							Name: route3,
							Action: &gwv1.Route_RouteAction{
								RouteAction: &v1.RouteAction{
									Destination: &v1.RouteAction_Single{
										Single: &v1.Destination{
											DestinationType: &v1.Destination_Upstream{
												Upstream: &core.ResourceRef{
													Name: stablesvc,
												},
											},
										},
									},
								},
							},
						},
						{
							// this route will remain unchanged
							Name: "route4",
							Action: &gwv1.Route_RouteAction{
								RouteAction: &v1.RouteAction{
									Destination: &v1.RouteAction_Multi{
										Multi: &v1.MultiDestination{
											Destinations: []*v1.WeightedDestination{
												{
													Destination: &v1.Destination{
														DestinationType: &v1.Destination_Upstream{
															Upstream: &core.ResourceRef{
																Name: "not-changing",
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			{
				Spec: gwv1.RouteTableSpec{
					Routes: []*gwv1.Route{
						{
							Name: route2,
							Action: &gwv1.Route_RouteAction{
								RouteAction: &v1.RouteAction{
									Destination: &v1.RouteAction_Multi{
										Multi: &v1.MultiDestination{
											Destinations: []*v1.WeightedDestination{
												{
													Destination: &v1.Destination{
														DestinationType: &v1.Destination_Upstream{
															Upstream: &core.ResourceRef{
																Name: stablesvc,
															},
														},
													},
													Weight: wrapperspb.UInt32(uint32(90)),
												},
												// canary destination will be created
											},
										},
									},
								},
							},
						},
						{
							Name: route1,
							Action: &gwv1.Route_RouteAction{
								RouteAction: &v1.RouteAction{
									Destination: &v1.RouteAction_Multi{
										Multi: &v1.MultiDestination{
											Destinations: []*v1.WeightedDestination{
												{
													Destination: &v1.Destination{
														DestinationType: &v1.Destination_Upstream{
															Upstream: &core.ResourceRef{
																Name: stablesvc,
															},
														},
													},
													Weight: wrapperspb.UInt32(uint32(80)),
												},
												{
													Destination: &v1.Destination{
														DestinationType: &v1.Destination_Upstream{
															Upstream: &core.ResourceRef{
																Name: canarysvc,
															},
														},
													},
													Weight: wrapperspb.UInt32(uint32(20)),
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}}

	expectedRts := make([]*gwv1.RouteTable, len(routeTableList.Items))
	for i := range routeTableList.Items {
		expected := &gwv1.RouteTable{}
		routeTableList.Items[i].DeepCopyInto(expected)
		expectedRts[i] = expected
	}

	expectedRts[0].Spec.GetRoutes()[0].GetRouteAction().GetMulti().Destinations =
		append(expectedRts[0].Spec.GetRoutes()[0].GetRouteAction().GetMulti().Destinations,
			&v1.WeightedDestination{
				Destination: &v1.Destination{
					DestinationType: &v1.Destination_Upstream{
						Upstream: &core.ResourceRef{
							Name: canarysvc,
						},
					},
				},
				Weight: wrapperspb.UInt32(uint32(20)),
			})
	expectedRts[1].Spec.GetRoutes()[0].GetRouteAction().GetMulti().Destinations =
		append(expectedRts[1].Spec.GetRoutes()[0].GetRouteAction().GetMulti().Destinations,
			&v1.WeightedDestination{
				Destination: &v1.Destination{
					DestinationType: &v1.Destination_Upstream{
						Upstream: &core.ResourceRef{
							Name: canarysvc,
						},
					},
				},
				Weight: wrapperspb.UInt32(uint32(20)),
			})
	expectedRts[0].Spec.GetRoutes()[2].GetRouteAction().Destination =
		&v1.RouteAction_Multi{
			Multi: &v1.MultiDestination{
				Destinations: []*v1.WeightedDestination{
					{
						Destination: expectedRts[0].Spec.GetRoutes()[2].GetRouteAction().GetSingle(),
						Weight:      wrapperspb.UInt32(uint32(100 - desiredWeight)),
					},
					{
						Destination: &v1.Destination{
							DestinationType: &v1.Destination_Upstream{
								Upstream: &core.ResourceRef{
									Name: canarysvc,
								},
							},
						},
						Weight: wrapperspb.UInt32(uint32(desiredWeight)),
					},
				},
			},
		}

	for _, rt := range expectedRts {
		for _, route := range rt.Spec.GetRoutes() {
			if route.GetName() == "route4" {
				continue
			}

			route.GetRouteAction().GetMulti().GetDestinations()[0].Weight =
				&wrapperspb.UInt32Value{Value: uint32(100 - desiredWeight)}
			route.GetRouteAction().GetMulti().GetDestinations()[1].Weight =
				&wrapperspb.UInt32Value{Value: uint32(desiredWeight)}
		}
	}

	// used in getRouteTables()
	s.rtclient.EXPECT().ListRouteTable(gomock.Any(),
		gomock.Eq(client.MatchingLabels(labels)),
		gomock.Eq(client.InNamespace(testns))).Times(1).
		Return(routeTableList, nil)
	// used in getRouteTable() and handleCanaryUsingRouteTables()
	s.gwclient.EXPECT().RouteTables().Return(s.rtclient).Times(3)
	// used in handleCanaryUsingRouteTables()
	s.rtclient.EXPECT().PatchRouteTable(
		gomock.Any(),
		gomock.Eq(expectedRts[0]),
		gomock.Any()).Times(1)

	s.rtclient.EXPECT().PatchRouteTable(
		gomock.Any(),
		gomock.Eq(expectedRts[1]),
		gomock.Any()).Times(1)

	filterConfig, err := json.Marshal(GlooEdgeTrafficRouting{
		Routes: []string{route1, route2, route3},
		RouteTableSelector: &DumbObjectSelector{
			Namespace: testns,
			Labels:    labels},
	})
	assert.NoError(s.T(), err)

	err = s.plugin.SetWeight(
		&v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					Canary: &v1alpha1.CanaryStrategy{
						TrafficRouting: &v1alpha1.RolloutTrafficRouting{
							Plugins: map[string]json.RawMessage{
								PluginName: filterConfig,
							},
						},
						CanaryService: canarysvc,
						StableService: stablesvc,
					},
				},
			},
		},
		desiredWeight,
		[]v1alpha1.WeightDestination{})

	assert.Empty(s.T(), err.Error())
	for i := range routeTableList.Items {
		assert.Equal(s.T(), expectedRts[i], &routeTableList.Items[i], fmt.Sprintf("items at index %d aren't equal", i))
	}
}

// getVirtualService() returns an error
func (s *RouteTableCanarySuite) Test_handleCanaryUsingRouteTables_ReturnErrorIfGetRouteTablesReturnsError() {

	// used in getRouteTable()
	s.rtclient.EXPECT().GetRouteTable(gomock.Any(), gomock.Any()).Times(1).Return(nil, fmt.Errorf("boom"))
	// used in getRouteTable()
	s.gwclient.EXPECT().RouteTables().Return(s.rtclient).Times(1)

	err := s.plugin.handleCanaryUsingRouteTables(s.ctx,
		&v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					Canary: &v1alpha1.CanaryStrategy{
						CanaryService: "canary",
						StableService: "stable",
					},
				},
			},
		},
		42,
		&GlooEdgeTrafficRouting{
			RouteTableSelector: &DumbObjectSelector{
				Namespace: "testns", Name: "testvs",
			},
		})

	assert.Error(s.T(), err, "boom")
}

func (s *RouteTableCanarySuite) Test_handleCanaryUsingRouteTables_ReturnErrorIfGetDestionationsReturnsError() {
	vs := &gwv1.RouteTable{
		Spec: gwv1.RouteTableSpec{},
	}

	// used in getVS()
	s.rtclient.EXPECT().GetRouteTable(gomock.Any(), gomock.Any()).Times(1).Return(vs, nil)
	// used in getRouteTable()
	s.gwclient.EXPECT().RouteTables().Return(s.rtclient).Times(1)

	err := s.plugin.handleCanaryUsingRouteTables(s.ctx,
		&v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					Canary: &v1alpha1.CanaryStrategy{
						CanaryService: "canary",
						StableService: "stable",
					},
				},
			},
		},
		42,
		&GlooEdgeTrafficRouting{
			RouteTableSelector: &DumbObjectSelector{
				Namespace: "testns", Name: "testvs",
			},
		})

	assert.Error(s.T(), err)
	assert.Contains(s.T(), err.Error(), "couldn't find stable services in RouteTables selected")
}
