package plugin

import (
	"context"
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

type PluginCanarySuite struct {
	suite.Suite
}

var (
	plugin     *RpcPlugin
	ctrl       *gomock.Controller
	ctx        context.Context
	gwclient   *gloov1.MockClientset
	vsclient   *gloov1.MockVirtualServiceClient
	rtclient   *gloov1.MockRouteTableClient
	loggerHook *test.Hook
)

func (s *PluginCanarySuite) SetupTest() {
	ctx = context.TODO()
	ctrl = gomock.NewController(s.T())
	gwclient = gloov1.NewMockClientset(ctrl)
	vsclient = gloov1.NewMockVirtualServiceClient(ctrl)
	rtclient = gloov1.NewMockRouteTableClient(ctrl)
	var testLogger *logrus.Logger
	// see https://github.com/mpchadwick/dbanon/blob/v0.6.0/src/provider_test.go#L39-L42
	// for example of how to use the hook in tests
	testLogger, loggerHook = test.NewNullLogger()
	plugin = &RpcPlugin{Client: gwclient, LogCtx: testLogger.WithContext(ctx)}
}

func TestPluginCanarySuite(t *testing.T) {
	suite.Run(t, new(PluginCanarySuite))
}

func (s *PluginCanarySuite) Test_getVirtualService() {
	expectedNs := "test-ns"
	expectedName := "test-vs"
	vsclient.EXPECT().GetVirtualService(gomock.Any(),
		gomock.Eq(client.ObjectKey{Namespace: expectedNs, Name: expectedName})).Times(1).Return(&gwv1.VirtualService{}, nil)
	gwclient.EXPECT().VirtualServices().Return(vsclient).Times(1)

	vs, err := plugin.getVirtualService(ctx, &v1alpha1.Rollout{},
		&GlooEdgeTrafficRouting{VirtualServiceSelector: &DumbObjectSelector{Namespace: expectedNs, Name: expectedName}})

	assert.NoError(s.T(), err)
	assert.NotNil(s.T(), vs)
}

func (s *PluginCanarySuite) Test_getVirtualService_UsesRolloutNS() {
	expectedNs := "rollout-ns"
	expectedName := "test-vs"
	vsclient.EXPECT().GetVirtualService(gomock.Any(),
		gomock.Eq(client.ObjectKey{Namespace: expectedNs, Name: expectedName})).Times(1).Return(&gwv1.VirtualService{}, nil)
	gwclient.EXPECT().VirtualServices().Return(vsclient).Times(1)

	rollout := &v1alpha1.Rollout{}
	rollout.SetNamespace(expectedNs)
	rollout.SetName("test-rollout")

	vs, err := plugin.getVirtualService(ctx, rollout,
		&GlooEdgeTrafficRouting{VirtualServiceSelector: &DumbObjectSelector{Namespace: "", Name: expectedName}})

	assert.NoError(s.T(), err)
	assert.NotNil(s.T(), vs)
}

func (s *PluginCanarySuite) Test_getVirtualService_ReturnsErrorWhenNameIsMissing() {
	expectedNs := "test-ns"
	expectedName := ""

	_, err := plugin.getVirtualService(ctx, &v1alpha1.Rollout{},
		&GlooEdgeTrafficRouting{VirtualServiceSelector: &DumbObjectSelector{Namespace: expectedNs, Name: expectedName}})

	assert.Error(s.T(), err, fmt.Errorf("must specify the name of the VirtualService"))
}

func (s *PluginCanarySuite) Test_getDestinationsInVirtualService() {
	stableUpstream := "stable-upstream"
	canaryUpstream := "canary-upstream"

	expectedStableDst := &v1.WeightedDestination{
		Destination: &v1.Destination{
			DestinationType: &v1.Destination_Upstream{
				Upstream: &core.ResourceRef{
					Name: stableUpstream,
				},
			},
		},
		Weight: wrapperspb.UInt32(uint32(90)),
	}
	expectedCanaryDst := &v1.WeightedDestination{
		Destination: &v1.Destination{
			DestinationType: &v1.Destination_Upstream{
				Upstream: &core.ResourceRef{
					Name: canaryUpstream,
				},
			},
		},
		Weight: wrapperspb.UInt32(uint32(10)),
	}

	vs := gwv1.VirtualService{
		Spec: gwv1.VirtualServiceSpec{
			VirtualHost: &gwv1.VirtualHost{
				Routes: []*gwv1.Route{
					{
						Action: &gwv1.Route_RouteAction{
							RouteAction: &v1.RouteAction{
								Destination: &v1.RouteAction_Multi{
									Multi: &v1.MultiDestination{
										Destinations: []*v1.WeightedDestination{
											expectedStableDst,
											expectedCanaryDst,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	dsts, err := plugin.getDestinationsInVirtualService(
		&v1alpha1.Rollout{Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					CanaryService: canaryUpstream,
					StableService: stableUpstream,
				},
			},
		}},
		&GlooEdgeTrafficRouting{}, &vs)

	assert.NoError(s.T(), err)
	assert.Len(s.T(), dsts, 1)
	assert.Equal(s.T(), expectedStableDst, dsts[0].Stable)
	assert.Equal(s.T(), expectedCanaryDst, dsts[0].Canary)
}

func (s *PluginCanarySuite) Test_getDestinationsInVirtualService_MultipleRoutes() {
	stableUpstream := "stable-upstream"
	canaryUpstream := "canary-upstream"

	expectedStableDst1 := &v1.WeightedDestination{
		Destination: &v1.Destination{
			DestinationType: &v1.Destination_Upstream{
				Upstream: &core.ResourceRef{
					Name: stableUpstream,
				},
			},
		},
		Weight: wrapperspb.UInt32(uint32(90)),
	}
	expectedStableDst2 := &v1.WeightedDestination{
		Destination: &v1.Destination{
			DestinationType: &v1.Destination_Upstream{
				Upstream: &core.ResourceRef{
					Name: stableUpstream,
				},
			},
		},
		Weight: wrapperspb.UInt32(uint32(80)),
	}
	expectedCanaryDst1 := &v1.WeightedDestination{
		Destination: &v1.Destination{
			DestinationType: &v1.Destination_Upstream{
				Upstream: &core.ResourceRef{
					Name: canaryUpstream,
				},
			},
		},
		Weight: wrapperspb.UInt32(uint32(10)),
	}
	expectedCanaryDst2 := &v1.WeightedDestination{
		Destination: &v1.Destination{
			DestinationType: &v1.Destination_Upstream{
				Upstream: &core.ResourceRef{
					Name: canaryUpstream,
				},
			},
		},
		Weight: wrapperspb.UInt32(uint32(20)),
	}

	vs := gwv1.VirtualService{
		Spec: gwv1.VirtualServiceSpec{
			VirtualHost: &gwv1.VirtualHost{
				Routes: []*gwv1.Route{
					{
						Name: "route-1",
						Action: &gwv1.Route_RouteAction{
							RouteAction: &v1.RouteAction{
								Destination: &v1.RouteAction_Multi{
									Multi: &v1.MultiDestination{
										Destinations: []*v1.WeightedDestination{
											expectedStableDst1,
											expectedCanaryDst1,
										},
									},
								},
							},
						},
					},
					{
						Name: "route-2",
						Action: &gwv1.Route_RouteAction{
							RouteAction: &v1.RouteAction{
								Destination: &v1.RouteAction_Multi{
									Multi: &v1.MultiDestination{
										Destinations: []*v1.WeightedDestination{
											expectedStableDst2,
											expectedCanaryDst2,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	dsts, err := plugin.getDestinationsInVirtualService(
		&v1alpha1.Rollout{Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					CanaryService: canaryUpstream,
					StableService: stableUpstream,
				},
			},
		}},
		&GlooEdgeTrafficRouting{Routes: []string{"route-1", "route-2"}}, &vs)

	assert.NoError(s.T(), err)
	assert.Len(s.T(), dsts, 2)
	assert.Equal(s.T(), expectedStableDst1, dsts[0].Stable)
	assert.Equal(s.T(), expectedCanaryDst1, dsts[0].Canary)
	assert.Equal(s.T(), expectedStableDst2, dsts[1].Stable)
	assert.Equal(s.T(), expectedCanaryDst2, dsts[1].Canary)
}

// VirtualHost or Routes are missing from the VirtualService
func (s *PluginCanarySuite) Test_getDestinationsInVirtualService_ReturnsErrorWithMissingVhOrRoutes() {
	type errorTestCases struct {
		description string
		vs          *gwv1.VirtualService
	}

	for _, test := range []errorTestCases{
		{
			description: "missing VH",
			vs:          &gwv1.VirtualService{Spec: gwv1.VirtualServiceSpec{}},
		},
		{
			description: "missing routes",
			vs: &gwv1.VirtualService{
				Spec: gwv1.VirtualServiceSpec{
					VirtualHost: &gwv1.VirtualHost{},
				},
			},
		},
	} {
		s.T().Run(test.description, func(t *testing.T) {
			_, err := plugin.getDestinationsInVirtualService(
				&v1alpha1.Rollout{},
				&GlooEdgeTrafficRouting{
					VirtualServiceSelector: &DumbObjectSelector{
						Namespace: "test",
						Name:      "test",
					}},
				test.vs)
			assert.Error(s.T(), err)
			assert.Contains(s.T(), err.Error(), "no virtual host or empty routes in VirtualSevice")
		})
	}
}

// multiple routes present in VS, but none specified in plugin config
func (s *PluginCanarySuite) Test_getDestinationsInVirtualService_MissingPluginConfig() {
	vs := gwv1.VirtualService{
		Spec: gwv1.VirtualServiceSpec{
			VirtualHost: &gwv1.VirtualHost{
				Routes: []*gwv1.Route{
					{Name: "route-1"},
					{Name: "route-2"},
				}}}}

	_, err := plugin.getDestinationsInVirtualService(
		&v1alpha1.Rollout{Spec: v1alpha1.RolloutSpec{}},
		&GlooEdgeTrafficRouting{Routes: []string{}}, &vs)

	assert.Error(s.T(), err)
	assert.Contains(s.T(), err.Error(),
		"virtual host has multiple routes but canary config doesn't specify which routes to use")
}

// multiple routes specified in plugin config, but not all are present in VS
func (s *PluginCanarySuite) Test_getDestinationsInVirtualService_MissingRoutes() {
	stableUpstream := "stable-upstream"
	canaryUpstream := "canary-upstream"

	expectedStableDst1 := &v1.WeightedDestination{
		Destination: &v1.Destination{
			DestinationType: &v1.Destination_Upstream{
				Upstream: &core.ResourceRef{
					Name: stableUpstream,
				},
			},
		},
		Weight: wrapperspb.UInt32(uint32(90)),
	}
	expectedCanaryDst1 := &v1.WeightedDestination{
		Destination: &v1.Destination{
			DestinationType: &v1.Destination_Upstream{
				Upstream: &core.ResourceRef{
					Name: canaryUpstream,
				},
			},
		},
		Weight: wrapperspb.UInt32(uint32(10)),
	}

	vs := gwv1.VirtualService{
		Spec: gwv1.VirtualServiceSpec{
			VirtualHost: &gwv1.VirtualHost{
				Routes: []*gwv1.Route{
					{
						Name: "route-1",
						Action: &gwv1.Route_RouteAction{
							RouteAction: &v1.RouteAction{
								Destination: &v1.RouteAction_Multi{
									Multi: &v1.MultiDestination{
										Destinations: []*v1.WeightedDestination{
											expectedStableDst1,
											expectedCanaryDst1,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	_, err := plugin.getDestinationsInVirtualService(
		&v1alpha1.Rollout{Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					CanaryService: canaryUpstream,
					StableService: stableUpstream,
				},
			},
		}},
		&GlooEdgeTrafficRouting{Routes: []string{"route-1", "route-2"}}, &vs)

	assert.Error(s.T(), err)
	assert.Contains(s.T(), err.Error(),
		"some/all routes specified in canary rollout configuration do not have stable upstreams")
}

func (s *PluginCanarySuite) Test_getDestinationsInVirtualService_MissingCanaryOrStableUpstream() {
	vs := gwv1.VirtualService{
		Spec: gwv1.VirtualServiceSpec{
			VirtualHost: &gwv1.VirtualHost{
				Routes: []*gwv1.Route{
					{
						Action: &gwv1.Route_RouteAction{
							RouteAction: &v1.RouteAction{
								Destination: &v1.RouteAction_Multi{
									Multi: &v1.MultiDestination{
										Destinations: []*v1.WeightedDestination{
											{
												Destination: &v1.Destination{
													DestinationType: &v1.Destination_Upstream{
														Upstream: &core.ResourceRef{
															Name: "unexpected",
														},
													},
												},
												Weight: wrapperspb.UInt32(uint32(90)),
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
	}

	_, err := plugin.getDestinationsInVirtualService(
		&v1alpha1.Rollout{Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					StableService: "stable",
					CanaryService: "canary",
				},
			},
		}},
		&GlooEdgeTrafficRouting{
			VirtualServiceSelector: &DumbObjectSelector{Name: "testvs", Namespace: "testns"}}, &vs)

	assert.Error(s.T(), err)
	assert.Contains(s.T(), err.Error(),
		"couldn't find stable upstreams in VirtualService testns/testvs, with route names in []")
}

func (s *PluginCanarySuite) Test_getDestinationsInRoutes() {
	stableUpstreamName := "stable-upstream"
	canaryUpstreamName := "canary-upstream"

	routes := []*gwv1.Route{
		{
			Name: "route-1",
			Action: &gwv1.Route_RouteAction{
				RouteAction: &v1.RouteAction{
					Destination: &v1.RouteAction_Multi{
						Multi: &v1.MultiDestination{
							Destinations: []*v1.WeightedDestination{
								{Destination: &v1.Destination{
									DestinationType: &v1.Destination_Upstream{
										Upstream: &core.ResourceRef{
											Name: stableUpstreamName,
										},
									},
								}},
								{Destination: &v1.Destination{
									DestinationType: &v1.Destination_Upstream{
										Upstream: &core.ResourceRef{
											Name: canaryUpstreamName,
										},
									},
								}},
							},
						},
					},
				}},
		},
		{
			Name: "route-2",
			Action: &gwv1.Route_RouteAction{
				RouteAction: &v1.RouteAction{
					Destination: &v1.RouteAction_Multi{
						Multi: &v1.MultiDestination{
							Destinations: []*v1.WeightedDestination{
								{Destination: &v1.Destination{
									DestinationType: &v1.Destination_Upstream{
										Upstream: &core.ResourceRef{
											Name: stableUpstreamName,
										},
									},
								}},
							},
						},
					},
				}},
		},
		{
			Name: "route-3",
			Action: &gwv1.Route_RouteAction{
				RouteAction: &v1.RouteAction{
					Destination: &v1.RouteAction_Multi{
						Multi: &v1.MultiDestination{
							Destinations: []*v1.WeightedDestination{
								{Destination: &v1.Destination{
									DestinationType: &v1.Destination_Upstream{
										Upstream: &core.ResourceRef{
											Name: stableUpstreamName,
										},
									},
								}},
							},
						},
					},
				}},
		},
	}

	dsts := plugin.getDestinationsInRoutes(
		routes,
		&v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					Canary: &v1alpha1.CanaryStrategy{
						CanaryService: canaryUpstreamName,
						StableService: stableUpstreamName,
					},
				},
			},
		},
		&GlooEdgeTrafficRouting{
			Routes: []string{"route-1", "route-2"},
		})

	assert.Len(s.T(), dsts, 2)
	assert.Equal(s.T(), routes[0].GetRouteAction().GetMulti().GetDestinations(), dsts[0].DestinationsParent.Destinations)
	assert.Equal(s.T(), routes[0].GetRouteAction().GetMulti().GetDestinations()[0], dsts[0].Stable)
	assert.Equal(s.T(), routes[0].GetRouteAction().GetMulti().GetDestinations()[1], dsts[0].Canary)

	assert.Equal(s.T(), routes[1].GetRouteAction().GetMulti().GetDestinations(), dsts[1].DestinationsParent.Destinations)
	assert.Equal(s.T(), routes[1].GetRouteAction().GetMulti().GetDestinations()[0], dsts[1].Stable)
	assert.Nil(s.T(), dsts[1].Canary)
}

// skip route if any of RouteAction/MultiDestination/WeightedDestination/Destination/Upstream are missing
// skip route if either canary or stable upstreams are missing
// skip route if its name isn't in the list of routes (GlooEdgeTrafficRouting.Routes)
//
// (TODO) dmitri-d It's very easy to get a false pass for this test
// the order of tests mimicks the order of checks in getDestinationsInRoutes
// to make it more managable
func (s *PluginCanarySuite) Test_getDestinationsInRoutes_SkipsWhenDestinationIsMissingComponents() {
	type errorTestCases struct {
		description string
		routes      []*gwv1.Route
	}

	stableUpstreamName := "stable-upstream"
	canaryUpstreamName := "canary-upstream"
	routeInList := "expected-route"

	for _, test := range []errorTestCases{
		{
			description: "missing RouteAction",
			routes:      []*gwv1.Route{{Name: routeInList}},
		},
		{
			description: "missing MultiDestination",
			routes: []*gwv1.Route{{
				Name: routeInList,
				Action: &gwv1.Route_RouteAction{
					RouteAction: &v1.RouteAction{}},
			}},
		},
		{
			description: "missing WeightedDestination",
			routes: []*gwv1.Route{{
				Name: routeInList,
				Action: &gwv1.Route_RouteAction{
					RouteAction: &v1.RouteAction{
						Destination: &v1.RouteAction_Multi{
							Multi: &v1.MultiDestination{},
						},
					}},
			}},
		},
		{
			description: "missing Destination",
			routes: []*gwv1.Route{{
				Name: routeInList,
				Action: &gwv1.Route_RouteAction{
					RouteAction: &v1.RouteAction{
						Destination: &v1.RouteAction_Multi{
							Multi: &v1.MultiDestination{
								Destinations: []*v1.WeightedDestination{
									{},
								},
							},
						},
					}},
			}},
		},
		{
			description: "missing Upstream",
			routes: []*gwv1.Route{{
				Name: routeInList,
				Action: &gwv1.Route_RouteAction{
					RouteAction: &v1.RouteAction{
						Destination: &v1.RouteAction_Multi{
							Multi: &v1.MultiDestination{
								Destinations: []*v1.WeightedDestination{
									{Destination: &v1.Destination{}},
								},
							},
						},
					}},
			}},
		},
		{
			description: "missing stable Upstream",
			routes: []*gwv1.Route{{
				Name: routeInList,
				Action: &gwv1.Route_RouteAction{
					RouteAction: &v1.RouteAction{
						Destination: &v1.RouteAction_Multi{
							Multi: &v1.MultiDestination{
								Destinations: []*v1.WeightedDestination{
									{Destination: &v1.Destination{
										DestinationType: &v1.Destination_Upstream{
											Upstream: &core.ResourceRef{
												Name: canaryUpstreamName,
											},
										},
									}},
								},
							},
						},
					}},
			}},
		},
		{
			description: "Route name isn't in the list of routes in 'GlooEdgeTrafficRouting.Routes'",
			routes: []*gwv1.Route{{
				Name: "not-in-the-list",
			}},
		},
	} {
		s.T().Run(test.description, func(t *testing.T) {
			dsts := plugin.getDestinationsInRoutes(
				test.routes,
				&v1alpha1.Rollout{
					Spec: v1alpha1.RolloutSpec{
						Strategy: v1alpha1.RolloutStrategy{
							Canary: &v1alpha1.CanaryStrategy{
								CanaryService: canaryUpstreamName,
								StableService: stableUpstreamName,
							},
						},
					},
				},
				&GlooEdgeTrafficRouting{
					Routes: []string{routeInList},
				})
			assert.Len(s.T(), dsts, 0, test.description)
		})
	}
}

func (s *PluginCanarySuite) Test_maybeCreateCanaryDestinations() {
	canarysvc := "canarysvc"
	stablesvc := "stablesvc"

	rts := []routeTableWithDestinations{
		{
			Destinations: []destinationPair{
				{
					DestinationsParent: &v1.MultiDestination{
						Destinations: []*v1.WeightedDestination{},
					},
					Stable: &v1.WeightedDestination{
						Destination: &v1.Destination{
							DestinationType: &v1.Destination_Upstream{
								Upstream: &core.ResourceRef{
									Name: stablesvc,
								},
							},
						},
						Weight: wrapperspb.UInt32(uint32(90)),
					},
					Canary: nil,
				},
				{
					DestinationsParent: &v1.MultiDestination{
						Destinations: []*v1.WeightedDestination{},
					},
					Stable: &v1.WeightedDestination{
						Destination: &v1.Destination{
							DestinationType: &v1.Destination_Upstream{
								Upstream: &core.ResourceRef{
									Name: stablesvc,
								},
							},
						},
					},
					Canary: nil,
				},
			},
		},
		{
			Destinations: []destinationPair{
				{
					DestinationsParent: &v1.MultiDestination{
						Destinations: []*v1.WeightedDestination{},
					},
					Stable: &v1.WeightedDestination{
						Destination: &v1.Destination{
							DestinationType: &v1.Destination_Upstream{
								Upstream: &core.ResourceRef{
									Name: stablesvc,
								},
							},
						},
						Weight: wrapperspb.UInt32(uint32(90)),
					},
					Canary: nil,
				},
				{
					DestinationsParent: &v1.MultiDestination{
						Destinations: []*v1.WeightedDestination{},
					},
					Stable: &v1.WeightedDestination{
						Destination: &v1.Destination{
							DestinationType: &v1.Destination_Upstream{
								Upstream: &core.ResourceRef{
									Name: stablesvc,
								},
							},
						},
					},
					Canary: nil,
				},
			},
		},
	}

	plugin.maybeCreateCanaryDestinations(rts, canarysvc)

	for i := 0; i < 2; i++ {
		for j := 0; j < 2; j++ {
			assert.Len(s.T(), rts[i].Destinations[j].DestinationsParent.GetDestinations(), 1)
			assert.NotNil(s.T(), rts[i].Destinations[j].Canary)
			assert.Equal(s.T(), canarysvc, rts[i].Destinations[j].Canary.GetDestination().GetUpstream().GetName())
			assert.Equal(s.T(), rts[i].Destinations[j].Canary,
				rts[i].Destinations[j].DestinationsParent.GetDestinations()[0])
		}
	}
}

func (s *PluginCanarySuite) Test_handleCanary_UsingVirtualService() {

	testns := "testns"
	testvs := "testvs"
	canarysvc := "canarysvc"
	stablesvc := "stablesvc"
	desiredWeight := int32(40)
	route1 := "route-1"
	route2 := "route-2"

	vs := &gwv1.VirtualService{
		Spec: gwv1.VirtualServiceSpec{
			VirtualHost: &gwv1.VirtualHost{
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
						// this route will remain unchanged
						Name: "route3",
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
	}

	originalVs := gwv1.VirtualService{}
	vs.DeepCopyInto(&originalVs)
	expectedVs := gwv1.VirtualService{}
	vs.DeepCopyInto(&expectedVs)
	expectedVs.Spec.GetVirtualHost().GetRoutes()[0].GetRouteAction().GetMulti().Destinations =
		append(expectedVs.Spec.GetVirtualHost().GetRoutes()[0].GetRouteAction().GetMulti().Destinations,
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

	for _, route := range expectedVs.Spec.GetVirtualHost().GetRoutes() {
		if route.GetName() == "route3" {
			continue
		}
		route.GetRouteAction().GetMulti().GetDestinations()[0].Weight =
			&wrapperspb.UInt32Value{Value: uint32(100 - desiredWeight)}
		route.GetRouteAction().GetMulti().GetDestinations()[1].Weight =
			&wrapperspb.UInt32Value{Value: uint32(desiredWeight)}
	}

	// used in getVS()
	vsclient.EXPECT().GetVirtualService(gomock.Any(),
		gomock.Eq(client.ObjectKey{Namespace: testns, Name: testvs})).Times(1).Return(vs, nil)
	// used in getVS() and handleCanary()
	gwclient.EXPECT().VirtualServices().Return(vsclient).Times(2)
	// used in handleCanary()
	vsclient.EXPECT().PatchVirtualService(
		gomock.Any(),
		gomock.Eq(&expectedVs),
		gomock.Any())

	err := plugin.handleCanary(ctx,
		&v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					Canary: &v1alpha1.CanaryStrategy{
						CanaryService: canarysvc,
						StableService: stablesvc,
					},
				},
			},
		},
		desiredWeight,
		[]v1alpha1.WeightDestination{},
		&GlooEdgeTrafficRouting{
			Routes: []string{route1, route2},
			VirtualServiceSelector: &DumbObjectSelector{
				Namespace: testns, Name: testvs,
			},
		})

	assert.NoError(s.T(), err)
	assert.Equal(s.T(), &expectedVs, vs)
}

// getVirtualService() returns an error
func (s *PluginCanarySuite) Test_handleCanary_ReturnErrorIfGetVirtualServiceReturnsError() {

	// used in getVS()
	vsclient.EXPECT().GetVirtualService(gomock.Any(), gomock.Any()).Times(1).Return(nil, fmt.Errorf("boom"))
	// used in getVS()
	gwclient.EXPECT().VirtualServices().Return(vsclient).Times(1)

	err := plugin.handleCanary(ctx,
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
		[]v1alpha1.WeightDestination{},
		&GlooEdgeTrafficRouting{
			VirtualServiceSelector: &DumbObjectSelector{
				Namespace: "testns", Name: "testvs",
			},
		})

	assert.Error(s.T(), err, "boom")
}

// getDestinationsInVirtualService() returns an error
func (s *PluginCanarySuite) Test_handleCanary_ReturnErrorIfGetDestionationsReturnsError() {
	vs := &gwv1.VirtualService{
		Spec: gwv1.VirtualServiceSpec{},
	}

	// used in getVS()
	vsclient.EXPECT().GetVirtualService(gomock.Any(), gomock.Any()).Times(1).Return(vs, nil)
	// used in getVS() and handleCanary()
	gwclient.EXPECT().VirtualServices().Return(vsclient).Times(1)

	err := plugin.handleCanary(ctx,
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
		[]v1alpha1.WeightDestination{},
		&GlooEdgeTrafficRouting{
			VirtualServiceSelector: &DumbObjectSelector{
				Namespace: "testns", Name: "testvs",
			},
		})

	assert.Error(s.T(), err)
	assert.Contains(s.T(), err.Error(), "no virtual host or empty routes in VirtualSevice")
}

func (s *PluginCanarySuite) Test_handleCanary_UsingRouteTables() {

	testns := "testns"
	canarysvc := "canarysvc"
	stablesvc := "stablesvc"
	desiredWeight := int32(40)
	route1 := "route-1"
	route2 := "route-2"
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
							// this route will remain unchanged
							Name: "route3",
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

	for _, rt := range expectedRts {
		for _, route := range rt.Spec.GetRoutes() {
			if route.GetName() == "route3" {
				continue
			}

			route.GetRouteAction().GetMulti().GetDestinations()[0].Weight =
				&wrapperspb.UInt32Value{Value: uint32(100 - desiredWeight)}
			route.GetRouteAction().GetMulti().GetDestinations()[1].Weight =
				&wrapperspb.UInt32Value{Value: uint32(desiredWeight)}
		}
	}

	// used in getRouteTables()
	rtclient.EXPECT().ListRouteTable(gomock.Any(),
		gomock.Eq(client.MatchingLabels(labels)),
		gomock.Eq(client.InNamespace(testns))).Times(1).
		Return(routeTableList, nil)
	// used in getRouteTable() and handleCanaryUsingRouteTables()
	gwclient.EXPECT().RouteTables().Return(rtclient).Times(3)
	// used in handleCanaryUsingRouteTables()
	rtclient.EXPECT().PatchRouteTable(
		gomock.Any(),
		gomock.Eq(expectedRts[0]),
		gomock.Any()).Times(1)

	rtclient.EXPECT().PatchRouteTable(
		gomock.Any(),
		gomock.Eq(expectedRts[1]),
		gomock.Any()).Times(1)

	err := plugin.handleCanary(ctx,
		&v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					Canary: &v1alpha1.CanaryStrategy{
						CanaryService: canarysvc,
						StableService: stablesvc,
					},
				},
			},
		},
		desiredWeight,
		[]v1alpha1.WeightDestination{},
		&GlooEdgeTrafficRouting{
			Routes: []string{route1, route2},
			RouteTableSelector: &DumbObjectSelector{
				Namespace: testns,
				Labels:    labels},
		},
	)

	assert.NoError(s.T(), err)
	for i := range routeTableList.Items {
		assert.Equal(s.T(), expectedRts[i], &routeTableList.Items[i], fmt.Sprintf("items at index %d aren't equal", i))
	}
}

// getVirtualService() returns an error
func (s *PluginCanarySuite) Test_handleCanaryUsingRouteTables_ReturnErrorIfGetRouteTablesReturnsError() {

	// used in getRouteTable()
	rtclient.EXPECT().GetRouteTable(gomock.Any(), gomock.Any()).Times(1).Return(nil, fmt.Errorf("boom"))
	// used in getRouteTable()
	gwclient.EXPECT().RouteTables().Return(rtclient).Times(1)

	err := plugin.handleCanary(ctx,
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
		[]v1alpha1.WeightDestination{},
		&GlooEdgeTrafficRouting{
			RouteTableSelector: &DumbObjectSelector{
				Namespace: "testns", Name: "testvs",
			},
		})

	assert.Error(s.T(), err, "boom")
}

func (s *PluginCanarySuite) Test_handleCanaryUsingRouteTables_ReturnErrorIfGetDestionationsReturnsError() {
	vs := &gwv1.RouteTable{
		Spec: gwv1.RouteTableSpec{},
	}

	// used in getVS()
	rtclient.EXPECT().GetRouteTable(gomock.Any(), gomock.Any()).Times(1).Return(vs, nil)
	// used in getRouteTable()
	gwclient.EXPECT().RouteTables().Return(rtclient).Times(1)

	err := plugin.handleCanary(ctx,
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
		[]v1alpha1.WeightDestination{},
		&GlooEdgeTrafficRouting{
			RouteTableSelector: &DumbObjectSelector{
				Namespace: "testns", Name: "testvs",
			},
		})

	assert.Error(s.T(), err)
	assert.Contains(s.T(), err.Error(), "couldn't find stable services in RouteTables selected")
}
