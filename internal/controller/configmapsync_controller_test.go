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
			Name:      "ginkgo-test-cms",
			Namespace: "default", // clusterscoped
		}
		cmsName := namespacedNameCMS.Name
		cmsNamespace := namespacedNameCMS.Namespace
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

		sourceCMDataContent := map[string]string{"firstfield": "bar", "f2": "v2"}

		BeforeEach(func() {
			By(fmt.Sprint("Ensure target namespace ", targetNamespace, " exists"))

			err := k8sClient.Create(ctx, &v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: targetNamespace},
			})
			Expect(err == nil || errors.IsAlreadyExists(err)).To(BeTrue())

			By(fmt.Sprint("creating the source ConfigMap ", sourceCMName, " in Namespace ", sourceNamespace))
			err = k8sClient.Get(ctx, namespacedNameCM, objectSourceCM)
			if err != nil && errors.IsNotFound(err) {
				objectCMS := &v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      sourceCMName,
						Namespace: sourceNamespace,
					},
					Data: sourceCMDataContent,
				}
				Expect(k8sClient.Create(ctx, objectCMS)).To(Succeed())
			}

			By(fmt.Sprint("creating the custom resource for the Kind ConfigMapSync ", cmsNamespace+"/"+cmsName))
			err = k8sClient.Get(ctx, namespacedNameCMS, objectCMS)
			if err != nil && errors.IsNotFound(err) {
				objectCMS := &weaverv1alpha1.ConfigMapSync{
					ObjectMeta: metav1.ObjectMeta{
						Name:      cmsName,
						Namespace: cmsNamespace,
					},
					Spec: weaverv1alpha1.ConfigMapSyncSpec{
						Source: weaverv1alpha1.Source{
							Name:      sourceCMName,
							Namespace: sourceNamespace,
						},
						// configure target ns here
						SyncToNamespaces: []string{targetNamespace},
					},
				}
				Expect(k8sClient.Create(ctx, objectCMS)).To(Succeed())
			}
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
		})

		// Are the "It()"s executed out-of-order?
		It(fmt.Sprint("should successfully sync source ConfigMap ", sourceCMName, " to target ConfigMap in Namespace ", targetNamespace), func() {
			By("Reconciling the created resource")
			controllerReconciler := &ConfigMapSyncReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: namespacedNameCMS,
			})
			Expect(err).NotTo(HaveOccurred())

			By(fmt.Sprint("Check if source matches with target ConfigMap in namespace ", targetNamespace))
			var targetCM v1.ConfigMap
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      sourceCMName,
				Namespace: targetNamespace,
			}, &targetCM)
			Expect(err).NotTo(HaveOccurred())
			Expect(targetCM.Data).To(Equal(sourceCMDataContent))
		})

		It("should keep target ConfigMap in sync when changing Data field in the source ConfigMap", func() {
			By("By inserting a new field in the source ConfigMap")
			var sourceCM v1.ConfigMap
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      sourceCMName,
				Namespace: sourceNamespace,
			}, &sourceCM)
			Expect(err).NotTo(HaveOccurred())
			sourceCM.Data["newField"] = "newValue"
			err = k8sClient.Update(ctx, &sourceCM)
			Expect(err).NotTo(HaveOccurred())

			By("Reconciling the ConfigMapSync Resource")
			controllerReconciler := &ConfigMapSyncReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: namespacedNameCMS,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Finally Checking if the target CM still matches the source CM Data field")
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      sourceCMName,
				Namespace: sourceNamespace,
			}, &sourceCM)
			Expect(err).NotTo(HaveOccurred())
			var targetCM v1.ConfigMap
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      sourceCMName,
				Namespace: targetNamespace,
			}, &targetCM)
			Expect(err).NotTo(HaveOccurred())
			Expect(targetCM.Data).To(Equal(sourceCM.Data))
		})

		AfterEach(func() {
			By("Cleanup the specific resource instance ConfigMapSync")
			objectCMS := &weaverv1alpha1.ConfigMapSync{}
			err := k8sClient.Get(ctx, namespacedNameCMS, objectCMS)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Delete(ctx, objectCMS)).To(Succeed())

			By("Cleanup the source ConfigMap")
			objectSourceCM := &v1.ConfigMap{}
			err = k8sClient.Get(ctx, namespacedNameCM, objectSourceCM)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Delete(ctx, objectSourceCM)).To(Succeed())

			// ENVTEST CAN'T DELETE NAMESPACES!
			// Source: https://book.kubebuilder.io/reference/envtest.html?utm_source=chatgpt.com#namespace-usage-limitation
			// By("Ensure target namespace is really deleted")
			// Eventually(func() error {
			// 	// TODO doesn't work, list all resource in the namespace to see what is blocking
			// 	tmpNs := &v1.Namespace{}
			// 	err := k8sClient.Get(ctx, types.NamespacedName{Name: targetNamespace}, tmpNs)
			// 	fmt.Println("STILL NOT DELETED target-ns:", objectTargetNamespace)
			// 	fmt.Println("deletion timestamp:", tmpNs.DeletionTimestamp, "phase:", tmpNs.Status.Phase)
			// 	if err == nil {
			// 		fmt.Println("Couldn't delete namespace, inspecting what configmaps still remain in cm")
			// 		var configMaps v1.ConfigMapList
			// 		err := k8sClient.List(ctx, &configMaps, client.InNamespace(targetNamespace))
			// 		Expect(err).NotTo(HaveOccurred())
			// 		fmt.Printf("Found %d configmaps in %s\n", len(configMaps.Items), targetNamespace)
			// 		for _, cm := range configMaps.Items {
			// 			fmt.Println("ConfigMap:", cm.Name)
			// 		}

			// 	}
			// 	return err
			// }, time.Second*5, time.Millisecond*200).ShouldNot(Succeed())

			// Expect(err).To(HaveOccurred())

		})

	})
})
