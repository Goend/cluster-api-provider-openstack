package controllers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"text/template"

	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/applicationcredentials"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	infrav1 "sigs.k8s.io/cluster-api-provider-openstack/api/v1beta1"
	"sigs.k8s.io/cluster-api-provider-openstack/pkg/cloud/services/networking"
	exthelpers "sigs.k8s.io/cluster-api-provider-openstack/pkg/extensions"
	"sigs.k8s.io/cluster-api-provider-openstack/pkg/scope"
	"sigs.k8s.io/cluster-api-provider-openstack/pkg/utils/names"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

const (
	clusterConfigNamespace    = "ems"
	clusterConfigName         = "clusterconfig"
	clusterConfigKind         = "Config"
	appCredentialSecretSuffix = "openstack-app-cred"
)

var (
	serviceCatalogConfigsGVR = schema.GroupVersionResource{
		Group:    "servicecatalog.ecp.com",
		Version:  "v1",
		Resource: "configs",
	}
	clusterConfigPublicVIPPath = []string{"data", "cluster_attrs", "public_vip"}
)

const authTemplate = `clouds:
  {{.ClusterName}}:
    identity_api_version: 3
    auth:
      auth_url: {{.AuthURL}}
      application_credential_id: {{.AppCredID}}
      application_credential_secret: {{.AppCredSecret}}
    region_name: {{.Region}}
`

type authConfig struct {
	ClusterName   string
	AuthURL       string
	AppCredID     string
	AppCredSecret string
	Region        string
}

func (r *OpenStackClusterReconciler) reconcileClusterExtensions(ctx context.Context, scope *scope.WithLogger, cluster *clusterv1.Cluster, osc *infrav1.OpenStackCluster) error {
	_ = ctx
	_ = cluster

	ext := ensureClusterExtensionsStatus(osc)
	if err := r.reconcileClusterLoadBalancers(ctx, scope, cluster, ext, osc); err != nil {
		return err
	}
	if err := reconcileClusterNetworking(ctx, scope, cluster, ext, osc); err != nil {
		return err
	}
	reconcileClusterPlatform(ext, osc)
	reconcileClusterEndpoints(scope, ext, osc)
	if err := r.reconcileClusterAppCredential(ctx, scope, cluster, ext, osc); err != nil {
		return err
	}

	scope.Logger().V(4).Info("Reconciled cluster extensions",
		"controlPlaneVIP", ext.LoadBalancers.ControlPlane,
		"projectID", ext.Networking.Cilium.ProjectID)
	return nil
}

func (r *OpenStackMachineReconciler) reconcileMachineExtensions(ctx context.Context, scope *scope.WithLogger, osm *infrav1.OpenStackMachine, osc *infrav1.OpenStackCluster) error {
	return r.reconcileAllowedAddressPairsForVIPs(ctx, scope, osm, osc)
}

func (r *OpenStackMachineReconciler) reconcileAllowedAddressPairsForVIPs(ctx context.Context, scope *scope.WithLogger, osm *infrav1.OpenStackMachine, osc *infrav1.OpenStackCluster) error {
	if osm == nil || osc == nil {
		return nil
	}
	if osm.Spec.Extensions == nil || osm.Spec.Extensions.LoadBalancers == nil {
		return nil
	}
	if osm.Status.InstanceID == nil || *osm.Status.InstanceID == "" {
		return nil
	}
	clusterExt := osc.Status.Extensions
	if clusterExt == nil || clusterExt.LoadBalancers == nil {
		return nil
	}
	if osc.Status.Network == nil || osc.Status.Network.ID == "" {
		return nil
	}

	var desiredPairs []infrav1.AddressPair
	if osm.Spec.Extensions.LoadBalancers.ControlPlaneVIP && clusterExt.LoadBalancers.ControlPlane != nil {
		if vip := clusterExt.LoadBalancers.ControlPlane.VIP; vip != "" {
			desiredPairs = append(desiredPairs, infrav1.AddressPair{IPAddress: vip})
		}
	}
	if osm.Spec.Extensions.LoadBalancers.IngressVIP && clusterExt.LoadBalancers.Ingress != nil {
		if vip := clusterExt.LoadBalancers.Ingress.VIP; vip != "" {
			desiredPairs = append(desiredPairs, infrav1.AddressPair{IPAddress: vip})
		}
	}
	if len(desiredPairs) == 0 {
		return nil
	}

	networkingService, err := networking.NewService(scope)
	if err != nil {
		return err
	}

	ports, err := networkingService.ListInstancePorts(*osm.Status.InstanceID, osc.Status.Network.ID)
	if err != nil {
		return err
	}
	for i := range ports {
		if _, err := networkingService.EnsureAllowedAddressPairs(&ports[i], desiredPairs); err != nil {
			return err
		}
	}

	return nil
}

