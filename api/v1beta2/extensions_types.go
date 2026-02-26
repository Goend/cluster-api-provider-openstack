package v1beta2

// SecretKeyReference extends SecretReference with an optional data key.
// Note: kept here for parity with v1beta1; not currently referenced from v1beta2 types.
type SecretKeyReference struct {
	Name        string `json:"name,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
	Key         string `json:"key,omitempty"`
	UsernameKey string `json:"usernameKey,omitempty"`
}

// OpenStackClusterExtensionsSpec captures provider-specific knobs consumed by bootstrap/controller layers.
type OpenStackClusterExtensionsSpec struct {
	Networking        *ClusterNetworkingExtensionsSpec        `json:"networking,omitempty"`
	NetworkInterfaces *ClusterNetworkInterfacesExtensionsSpec `json:"networkInterfaces,omitempty"`
	OpenStack         *ClusterOpenStackExtensionsSpec         `json:"openStack,omitempty"`
}

type ClusterNetworkingExtensionsSpec struct {
	// +kubebuilder:validation:Enum=cilium;flannel
	// +kubebuilder:validation:Required
	// Supported values: cilium, flannel.
	KubeNetworkPlugin string `json:"kubeNetworkPlugin"`
}

const (
	KubeNetworkPluginCilium  = "cilium"
	KubeNetworkPluginFlannel = "flannel"
)

type ClusterNetworkInterfacesExtensionsSpec struct {
	// Required when kubeNetworkPlugin is set to flannel.
	Flannel string `json:"flannel,omitempty"`
}

type ClusterOpenStackExtensionsSpec struct{}

// OpenStackClusterExtensionsStatus surfaces infra-derived facts for bootstrap/ACP.
type OpenStackClusterExtensionsStatus struct {
	Networking    *ClusterNetworkingExtensionsStatus    `json:"networking,omitempty"`
	LoadBalancers *ClusterLoadBalancersExtensionsStatus `json:"loadBalancers,omitempty"`
	Platform      *ClusterPlatformExtensionsStatus      `json:"platform,omitempty"`
	OpenStack     *ClusterOpenStackExtensionsStatus     `json:"openStack,omitempty"`
	Endpoints     *ClusterEndpointsExtensionsStatus     `json:"endpoints,omitempty"`
}

type ClusterNetworkingExtensionsStatus struct {
	Cilium *CiliumNetworkingStatus `json:"cilium,omitempty"`
}

type CiliumNetworkingStatus struct {
	ProjectID        string   `json:"projectID,omitempty"`
	DefaultSubnetID  string   `json:"defaultSubnetID,omitempty"`
	SecurityGroupIDs []string `json:"securityGroupIDs,omitempty"`
	WebhookEnable    *bool    `json:"webhookEnable,omitempty"`
}

type ClusterLoadBalancersExtensionsStatus struct {
	ControlPlane *ClusterVIPStatus `json:"controlPlane,omitempty"`
	Ingress      *ClusterVIPStatus `json:"ingress,omitempty"`
	Harbor       *ClusterVIPStatus `json:"harbor,omitempty"`
}

type ClusterVIPStatus struct {
	VIP string `json:"vip,omitempty"`
}

type ClusterPlatformExtensionsStatus struct {
	NTP        *ClusterPlatformNTPStatus        `json:"ntp,omitempty"`
	Management *ClusterPlatformManagementStatus `json:"management,omitempty"`
}

type ClusterPlatformNTPStatus struct {
	Server string `json:"server,omitempty"`
}

type ClusterPlatformManagementStatus struct {
	VIP string `json:"vip,omitempty"`
}

type ClusterOpenStackExtensionsStatus struct {
	Mgmt          string                               `json:"mgmt,omitempty"`
	Keystone      string                               `json:"keystone,omitempty"`
	Cinder        string                               `json:"cinder,omitempty"`
	Nova          string                               `json:"nova,omitempty"`
	Neutron       string                               `json:"neutron,omitempty"`
	Project       string                               `json:"project,omitempty"`
	ProjectDomain string                               `json:"projectDomain,omitempty"`
	AppCredential *ClusterOpenStackAppCredentialStatus `json:"appCredential,omitempty"`
	Region        string                               `json:"region,omitempty"`
}

type ClusterOpenStackAppCredentialStatus struct {
	Ref string `json:"ref,omitempty"`
}

type ClusterEndpointsExtensionsStatus struct {
	Keystone string `json:"keystone,omitempty"`
	Cinder   string `json:"cinder,omitempty"`
	Nova     string `json:"nova,omitempty"`
	Neutron  string `json:"neutron,omitempty"`
}

// OpenStackMachineExtensionsSpec captures machine-scoped knobs.
type OpenStackMachineExtensionsSpec struct {
	NetworkInterfaces *MachineNetworkInterfacesSpec `json:"networkInterfaces,omitempty"`
	LoadBalancers     *MachineLoadBalancersSpec     `json:"loadBalancers,omitempty"`
	Memory            *MachineMemoryExtensionsSpec  `json:"memory,omitempty"`
}

type MachineNetworkInterfacesSpec struct {
	Keepalived string `json:"keepalived,omitempty"`
}

type MachineLoadBalancersSpec struct {
	ControlPlaneVIP bool `json:"controlPlaneVIP,omitempty"`
	IngressVIP      bool `json:"ingressVIP,omitempty"`
}

type MachineMemoryExtensionsSpec struct {
	// +kubebuilder:validation:Pattern="^-[0-9]+$"
	Reserved string `json:"reserved,omitempty"`
}

// OpenStackMachineExtensionsStatus surfaces infra-derived machine data.
type OpenStackMachineExtensionsStatus struct{}
