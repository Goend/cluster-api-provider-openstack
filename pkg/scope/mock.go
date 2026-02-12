/*
Copyright 2022 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package scope

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"
	"go.uber.org/mock/gomock"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "sigs.k8s.io/cluster-api-provider-openstack/api/v1beta1"
	"sigs.k8s.io/cluster-api-provider-openstack/pkg/clients"
	"sigs.k8s.io/cluster-api-provider-openstack/pkg/clients/mock"
)

// MockScopeFactory implements both the ScopeFactory and ClientScope interfaces. It can be used in place of the default ProviderScopeFactory
// when we want to use mocked service clients which do not attempt to connect to a running OpenStack cloud.
type MockScopeFactory struct {
	ComputeClient *mock.MockComputeClient
	NetworkClient *mock.MockNetworkClient
	VolumeClient  *mock.MockVolumeClient
	ImageClient   *mock.MockImageClient
	LbClient      *mock.MockLbClient

	projectID              string
	clientScopeCreateError error
	serviceEndpoints       map[string]string
}

func NewMockScopeFactory(mockCtrl *gomock.Controller, projectID string) *MockScopeFactory {
	computeClient := mock.NewMockComputeClient(mockCtrl)
	volumeClient := mock.NewMockVolumeClient(mockCtrl)
	imageClient := mock.NewMockImageClient(mockCtrl)
	networkClient := mock.NewMockNetworkClient(mockCtrl)
	lbClient := mock.NewMockLbClient(mockCtrl)

	return &MockScopeFactory{
		ComputeClient: computeClient,
		VolumeClient:  volumeClient,
		ImageClient:   imageClient,
		NetworkClient: networkClient,
		LbClient:      lbClient,
		projectID:     projectID,
		serviceEndpoints: map[string]string{},
	}
}

func (f *MockScopeFactory) SetClientScopeCreateError(err error) {
	f.clientScopeCreateError = err
}

func (f *MockScopeFactory) SetServiceEndpoint(service, endpoint string) {
	if f.serviceEndpoints == nil {
		f.serviceEndpoints = map[string]string{}
	}
	f.serviceEndpoints[service] = endpoint
}

func (f *MockScopeFactory) NewClientScopeFromObject(_ context.Context, _ client.Client, _ []byte, _ logr.Logger, _ ...infrav1.IdentityRefProvider) (Scope, error) {
	if f.clientScopeCreateError != nil {
		return nil, f.clientScopeCreateError
	}
	return f, nil
}

func (f *MockScopeFactory) NewComputeClient() (clients.ComputeClient, error) {
	return f.ComputeClient, nil
}

func (f *MockScopeFactory) NewVolumeClient() (clients.VolumeClient, error) {
	return f.VolumeClient, nil
}

func (f *MockScopeFactory) NewImageClient() (clients.ImageClient, error) {
	return f.ImageClient, nil
}

func (f *MockScopeFactory) NewNetworkClient() (clients.NetworkClient, error) {
	return f.NetworkClient, nil
}

func (f *MockScopeFactory) NewLbClient() (clients.LbClient, error) {
	return f.LbClient, nil
}

func (f *MockScopeFactory) NewIdentityClient() (*gophercloud.ServiceClient, error) {
	return nil, ErrIdentityClientUnavailable
}

func (f *MockScopeFactory) ServiceEndpoint(service string) (string, error) {
	if f.serviceEndpoints == nil {
		return "", nil
	}
	return f.serviceEndpoints[service], nil
}

func (f *MockScopeFactory) ProjectID() string {
	return f.projectID
}

func (f *MockScopeFactory) ExtractToken() (*tokens.Token, error) {
	return &tokens.Token{ExpiresAt: time.Now().Add(24 * time.Hour)}, nil
}

func (f *MockScopeFactory) AuthResult() gophercloud.AuthResult {
	return nil
}

func (f *MockScopeFactory) IdentityEndpoint() string {
	return ""
}

func (f *MockScopeFactory) RegionName() string {
	return ""
}
