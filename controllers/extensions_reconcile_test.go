package controllers

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic/fake"
)

func TestFetchUnstructuredStringField(t *testing.T) {
	t.Parallel()

	gvk := schema.GroupVersionKind{
		Group:   serviceCatalogConfigsGVR.Group,
		Version: serviceCatalogConfigsGVR.Version,
		Kind:    clusterConfigKind,
	}

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": gvk.GroupVersion().String(),
			"kind":       gvk.Kind,
			"metadata": map[string]interface{}{
				"name":      clusterConfigName,
				"namespace": clusterConfigNamespace,
			},
			"data": map[string]interface{}{
				"cluster_attrs": map[string]interface{}{
					"public_vip": "10.0.0.10",
				},
			},
		},
	}
	obj.SetGroupVersionKind(gvk)

	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})

	r := &OpenStackClusterReconciler{
		DynamicClient: fake.NewSimpleDynamicClient(scheme, obj),
	}

	value, err := r.fetchUnstructuredStringField(
		context.Background(),
		serviceCatalogConfigsGVR,
		types.NamespacedName{
			Namespace: clusterConfigNamespace,
			Name:      clusterConfigName,
		},
		clusterConfigPublicVIPPath...,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if value != "10.0.0.10" {
		t.Fatalf("unexpected vip, want 10.0.0.10, got %s", value)
	}
}

func TestFetchUnstructuredStringFieldNotFound(t *testing.T) {
	t.Parallel()

	gvk := schema.GroupVersionKind{
		Group:   serviceCatalogConfigsGVR.Group,
		Version: serviceCatalogConfigsGVR.Version,
		Kind:    clusterConfigKind,
	}

	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})

	r := &OpenStackClusterReconciler{
		DynamicClient: fake.NewSimpleDynamicClient(scheme),
	}

	value, err := r.fetchUnstructuredStringField(
		context.Background(),
		serviceCatalogConfigsGVR,
		types.NamespacedName{
			Namespace: clusterConfigNamespace,
			Name:      clusterConfigName,
		},
		clusterConfigPublicVIPPath...,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if value != "" {
		t.Fatalf("expected empty vip, got %s", value)
	}
}