func ensureClusterExtensionsStatus(osc *infrav1.OpenStackCluster) *infrav1.OpenStackClusterExtensionsStatus {
	if osc.Status.Extensions == nil {
		osc.Status.Extensions = &infrav1.OpenStackClusterExtensionsStatus{}
	}
	if osc.Status.Extensions.LoadBalancers == nil {
		osc.Status.Extensions.LoadBalancers = &infrav1.ClusterLoadBalancersExtensionsStatus{}
	}
	if osc.Status.Extensions.Networking == nil {
		osc.Status.Extensions.Networking = &infrav1.ClusterNetworkingExtensionsStatus{}
	}
	if osc.Status.Extensions.Networking.Cilium == nil {
		osc.Status.Extensions.Networking.Cilium = &infrav1.CiliumNetworkingStatus{}
	}
	if osc.Status.Extensions.OpenStack == nil {
		osc.Status.Extensions.OpenStack = &infrav1.ClusterOpenStackExtensionsStatus{}
	}
	if osc.Status.Extensions.Platform == nil {
		osc.Status.Extensions.Platform = &infrav1.ClusterPlatformExtensionsStatus{}
	}
	if osc.Status.Extensions.Endpoints == nil {
		osc.Status.Extensions.Endpoints = &infrav1.ClusterEndpointsExtensionsStatus{}
	}
	return osc.Status.Extensions
}

func (r *OpenStackClusterReconciler) reconcileClusterLoadBalancers(ctx context.Context, scope *scope.WithLogger, cluster *clusterv1.Cluster, ext *infrav1.OpenStackClusterExtensionsStatus, osc *infrav1.OpenStackCluster) error {
	if ext.LoadBalancers.ControlPlane == nil {
		ext.LoadBalancers.ControlPlane = &infrav1.ClusterVIPStatus{}
	}
	privateIP, err := r.ensureControlPlaneKeepalivedPort(ctx, scope, cluster, osc)
	if err != nil {
		return err
	}

	// ext.LoadBalancers.ControlPlane.VIP 字段设置为申请网卡的私网IP
	if privateIP != "" {
		ext.LoadBalancers.ControlPlane.VIP = privateIP
	}

	// ext.OpenStack.Mgmt 为控制面的floating ip，来自 clusterconfig CR
	publicVIP, fetchErr := r.fetchUnstructuredStringField(
		ctx,
		serviceCatalogConfigsGVR,
		types.NamespacedName{
			Namespace: clusterConfigNamespace,
			Name:      clusterConfigName,
		},
		clusterConfigPublicVIPPath...,
	)

	if fetchErr != nil {
		scope.Logger().Error(fetchErr, "failed to fetch clusterconfig public VIP")
	}
	ext.OpenStack.Mgmt = publicVIP

	// ext.LoadBalancers.Ingress  字段设置为申请网卡的私网IP 需要单独申请
	if ext.LoadBalancers.Ingress == nil {
		ext.LoadBalancers.Ingress = &infrav1.ClusterVIPStatus{}
	}
	ingressIP, err := r.ensureIngressKeepalivedPort(ctx, scope, cluster, osc)
	if err != nil {
		return err
	}
	if ingressIP != "" {
		ext.LoadBalancers.Ingress.VIP = ingressIP
	}

	if ext.LoadBalancers.Harbor == nil {
		ext.LoadBalancers.Harbor = &infrav1.ClusterVIPStatus{}
	}

	ext.LoadBalancers.Harbor.VIP = ingressIP
	return nil
}

func reconcileClusterNetworking(ctx context.Context, scope *scope.WithLogger, cluster *clusterv1.Cluster, ext *infrav1.OpenStackClusterExtensionsStatus, osc *infrav1.OpenStackCluster) error {
	plugin := ""
	if osc.Spec.Extensions != nil && osc.Spec.Extensions.Networking != nil {
		plugin = osc.Spec.Extensions.Networking.KubeNetworkPlugin
	}
	if strings.EqualFold(plugin, infrav1.KubeNetworkPluginCilium) {
		ext.Networking.Cilium.ProjectID = scope.ProjectID()
		if len(ext.Networking.Cilium.SecurityGroupIDs) == 0 {
			createdID, err := exthelpers.ReconcileVpcCniSecurityGroup(ctx, scope, cluster, osc)
			if err != nil {
				return err
			}
			ext.Networking.Cilium.SecurityGroupIDs = []string{createdID}
		}
		if ext.Networking.Cilium.DefaultSubnetID == "" {
			subnetID, err := exthelpers.ReconcileVpcCniNetworking(ctx, scope, cluster, osc)
			if err != nil {
				return err
			}
			if subnetID != "" {
				ext.Networking.Cilium.DefaultSubnetID = subnetID
			}
		}
	}
	return nil
}

