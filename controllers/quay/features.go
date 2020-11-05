package controllers

import (
	"context"
	"strings"

	objectbucket "github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	v1 "github.com/quay/quay-operator/apis/quay/v1"
)

const (
	datastoreBucketName = "BUCKET_NAME"
	datastoreBucketHost = "BUCKET_HOST"
	datastoreAccessKey  = "AWS_ACCESS_KEY_ID"
	datastoreSecretKey  = "AWS_SECRET_ACCESS_KEY"
)

func (r *QuayRegistryReconciler) checkRoutesAvailable(quay *v1.QuayRegistry) (*v1.QuayRegistry, error) {
	var routes routev1.RouteList
	err := r.Client.List(context.Background(), &routes)
	if err == nil {
		r.Log.Info("cluster supports `Routes` API")
		existingAnnotations := quay.GetAnnotations()
		if existingAnnotations == nil {
			existingAnnotations = map[string]string{}
		}

		existingAnnotations[v1.SupportsRoutesAnnotation] = "true"

		if _, ok := existingAnnotations[v1.ClusterHostnameAnnotation]; !ok && len(routes.Items) > 0 {
			for _, route := range routes.Items {
				if len(route.Status.Ingress) > 0 {
					existingAnnotations[v1.ClusterHostnameAnnotation] = route.Status.Ingress[0].RouterCanonicalHostname
					r.Log.Info("detected router canonical hostname: " + route.Status.Ingress[0].RouterCanonicalHostname)
					break
				}
			}
		}

		quay.SetAnnotations(existingAnnotations)
	}

	return quay, nil
}

func (r *QuayRegistryReconciler) checkObjectBucketClaimsAvailable(quay *v1.QuayRegistry) (*v1.QuayRegistry, error) {
	datastoreName := types.NamespacedName{Namespace: quay.GetNamespace(), Name: quay.GetName() + "-quay-datastore"}
	var objectBucketClaims objectbucket.ObjectBucketClaimList
	if err := r.Client.List(context.Background(), &objectBucketClaims); err == nil {
		r.Log.Info("cluster supports `ObjectBucketClaims` API")

		existingAnnotations := quay.GetAnnotations()
		if existingAnnotations == nil {
			existingAnnotations = map[string]string{}
		}
		existingAnnotations[v1.SupportsObjectStorageAnnotation] = "true"

		found := false
		for _, obc := range objectBucketClaims.Items {
			if obc.GetNamespace()+"/"+obc.GetName() == datastoreName.String() {
				found = true
				r.Log.Info("`ObjectBucketClaim` exists")

				var datastoreSecret corev1.Secret
				if err = r.Client.Get(context.Background(), datastoreName, &datastoreSecret); err != nil {
					r.Log.Error(err, "unable to retrieve Quay datastore `Secret`")
					return nil, err
				}

				var datastoreConfig corev1.ConfigMap
				if err = r.Client.Get(context.Background(), datastoreName, &datastoreConfig); err != nil {
					r.Log.Error(err, "unable to retrieve Quay datastore `ConfigMap`")
					return nil, err
				}

				r.Log.Info("found `ObjectBucketClaim` and credentials `Secret`, `ConfigMap`")

				host := string(datastoreConfig.Data[datastoreBucketHost])
				if strings.Contains(host, ".svc") && !strings.Contains(host, ".svc.cluster.local") {
					r.Log.Info("`ObjectBucketClaim` is using in-cluster endpoint, ensuring we use the fully qualified domain name")
					host = strings.ReplaceAll(host, ".svc", ".svc.cluster.local")
				}

				existingAnnotations[v1.StorageBucketNameAnnotation] = string(datastoreConfig.Data[datastoreBucketName])
				existingAnnotations[v1.StorageHostnameAnnotation] = host
				existingAnnotations[v1.StorageAccessKeyAnnotation] = string(datastoreSecret.Data[datastoreAccessKey])
				existingAnnotations[v1.StorageSecretKeyAnnotation] = string(datastoreSecret.Data[datastoreSecretKey])
				existingAnnotations[v1.ObjectStorageInitializedAnnotation] = "true"
			}
		}

		if !found {
			r.Log.Info("`ObjectBucketClaim` not found")
		}

		quay.SetAnnotations(existingAnnotations)
	} else if err != nil {
		r.Log.Info("cluster does not support `ObjectBucketClaim` API")
	}

	return quay, nil
}

func stripObjectBucketClaimAnnotations(quay *v1.QuayRegistry) *v1.QuayRegistry {
	existingAnnotations := quay.GetAnnotations()
	if existingAnnotations == nil {
		return quay
	}

	delete(existingAnnotations, v1.StorageBucketNameAnnotation)
	delete(existingAnnotations, v1.StorageHostnameAnnotation)
	delete(existingAnnotations, v1.StorageAccessKeyAnnotation)
	delete(existingAnnotations, v1.StorageSecretKeyAnnotation)

	return quay
}

func configEditorCredentialsSecretFrom(objs []runtime.Object) string {
	for _, obj := range objs {
		objectMeta, _ := meta.Accessor(obj)
		groupVersionKind := obj.GetObjectKind().GroupVersionKind().String()
		secretGVK := schema.GroupVersionKind{Version: "v1", Kind: "Secret"}.String()

		if groupVersionKind == secretGVK && strings.Contains(objectMeta.GetName(), "quay-config-editor-credentials") {
			return objectMeta.GetName()
		}
	}

	return ""
}
