package extensions

import (
	"sort"

	"k8s.io/utils/ptr"
	infrav1 "sigs.k8s.io/cluster-api-provider-openstack/api/v1beta1"
)

func CollectSecurityGroups(osc *infrav1.OpenStackCluster) []string {
	idSet := map[string]struct{}{}
	appendSG := func(sg *infrav1.SecurityGroupStatus) {
		if sg == nil || sg.ID == "" {
			return
		}
		idSet[sg.ID] = struct{}{}
	}
	appendSG(osc.Status.ControlPlaneSecurityGroup)
	appendSG(osc.Status.WorkerSecurityGroup)
	appendSG(osc.Status.BastionSecurityGroup)
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func PickSubnetID(network *infrav1.NetworkStatusWithSubnets) string {
	if network == nil {
		return ""
	}
	var fallback string
	for i := range network.Subnets {
		s := network.Subnets[i]
		if fallback == "" {
			fallback = s.ID
		}
		for _, tag := range s.Tags {
			if tag == "cilium-default" {
				return s.ID
			}
		}
	}
	return fallback
}

func CollectControlPlaneSecurityGroups(osc *infrav1.OpenStackCluster) []string {
	if osc == nil || osc.Status.ControlPlaneSecurityGroup == nil || osc.Status.ControlPlaneSecurityGroup.ID == "" {
		return nil
	}
	return []string{osc.Status.ControlPlaneSecurityGroup.ID}
}

func ShouldAttachKeepalivedFloatingIP(osc *infrav1.OpenStackCluster) bool {
	if osc == nil {
		return false
	}
	if osc.Status.ExternalNetwork == nil {
		return false
	}
	if ptr.Deref(osc.Spec.DisableExternalNetwork, false) {
		return false
	}
	if ptr.Deref(osc.Spec.DisableAPIServerFloatingIP, false) {
		return false
	}
	if osc.Spec.APIServerLoadBalancer != nil && osc.Spec.APIServerLoadBalancer.IsEnabled() {
		return false
	}
	return true
}

func DesiredKeepalivedFloatingIP(osc *infrav1.OpenStackCluster) *string {
	switch {
	case osc == nil:
		return nil
	case osc.Spec.ControlPlaneEndpoint != nil && osc.Spec.ControlPlaneEndpoint.IsValid():
		host := osc.Spec.ControlPlaneEndpoint.Host
		return &host
	case osc.Spec.APIServerFloatingIP != nil:
		return osc.Spec.APIServerFloatingIP
	default:
		return nil
	}
}

func DeduplicateStrings(initial []string, extras ...string) []string {
	set := make(map[string]struct{}, len(initial)+len(extras))
	result := make([]string, 0, len(initial)+len(extras))
	appendValue := func(value string) {
		if value == "" {
			return
		}
		if _, exists := set[value]; exists {
			return
		}
		set[value] = struct{}{}
		result = append(result, value)
	}
	for _, v := range initial {
		appendValue(v)
	}
	for _, v := range extras {
		appendValue(v)
	}
	return result
}