func reconcileClusterPlatform(ext *infrav1.OpenStackClusterExtensionsStatus, osc *infrav1.OpenStackCluster) {
	if ext.Platform.Management == nil {
		ext.Platform.Management = &infrav1.ClusterPlatformManagementStatus{}
	}
	// ext.Platform.Harbor.VIP 字段设置为控制面ingress vip
	if osc.Spec.Bastion.IsEnabled() {
		ext.Platform.Management.VIP = osc.Status.Bastion.IP
	} else {
		ext.Platform.Management.VIP = ""
	}
}

func reconcileClusterEndpoints(scope *scope.WithLogger, ext *infrav1.OpenStackClusterExtensionsStatus, osc *infrav1.OpenStackCluster) {
	_ = osc
	endpoints := map[string]*string{
		"keystone": &ext.Endpoints.Keystone,
		"cinder":   &ext.Endpoints.Cinder,
		"nova":     &ext.Endpoints.Nova,
		"neutron":  &ext.Endpoints.Neutron,
	}
	for service, target := range endpoints {
		endpoint, err := scope.ServiceEndpoint(service)
		if err != nil {
			scope.Logger().V(4).Error(err, "failed to resolve service endpoint", "service", service)
			continue
		}
		if endpoint == "" {
			continue
		}
		*target = extractEndpointHost(endpoint)
	}
}

func (r *OpenStackClusterReconciler) reconcileClusterAppCredential(ctx context.Context, scope1 *scope.WithLogger, cluster *clusterv1.Cluster, ext *infrav1.OpenStackClusterExtensionsStatus, osc *infrav1.OpenStackCluster) error {
	if ext.OpenStack.AppCredential == nil {
		ext.OpenStack.AppCredential = &infrav1.ClusterOpenStackAppCredentialStatus{}
	}

	secretName := fmt.Sprintf("%s-%s", names.ClusterResourceName(cluster), appCredentialSecretSuffix)
	if ext.OpenStack.AppCredential.Ref == secretName {
		return nil
	}

	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{
		Name:      secretName,
		Namespace: osc.Namespace,
	}
	if err := r.Client.Get(ctx, secretKey, secret); err == nil {
		ext.OpenStack.AppCredential.Ref = secretName
		return nil
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	identityClient, err := scope1.NewIdentityClient()
	if err != nil {
		if errors.Is(err, scope.ErrIdentityClientUnavailable) {
			scope1.Logger().V(4).Info("identity client unavailable, skipping app credential reconcile")
			return nil
		}
		return err
	}

	authResult := scope1.AuthResult()
	createResult, ok := authResult.(tokens.CreateResult)
	if !ok {
		return fmt.Errorf("unexpected auth result type %T", authResult)
	}
	user, err := createResult.ExtractUser()
	if err != nil {
		return fmt.Errorf("extract user from auth result: %w", err)
	}
	if user == nil || user.ID == "" {
		return fmt.Errorf("missing user ID in auth result")
	}

	appCredName := fmt.Sprintf("%s-appcred", names.ClusterResourceName(cluster))
	createOpts := applicationcredentials.CreateOpts{
		Name:        appCredName,
		Description: names.GetDescription(names.ClusterResourceName(cluster)),
	}
	appCred, err := applicationcredentials.Create(ctx, identityClient, user.ID, createOpts).Extract()
	if err != nil {
		return fmt.Errorf("create application credential: %w", err)
	}

	auth := authConfig{
		ClusterName:   names.ClusterResourceName(cluster),
		AuthURL:       scope1.IdentityEndpoint(),
		AppCredID:     appCred.ID,
		AppCredSecret: appCred.Secret,
		Region:        scope1.RegionName(),
	}
	tmpl, err := template.New("auth").Parse(authTemplate)
	if err != nil {
		return fmt.Errorf("parse auth template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, auth); err != nil {
		return fmt.Errorf("execute auth template: %w", err)
	}

	secretData := map[string][]byte{
		"clouds.yaml": buf.Bytes(),
		"cacert":      []byte("\n"),
	}
	secret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: osc.Namespace,
			Labels: map[string]string{
				"creId": appCred.ID,
			},
		},
		Data: secretData,
	}
	if err := r.Client.Create(ctx, secret); err != nil {
		if apierrors.IsAlreadyExists(err) {
			ext.OpenStack.AppCredential.Ref = secretName
			return nil
		}
		return err
	}

	ext.OpenStack.AppCredential.Ref = secretName
	return nil
}

