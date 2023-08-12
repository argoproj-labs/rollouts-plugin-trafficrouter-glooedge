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

type VirtualServiceCanarySuite struct {
	suite.Suite
	plugin     *RpcPlugin
	ctrl       *gomock.Controller
	ctx        context.Context
	gwclient   *gloov1.MockClientset
	vsclient   *gloov1.MockVirtualServiceClient
	loggerHook *test.Hook
}

func (s *VirtualServiceCanarySuite) SetupTest() {
	s.ctx = context.TODO()
	s.ctrl = gomock.NewController(s.T())
	s.gwclient = gloov1.NewMockClientset(s.ctrl)
	s.vsclient = gloov1.NewMockVirtualServiceClient(s.ctrl)
	var testLogger *logrus.Logger
	// see https://github.com/mpchadwick/dbanon/blob/v0.6.0/src/provider_test.go#L39-L42
	// for example of how to use the hook in tests
	testLogger, s.loggerHook = test.NewNullLogger()
	s.plugin = &RpcPlugin{Client: s.gwclient, LogCtx: testLogger.WithContext(s.ctx)}
}

func TestVirtualServiceCanarySuite(t *testing.T) {
	suite.Run(t, new(VirtualServiceCanarySuite))
}

func (s *VirtualServiceCanarySuite) Test_getVirtualService() {
	expectedNs := "test-ns"
	expectedName := "test-vs"
	s.vsclient.EXPECT().GetVirtualService(gomock.Any(),
		gomock.Eq(client.ObjectKey{Namespace: expectedNs, Name: expectedName})).Times(1).Return(&gwv1.VirtualService{}, nil)
	s.gwclient.EXPECT().VirtualServices().Return(s.vsclient).Times(1)

	vs, err := s.plugin.getVirtualService(s.ctx, &v1alpha1.Rollout{},
		&GlooEdgeTrafficRouting{VirtualServiceSelector: &DumbObjectSelector{Namespace: expectedNs, Name: expectedName}})

	assert.NoError(s.T(), err)
	assert.NotNil(s.T(), vs)
}

func (s *VirtualServiceCanarySuite) Test_getVirtualService_UsesRolloutNS() {
	expectedNs := "rollout-ns"
	expectedName := "test-vs"
	s.vsclient.EXPECT().GetVirtualService(gomock.Any(),
		gomock.Eq(client.ObjectKey{Namespace: expectedNs, Name: expectedName})).Times(1).Return(&gwv1.VirtualService{}, nil)
	s.gwclient.EXPECT().VirtualServices().Return(s.vsclient).Times(1)

	rollout := &v1alpha1.Rollout{}
	rollout.SetNamespace(expectedNs)
	rollout.SetName("test-rollout")

	vs, err := s.plugin.getVirtualService(s.ctx, rollout,
		&GlooEdgeTrafficRouting{VirtualServiceSelector: &DumbObjectSelector{Namespace: "", Name: expectedName}})

	assert.NoError(s.T(), err)
	assert.NotNil(s.T(), vs)
}

func (s *VirtualServiceCanarySuite) Test_getVirtualService_ReturnsErrorWhenNameIsMissing() {
	expectedNs := "test-ns"
	expectedName := ""

	_, err := s.plugin.getVirtualService(s.ctx, &v1alpha1.Rollout{},
		&GlooEdgeTrafficRouting{VirtualServiceSelector: &DumbObjectSelector{Namespace: expectedNs, Name: expectedName}})

	assert.Error(s.T(), err, fmt.Errorf("must specify the name of the VirtualService"))
}

func (s *VirtualServiceCanarySuite) Test_getDestinationsInVirtualService() {
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

	dsts, err := s.plugin.getDestinationsInVirtualService(
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

func (s *VirtualServiceCanarySuite) Test_getDestinationsInVirtualService_MultipleRoutes() {
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

	dsts, err := s.plugin.getDestinationsInVirtualService(
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
func (s *VirtualServiceCanarySuite) Test_getDestinationsInVirtualService_ReturnsErrorWithMissingVhOrRoutes() {
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
			_, err := s.plugin.getDestinationsInVirtualService(
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

// multiple routes present in VS, but none specified in s.plugin config
func (s *VirtualServiceCanarySuite) Test_getDestinationsInVirtualService_MissingPluginConfig() {
	vs := gwv1.VirtualService{
		Spec: gwv1.VirtualServiceSpec{
			VirtualHost: &gwv1.VirtualHost{
				Routes: []*gwv1.Route{
					{Name: "route-1"},
					{Name: "route-2"},
				}}}}

	_, err := s.plugin.getDestinationsInVirtualService(
		&v1alpha1.Rollout{Spec: v1alpha1.RolloutSpec{}},
		&GlooEdgeTrafficRouting{Routes: []string{}}, &vs)

	assert.Error(s.T(), err)
	assert.Contains(s.T(), err.Error(),
		"virtual host has multiple routes but canary config doesn't specify which routes to use")
}

// multiple routes specified in s.plugin config, but not all are present in VS
func (s *VirtualServiceCanarySuite) Test_getDestinationsInVirtualService_MissingRoutes() {
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

	_, err := s.plugin.getDestinationsInVirtualService(
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

func (s *VirtualServiceCanarySuite) Test_getDestinationsInVirtualService_MissingCanaryOrStableUpstream() {
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

	_, err := s.plugin.getDestinationsInVirtualService(
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

func (s *VirtualServiceCanarySuite) Test_handleCanary_UsingVirtualService() {

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
	s.vsclient.EXPECT().GetVirtualService(gomock.Any(),
		gomock.Eq(client.ObjectKey{Namespace: testns, Name: testvs})).Times(1).Return(vs, nil)
	// used in getVS() and handleCanary()
	s.gwclient.EXPECT().VirtualServices().Return(s.vsclient).Times(2)
	// used in handleCanary()
	s.vsclient.EXPECT().PatchVirtualService(
		gomock.Any(),
		gomock.Eq(&expectedVs),
		gomock.Any())

	filterConfig, err := json.Marshal(GlooEdgeTrafficRouting{
		Routes: []string{route1, route2},
		VirtualServiceSelector: &DumbObjectSelector{
			Namespace: testns, Name: testvs,
		}})
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
	assert.Equal(s.T(), &expectedVs, vs)
}

// getVirtualService() returns an error
func (s *VirtualServiceCanarySuite) Test_handleCanary_ReturnErrorIfGetVirtualServiceReturnsError() {

	// used in getVS()
	s.vsclient.EXPECT().GetVirtualService(gomock.Any(), gomock.Any()).Times(1).Return(nil, fmt.Errorf("boom"))
	// used in getVS()
	s.gwclient.EXPECT().VirtualServices().Return(s.vsclient).Times(1)

	err := s.plugin.handleCanaryUsingVirtualService(s.ctx,
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
func (s *VirtualServiceCanarySuite) Test_handleCanary_ReturnErrorIfGetDestionationsReturnsError() {
	vs := &gwv1.VirtualService{
		Spec: gwv1.VirtualServiceSpec{},
	}

	// used in getVS()
	s.vsclient.EXPECT().GetVirtualService(gomock.Any(), gomock.Any()).Times(1).Return(vs, nil)
	// used in getVS() and handleCanary()
	s.gwclient.EXPECT().VirtualServices().Return(s.vsclient).Times(1)

	err := s.plugin.handleCanaryUsingVirtualService(s.ctx,
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
