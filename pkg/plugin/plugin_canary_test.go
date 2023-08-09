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
	loggerHook *test.Hook
)

func (s *PluginCanarySuite) SetupTest() {
	ctx = context.TODO()
	ctrl = gomock.NewController(s.T())
	gwclient = gloov1.NewMockClientset(ctrl)
	vsclient = gloov1.NewMockVirtualServiceClient(ctrl)
	var testLogger *logrus.Logger
	// see https://github.com/mpchadwick/dbanon/blob/v0.6.0/src/provider_test.go#L39-L42
	// for example of how to use the hook in tests
	testLogger, loggerHook = test.NewNullLogger()
	plugin = &RpcPlugin{Client: gwclient, LogCtx: testLogger.WithContext(ctx)}
}

// func (s *PluginCanarySuite) TearDownTest() {
// fmt.Printf("XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX")
// defer ctrl.Finish()
// }

func TestPluginCanarySuite(t *testing.T) {
	suite.Run(t, new(PluginCanarySuite))
}

func (s *PluginCanarySuite) Test_getVirtualService() {
	expectedNs := "test-ns"
	expectedName := "test-vs"
	vsclient.EXPECT().GetVirtualService(gomock.Any(),
		gomock.Eq(client.ObjectKey{Namespace: expectedNs, Name: expectedName})).Times(1).Return(&gwv1.VirtualService{}, nil)
	gwclient.EXPECT().VirtualServices().Return(vsclient).Times(1)

	vs, err := plugin.getVS(ctx, &v1alpha1.Rollout{},
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

	vs, err := plugin.getVS(ctx, rollout,
		&GlooEdgeTrafficRouting{VirtualServiceSelector: &DumbObjectSelector{Namespace: "", Name: expectedName}})

	assert.NoError(s.T(), err)
	assert.NotNil(s.T(), vs)
}

func (s *PluginCanarySuite) Test_getVirtualService_ReturnsErrorWhenNameIsMissing() {
	expectedNs := "test-ns"
	expectedName := ""

	_, err := plugin.getVS(ctx, &v1alpha1.Rollout{},
		&GlooEdgeTrafficRouting{VirtualServiceSelector: &DumbObjectSelector{Namespace: expectedNs, Name: expectedName}})

	assert.Error(s.T(), err, fmt.Errorf("must specify the name of the VirtualService"))
}

func (s *PluginCanarySuite) Test_getDestinations() {
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

	stable, canary, err := plugin.getDestinations(ctx,
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
	assert.Equal(s.T(), expectedStableDst, stable)
	assert.Equal(s.T(), expectedCanaryDst, canary)
}

// verify behaviour when VirtualHost or Routes are missing in a VirtualService
func (s *PluginCanarySuite) Test_getDestinations_ReturnsErrorWithMissingVhOrRoutes() {
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
			_, _, err := plugin.getDestinations(ctx,
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

// Verify behaviour when any of RouteAction/MultiDestination/WeightedDestination/Destination/Upstream are missing
func (s *PluginCanarySuite) Test_getDestinations_SkipsWhenDestinationIsMissingComponents() {
	type errorTestCases struct {
		description string
		vs          *gwv1.VirtualService
	}

	for _, test := range []errorTestCases{
		{
			description: "missing RouteAction",
			vs: &gwv1.VirtualService{Spec: gwv1.VirtualServiceSpec{
				VirtualHost: &gwv1.VirtualHost{
					Routes: []*gwv1.Route{{}}},
			}},
		},
		{
			description: "missing MultiDestination",
			vs: &gwv1.VirtualService{Spec: gwv1.VirtualServiceSpec{
				VirtualHost: &gwv1.VirtualHost{
					Routes: []*gwv1.Route{{
						Action: &gwv1.Route_RouteAction{
							RouteAction: &v1.RouteAction{}},
					}}},
			}},
		},
		{
			description: "missing WeightedDestination",
			vs: &gwv1.VirtualService{Spec: gwv1.VirtualServiceSpec{
				VirtualHost: &gwv1.VirtualHost{
					Routes: []*gwv1.Route{{
						Action: &gwv1.Route_RouteAction{
							RouteAction: &v1.RouteAction{
								Destination: &v1.RouteAction_Multi{
									Multi: &v1.MultiDestination{},
								},
							}},
					}}},
			}},
		},
		{
			description: "missing Destination",
			vs: &gwv1.VirtualService{Spec: gwv1.VirtualServiceSpec{
				VirtualHost: &gwv1.VirtualHost{
					Routes: []*gwv1.Route{{
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
					}}},
			}},
		},
		{
			description: "missing Upstream",
			vs: &gwv1.VirtualService{Spec: gwv1.VirtualServiceSpec{
				VirtualHost: &gwv1.VirtualHost{
					Routes: []*gwv1.Route{{
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
					}}},
			}},
		},
	} {
		s.T().Run(test.description, func(t *testing.T) {
			stable, canary, err := plugin.getDestinations(ctx,
				&v1alpha1.Rollout{},
				&GlooEdgeTrafficRouting{
					VirtualServiceSelector: &DumbObjectSelector{
						Namespace: "test",
						Name:      "test",
					}},
				test.vs)
			assert.NoError(s.T(), err)
			assert.Nil(s.T(), stable)
			assert.Nil(s.T(), canary)
		})
	}
}

func (s *PluginCanarySuite) Test_handleCanary() {

	testns := "testns"
	testvs := "testvs"
	canarysvc := "canarysvc"
	stablesvc := "stablesvc"
	desiredWeight := int32(40)

	vs := &gwv1.VirtualService{
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
															Name: stablesvc,
														},
													},
												},
												Weight: wrapperspb.UInt32(uint32(90)),
											},
											{
												Destination: &v1.Destination{
													DestinationType: &v1.Destination_Upstream{
														Upstream: &core.ResourceRef{
															Name: canarysvc,
														},
													},
												},
												Weight: wrapperspb.UInt32(uint32(10)),
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
	expectedVs.Spec.GetVirtualHost().
		GetRoutes()[0].GetRouteAction().GetMulti().
		GetDestinations()[0].Weight = &wrapperspb.UInt32Value{Value: uint32(100 - desiredWeight)}
	expectedVs.Spec.GetVirtualHost().
		GetRoutes()[0].GetRouteAction().GetMulti().
		GetDestinations()[1].Weight = &wrapperspb.UInt32Value{Value: uint32(desiredWeight)}

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
			VirtualServiceSelector: &DumbObjectSelector{
				Namespace: testns, Name: testvs,
			},
		})

	assert.NoError(s.T(), err)
	assert.Equal(s.T(), &expectedVs, vs)
}

// verify the behaviour when stable service cannot be found in VS
func (s *PluginCanarySuite) Test_handleCanary_ReturnErrorIfStableServiceNotInVs() {
	vs := &gwv1.VirtualService{
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
															Name: "canary",
														},
													},
												},
												Weight: wrapperspb.UInt32(uint32(10)),
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
	assert.Contains(s.T(), err.Error(), "couldn't find stable or canary subsets in VirtualService")
}

// verify the behaviour when canary service cannot be found in VS
func (s *PluginCanarySuite) Test_handleCanary_ReturnErrorIfCanaryServiceNotInVs() {
	vs := &gwv1.VirtualService{
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
															Name: "stable",
														},
													},
												},
												Weight: wrapperspb.UInt32(uint32(10)),
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
	assert.Contains(s.T(), err.Error(), "couldn't find stable or canary subsets in VirtualService")
}

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
