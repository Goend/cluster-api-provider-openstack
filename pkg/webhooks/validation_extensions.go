/*
Copyright 2023 The Kubernetes Authors.

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

package webhooks

import (
	"strings"

	"k8s.io/apimachinery/pkg/util/validation/field"

	infrav1 "sigs.k8s.io/cluster-api-provider-openstack/api/v1beta1"
)

func validateNetworkingExtensions(spec *infrav1.OpenStackClusterSpec, basePath *field.Path) field.ErrorList {
	var allErrs field.ErrorList
	if spec == nil || spec.Extensions == nil || spec.Extensions.Networking == nil {
		return allErrs
	}

	plugin := spec.Extensions.Networking.KubeNetworkPlugin
	if strings.EqualFold(plugin, infrav1.KubeNetworkPluginFlannel) {
		if spec.Extensions.NetworkInterfaces == nil || spec.Extensions.NetworkInterfaces.Flannel == "" {
			allErrs = append(allErrs, field.Required(basePath.Child("extensions", "networkInterfaces", "flannel"), "required when kubeNetworkPlugin is flannel"))
		}
	}

	return allErrs
}
