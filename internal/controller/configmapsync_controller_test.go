/*
Copyright 2025.

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

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	weaverv1alpha1 "github.com/k-stz/config-weaver-operator/api/v1alpha1"
)

var _ = Describe("ConfigMapSync Controller", func() {
	Context("When reconciling a ConfigMapSync resource", func() {
		const cmsResourceName = "test-resource"
		const sourceConfigMapName = "foocm"

		ctx := context.Background()

		namespacedNameCMS := types.NamespacedName{
			Name:      cmsResourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		configmapsync := &weaverv1alpha1.ConfigMapSync{}

		BeforeEach(func() {
			// first create the source ConfigMap
			By("creating the source configmap for further test")
			sourceCM := &v1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
				Name:      "foocm",
				Namespace: "default",
			}}
			Expect(k8sClient.Create(ctx, sourceCM)).To(Succeed())

			By("creating the custom resource for the Kind ConfigMapSync")
			err := k8sClient.Get(ctx, namespacedNameCMS, configmapsync)
			if err != nil && errors.IsNotFound(err) {
				objectCMS := &weaverv1alpha1.ConfigMapSync{
					ObjectMeta: metav1.ObjectMeta{
						Name:      cmsResourceName,
						Namespace: "default",
					},
					Spec: weaverv1alpha1.ConfigMapSyncSpec{
						Source: weaverv1alpha1.Source{
							Name:      "foocm",
							Namespace: "default",
						},
					},
				}
				Expect(k8sClient.Create(ctx, objectCMS)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.

			objectCMS := &weaverv1alpha1.ConfigMapSync{}
			err := k8sClient.Get(ctx, namespacedNameCMS, objectCMS)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance ConfigMapSync")
			Expect(k8sClient.Delete(ctx, objectCMS)).To(Succeed())
			By("Cleanup the specific source ConfigMap")
			//Expect(k8sClient.Delete(ctx, resourceName)).To(Succeed())

		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ConfigMapSyncReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: namespacedNameCMS,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})
})