func extractEndpointHost(endpoint string) string {
	if endpoint == "" {
		return ""
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	if parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	return parsed.Host
}

type keepalivedPortInput struct {
	name           string
	description    string
	tags           []string
	securityGroups []string
}

func (r *OpenStackClusterReconciler) ensureControlPlaneKeepalivedPort(ctx context.Context, scope *scope.WithLogger, cluster *clusterv1.Cluster, osc *infrav1.OpenStackCluster) (string, error) {
	clusterResourceName := names.ClusterResourceName(cluster)
	tags := exthelpers.DeduplicateStrings(append([]string{}, osc.Spec.Tags...), "keepalived", clusterResourceName, "controlplane")
	return r.ensureKeepalivedPort(ctx, scope, osc, keepalivedPortInput{
		name:           fmt.Sprintf("%s-controlplane-keepalived", clusterResourceName),
		description:    fmt.Sprintf("Control plane keepalived VIP port for cluster %s", clusterResourceName),
		tags:           tags,
		securityGroups: exthelpers.CollectControlPlaneSecurityGroups(osc),
	})
}

func (r *OpenStackClusterReconciler) ensureIngressKeepalivedPort(ctx context.Context, scope *scope.WithLogger, cluster *clusterv1.Cluster, osc *infrav1.OpenStackCluster) (string, error) {
	clusterResourceName := names.ClusterResourceName(cluster)
	tags := exthelpers.DeduplicateStrings(append([]string{}, osc.Spec.Tags...), "keepalived", clusterResourceName, "ingress")
	return r.ensureKeepalivedPort(ctx, scope, osc, keepalivedPortInput{
		name:           fmt.Sprintf("%s-ingress-keepalived", clusterResourceName),
		description:    fmt.Sprintf("Ingress keepalived VIP port for cluster %s", clusterResourceName),
		tags:           tags,
		securityGroups: exthelpers.CollectControlPlaneSecurityGroups(osc),
	})
}

func (r *OpenStackClusterReconciler) ensureKeepalivedPort(ctx context.Context, scope *scope.WithLogger, osc *infrav1.OpenStackCluster, input keepalivedPortInput) (string, error) {
	if osc.Status.Network == nil || osc.Status.Network.ID == "" {
		return "", fmt.Errorf("cluster network is not ready")
	}
	if input.name == "" {
		return "", fmt.Errorf("keepalived port name must be provided")
	}

	networkingService, err := networking.NewService(scope)
	if err != nil {
		return "", err
	}

	portSpec := infrav1.ResolvedPortSpec{
		Name:        input.name,
		Description: input.description,
		NetworkID:   osc.Status.Network.ID,
		Tags:        input.tags,
		SecurityGroups: func() []string {
			return input.securityGroups
		}(),
		ResolvedPortSpecFields: infrav1.ResolvedPortSpecFields{
			AdminStateUp: ptr.To(false),
		},
	}

	port, err := networkingService.EnsurePort(osc, &portSpec, infrav1.PortStatus{})
	if err != nil {
		return "", fmt.Errorf("ensure keepalived VIP port: %w", err)
	}
	if len(port.FixedIPs) == 0 || port.FixedIPs[0].IPAddress == "" {
		return "", fmt.Errorf("keepalived VIP port %s has no fixed IP", port.ID)
	}
	return port.FixedIPs[0].IPAddress, nil
}

func (r *OpenStackClusterReconciler) fetchUnstructuredStringField(ctx context.Context, gvr schema.GroupVersionResource, key types.NamespacedName, path ...string) (string, error) {
	if r.DynamicClient == nil {
		return "", fmt.Errorf("dynamic client not configured")
	}

	var resource dynamic.ResourceInterface
	if key.Namespace == "" {
		resource = r.DynamicClient.Resource(gvr)
	} else {
		resource = r.DynamicClient.Resource(gvr).Namespace(key.Namespace)
	}

	obj, err := resource.Get(ctx, key.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}

	value, found, err := unstructured.NestedString(obj.Object, path...)
	if err != nil {
		return "", err
	}
	if !found {
		return "", nil
	}
	return value, nil
}
