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
	"fmt"

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
		ctx := context.Background()

		// CMS setup
		namespacedNameCMS := types.NamespacedName{
			Name: "ginkgo-test-cms",
			//Namespace: "default", // clusterscoped
		}
		cmsName := namespacedNameCMS.Name
		objectCMS := &weaverv1alpha1.ConfigMapSync{}

		// source CM setup
		namespacedNameCM := types.NamespacedName{
			Name:      "my-source-cm",
			Namespace: "default",
		}
		sourceCMName := namespacedNameCM.Name
		sourceNamespace := namespacedNameCM.Namespace
		objectSourceCM := &v1.ConfigMap{}

		targetNamespace := "target-ns"

		BeforeEach(func() {
			By(fmt.Sprint("creating the target namespace ", targetNamespace))
			Expect(k8sClient.Create(ctx, &v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: targetNamespace},
			})).To(Succeed())

			By(fmt.Sprint("creating the source ConfigMap ", sourceCMName, " in Namespace ", sourceNamespace))
			err := k8sClient.Get(ctx, namespacedNameCM, objectSourceCM)
			if err != nil && errors.IsNotFound(err) {
				objectCMS := &v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      sourceCMName,
						Namespace: sourceNamespace,
					},
					Data: map[string]string{"firstfield": "bar", "f2": "v2"},
				}
				Expect(k8sClient.Create(ctx, objectCMS)).To(Succeed())
			}

			By(fmt.Sprint("creating the custom resource for the Kind ConfigMapSync named ", cmsName))
			err = k8sClient.Get(ctx, namespacedNameCMS, objectCMS)
			if err != nil && errors.IsNotFound(err) {
				objectCMS := &weaverv1alpha1.ConfigMapSync{
					ObjectMeta: metav1.ObjectMeta{
						Name: cmsName,
						// Namespace: "default",
					},
					Spec: weaverv1alpha1.ConfigMapSyncSpec{
						Source: weaverv1alpha1.Source{
							Name:      sourceCMName,
							Namespace: sourceNamespace,
						},
					},
				}
				Expect(k8sClient.Create(ctx, objectCMS)).To(Succeed())
			}
		})

		AfterEach(func() {
			objectCMS := &weaverv1alpha1.ConfigMapSync{}
			err := k8sClient.Get(ctx, namespacedNameCMS, objectCMS)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance ConfigMapSync")
			Expect(k8sClient.Delete(ctx, objectCMS)).To(Succeed())
			By("Cleanup the specific source ConfigMap")
			//Expect(k8sClient.Delete(ctx, resourceName)).To(Succeed())

		})

		// Here actual tests start
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
