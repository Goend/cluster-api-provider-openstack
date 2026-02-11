package extensions

import (
	"context"
	"fmt"

	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/attributestags"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/routers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	"k8s.io/utils/ptr"

	infrav1 "sigs.k8s.io/cluster-api-provider-openstack/api/v1beta1"
	"sigs.k8s.io/cluster-api-provider-openstack/pkg/clients"
	"sigs.k8s.io/cluster-api-provider-openstack/pkg/cloud/services/networking"
	"sigs.k8s.io/cluster-api-provider-openstack/pkg/scope"
	"sigs.k8s.io/cluster-api-provider-openstack/pkg/utils/names"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

const (
	routerInterfaceOwner = "network:router_interface"
)

type vpcCniWarmupConfig struct {
	NetworkName     string
	SubnetCIDR      string
	RouterID        string
	Tags            []string
}

type vpcCniResources struct {
	networkID string
	subnetID  string
}

// ReconcileVpcCniSecurityGroup ensures the VPC CNI security group exists and is fully open.
func ReconcileVpcCniSecurityGroup(ctx context.Context, scope *scope.WithLogger, cluster *clusterv1.Cluster, osc *infrav1.OpenStackCluster) (string, error) {
	_ = ctx
	_ = osc
	clusterName := names.ClusterResourceName(cluster)
	networkClient, err := scope.NewNetworkClient()
	if err != nil {
		return "", err
	}

	secGroupName := fmt.Sprintf("%s-vpc-cni-secgroup", clusterName)
	secGroup, err := ensureVpcCniSecurityGroup(networkClient, secGroupName)
	if err != nil {
		return "", err
	}
	networkingService, err := networking.NewService(scope)
	if err != nil {
		return "", err
	}
	if err := networkingService.EnsureAllowAllSecurityGroupRules(secGroup.ID); err != nil {
		return "", err
	}
	return secGroup.ID, nil
}

// ReconcileVpcCniNetworking ensures the VPC CNI 网络、子网、路由接口已经完成。
func ReconcileVpcCniNetworking(ctx context.Context, scope *scope.WithLogger, cluster *clusterv1.Cluster, osc *infrav1.OpenStackCluster) (string, error) {
	if osc.Spec.Extensions == nil || osc.Spec.Extensions.Networking == nil {
		return "", nil
	}

	cfg, err := buildVpcCniConfig(cluster, osc)
	if err != nil {
		return "", err
	}

	networkClient, err := scope.NewNetworkClient()
	if err != nil {
		return "", err
	}

	resources, err := ensureVpcCniNetworkStack(ctx, scope, networkClient, cfg)
	if err != nil {
		return "", err
	}

	return resources.subnetID, nil
}

func buildVpcCniConfig(cluster *clusterv1.Cluster, osc *infrav1.OpenStackCluster) (vpcCniWarmupConfig, error) {
	baseName := names.ClusterResourceName(cluster)
	cfg := vpcCniWarmupConfig{
		NetworkName: baseName + "-vpc-cni",
		Tags:        DeduplicateStrings(append([]string{}, osc.Spec.Tags...), "vpc-cni", baseName),
	}

	if cluster == nil || len(cluster.Spec.ClusterNetwork.Pods.CIDRBlocks) == 0 {
		return cfg, fmt.Errorf("clusterNetwork.pods.cidrBlocks 未配置，无法计算 VPC CNI 子网")
	}
	cfg.SubnetCIDR = cluster.Spec.ClusterNetwork.Pods.CIDRBlocks[0]
	if cfg.SubnetCIDR == "" {
		return cfg, fmt.Errorf("clusterNetwork.pods.cidrBlocks 为空，无法计算 VPC CNI 子网")
	}

	if osc.Status.Router != nil {
		cfg.RouterID = osc.Status.Router.ID
	}
	if cfg.RouterID == "" {
		return cfg, fmt.Errorf("router ID 未就绪，无法完成 VPC CNI 路由绑定")
	}

	return cfg, nil
}

func ensureVpcCniNetworkStack(ctx context.Context, scope *scope.WithLogger, networkClient clients.NetworkClient, cfg vpcCniWarmupConfig) (*vpcCniResources, error) {
	resource := &vpcCniResources{}

	networkID, err := ensureNetwork(ctx, scope, networkClient, cfg)
	if err != nil {
		return nil, err
	}
	resource.networkID = networkID

	subnetID, err := ensureSubnet(ctx, scope, networkClient, cfg, networkID)
	if err != nil {
		return nil, err
	}
	resource.subnetID = subnetID

	if err := ensureRouterInterface(networkClient, cfg.RouterID, subnetID); err != nil {
		return nil, err
	}

	return resource, nil
}

func ensureNetwork(_ context.Context, scope *scope.WithLogger, networkClient clients.NetworkClient, cfg vpcCniWarmupConfig) (string, error) {
	listOpts := &networks.ListOpts{
		Name: cfg.NetworkName,
	}
	existing, err := networkClient.ListNetwork(listOpts)
	if err != nil {
		return "", fmt.Errorf("查询 VPC CNI 网络 %q 失败: %w", cfg.NetworkName, err)
	}
	switch len(existing) {
	case 1:
		return existing[0].ID, nil
	case 0:
	default:
		return "", fmt.Errorf("找到多个名为 %q 的网络，无法继续", cfg.NetworkName)
	}

	net, err := networkClient.CreateNetwork(networks.CreateOpts{
		AdminStateUp: ptr.To(true),
		Name:         cfg.NetworkName,
	})
	if err != nil {
		return "", fmt.Errorf("创建网络 %q 失败: %w", cfg.NetworkName, err)
	}

	if len(cfg.Tags) > 0 {
		if _, err := networkClient.ReplaceAllAttributesTags("networks", net.ID, attributestags.ReplaceAllOpts{Tags: cfg.Tags}); err != nil {
			return "", fmt.Errorf("更新网络 %s 标签失败: %w", net.ID, err)
		}
	}
	scope.Logger().Info("已创建 VPC CNI 网络", "name", cfg.NetworkName, "id", net.ID)
	return net.ID, nil
}

func ensureSubnet(_ context.Context, scope *scope.WithLogger, networkClient clients.NetworkClient, cfg vpcCniWarmupConfig, networkID string) (string, error) {
	listOpts := &subnets.ListOpts{
		NetworkID: networkID,
		CIDR:      cfg.SubnetCIDR,
	}
	subnetsList, err := networkClient.ListSubnet(listOpts)
	if err != nil {
		return "", fmt.Errorf("查询子网 %s 失败: %w", cfg.SubnetCIDR, err)
	}
	switch len(subnetsList) {
	case 1:
		return subnetsList[0].ID, nil
	case 0:
	default:
		return "", fmt.Errorf("发现多个 CIDR=%s 的子网，请检查配置", cfg.SubnetCIDR)
	}

	name := fmt.Sprintf("%s-subnet", cfg.NetworkName)
	sn, err := networkClient.CreateSubnet(subnets.CreateOpts{
		NetworkID:   networkID,
		Name:        name,
		IPVersion:   4,
		CIDR:        cfg.SubnetCIDR,
		EnableDHCP:  ptr.To(false),
		Description: fmt.Sprintf("VPC CNI subnet for %s", cfg.NetworkName),
	})
	if err != nil {
		return "", fmt.Errorf("创建 VPC CNI 子网失败: %w", err)
	}
	scope.Logger().Info("已创建 VPC CNI 子网", "name", name, "cidr", cfg.SubnetCIDR, "id", sn.ID)
	return sn.ID, nil
}

func ensureVpcCniSecurityGroup(networkClient clients.NetworkClient, name string) (*groups.SecGroup, error) {
	existing, err := networkClient.ListSecGroup(groups.ListOpts{Name: name})
	if err != nil {
		return nil, fmt.Errorf("查询 VPC CNI 安全组失败: %w", err)
	}
	switch len(existing) {
	case 0:
	case 1:
		return &existing[0], nil
	default:
		return nil, fmt.Errorf("找到多个名为 %q 的安全组，无法继续", name)
	}

	group, err := networkClient.CreateSecGroup(groups.CreateOpts{
		Name:        name,
		Description: fmt.Sprintf("VPC CNI security group for %s", name),
	})
	if err != nil {
		return nil, fmt.Errorf("创建 VPC CNI 安全组失败: %w", err)
	}
	return group, nil
}

func ensureRouterInterface(networkClient clients.NetworkClient, routerID, subnetID string) error {
	if routerID == "" || subnetID == "" {
		return nil
	}
	portsByRouter, err := networkClient.ListPort(&ports.ListOpts{
		DeviceID:    routerID,
		DeviceOwner: routerInterfaceOwner,
	})
	if err != nil {
		return fmt.Errorf("查询路由 %s 接口失败: %w", routerID, err)
	}
	for _, port := range portsByRouter {
		for _, ip := range port.FixedIPs {
			if ip.SubnetID == subnetID {
				return nil
			}
		}
	}
	if _, err := networkClient.AddRouterInterface(routerID, routers.AddInterfaceOpts{SubnetID: subnetID}); err != nil {
		return fmt.Errorf("路由 %s 添加子网 %s 接口失败: %w", routerID, subnetID, err)
	}
	return nil
}
