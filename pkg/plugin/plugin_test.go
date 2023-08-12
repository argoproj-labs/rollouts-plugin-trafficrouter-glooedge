package plugin

import (
	"context"
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/golang/mock/gomock"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	gwv1 "github.com/solo-io/solo-apis/pkg/api/gateway.solo.io/v1"
	gloov1 "github.com/solo-io/solo-apis/pkg/api/gateway.solo.io/v1/mocks"
	v1 "github.com/solo-io/solo-apis/pkg/api/gloo.solo.io/v1"
	"github.com/solo-io/solo-kit/pkg/api/v1/resources/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type PluginSuite struct {
	suite.Suite
	plugin     *RpcPlugin
	ctrl       *gomock.Controller
	ctx        context.Context
	gwclient   *gloov1.MockClientset
	loggerHook *test.Hook
}

func (s *PluginSuite) SetupTest() {
	s.ctx = context.TODO()
	s.ctrl = gomock.NewController(s.T())
	s.gwclient = gloov1.NewMockClientset(s.ctrl)
	var testLogger *logrus.Logger
	// see https://github.com/mpchadwick/dbanon/blob/v0.6.0/src/provider_test.go#L39-L42
	// for example of how to use the hook in tests
	testLogger, s.loggerHook = test.NewNullLogger()
	s.plugin = &RpcPlugin{Client: s.gwclient, LogCtx: testLogger.WithContext(s.ctx)}
}

func TestPluginSuite(t *testing.T) {
	suite.Run(t, new(PluginSuite))
}

func (s *PluginSuite) Test_getDestinationsInRoutes() {
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

	dsts := s.plugin.getDestinationsInRoutes(
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
func (s *PluginSuite) Test_getDestinationsInRoutes_SkipsWhenDestinationIsMissingComponents() {
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
			dsts := s.plugin.getDestinationsInRoutes(
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

func (s *PluginSuite) Test_maybeCreateCanaryDestinations() {
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

	s.plugin.maybeCreateCanaryDestinations(rts, canarysvc)

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
