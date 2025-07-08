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
	rbacv1 "k8s.io/api/rbac/v1"

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

		// serviceaccount
		namespacedNameServiceAccount := types.NamespacedName{
			Name:      "default",
			Namespace: "default",
		}
		objectServiceAccount := &v1.ServiceAccount{}

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

			By(fmt.Sprint("creating system:serviceaccount:default:default used by CMS ", cmsNamespace+"/"+cmsName))
			err = k8sClient.Get(ctx, namespacedNameCMS, objectServiceAccount)
			if err != nil && errors.IsNotFound(err) {
				objectServiceAccount := &v1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      namespacedNameServiceAccount.Name,
						Namespace: namespacedNameServiceAccount.Namespace,
					},
				}
				Expect(k8sClient.Create(ctx, objectServiceAccount)).To(Succeed())
			}

			By(fmt.Sprint("creating rbac Role for the serviceaccount for the default ns", cmsNamespace+"/"+cmsName))
			roleObj := &rbacv1.Role{}
			roleNamedNamespace := types.NamespacedName{
				Name:      "cms-sync",
				Namespace: cmsNamespace,
			}
			err = k8sClient.Get(ctx, roleNamedNamespace, roleObj)
			if err != nil && errors.IsNotFound(err) {
				roleObj = &rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Name:      roleNamedNamespace.Name,
						Namespace: roleNamedNamespace.Namespace,
					},
					Rules: []rbacv1.PolicyRule{
						{
							APIGroups: []string{""},
							Verbs:     []string{"create", "update", "delete"},
							Resources: []string{"configmaps"},
						},
					},
				}
				Expect(k8sClient.Create(ctx, roleObj)).To(Succeed())
			}

			By(fmt.Sprint("creating Rolebinding serviceaccount for namespace ", "default", cmsNamespace+"/"+cmsName))
			rolebindingObj := &rbacv1.RoleBinding{}
			roleNamedNamespace = types.NamespacedName{
				Name:      "cms-sync",
				Namespace: cmsNamespace,
			}
			err = k8sClient.Get(ctx, roleNamedNamespace, rolebindingObj)
			if err != nil && errors.IsNotFound(err) {
				rolebindingObj = &rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      roleNamedNamespace.Name,
						Namespace: roleNamedNamespace.Namespace,
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "Role",
						Name:     "cms-sync",
					},
					Subjects: []rbacv1.Subject{
						{Kind: "ServiceAccount",
							Name:      "default",
							Namespace: "default",
						},
					},
				}
				Expect(k8sClient.Create(ctx, rolebindingObj)).To(Succeed())
			}

			By(fmt.Sprint("creating rbac Role for the serviceaccount for the namespace ", targetNamespace, cmsNamespace+"/"+cmsName))
			roleObjTargetNs := &rbacv1.Role{}
			roleNamedNamespaceTargetNs := types.NamespacedName{
				Name:      "cms-sync",
				Namespace: targetNamespace,
			}
			err = k8sClient.Get(ctx, roleNamedNamespaceTargetNs, roleObjTargetNs)
			if err != nil && errors.IsNotFound(err) {
				roleObjTargetNs = &rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cms-sync",
						Namespace: targetNamespace,
					},
					Rules: []rbacv1.PolicyRule{
						{
							APIGroups: []string{""},
							Verbs:     []string{"create", "update", "delete"},
							Resources: []string{"configmaps"},
						},
					},
				}
				Expect(k8sClient.Create(ctx, roleObjTargetNs)).To(Succeed())
			}

			By(fmt.Sprint("creating Rolebinding serviceaccount for the namespace", targetNamespace, cmsNamespace+"/"+cmsName))
			rolebindingObjTargetNs := &rbacv1.RoleBinding{}
			roleNamedNamespaceTargetNs = types.NamespacedName{
				Name:      "cms-sync",
				Namespace: targetNamespace,
			}
			err = k8sClient.Get(ctx, roleNamedNamespaceTargetNs, rolebindingObjTargetNs)
			if err != nil && errors.IsNotFound(err) {
				rolebindingObjTargetNs = &rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      roleNamedNamespaceTargetNs.Name,
						Namespace: roleNamedNamespaceTargetNs.Namespace,
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "Role",
						Name:     "cms-sync",
					},
					Subjects: []rbacv1.Subject{
						{Kind: "ServiceAccount",
							Name:      "default",
							Namespace: "default",
						},
					},
				}
				Expect(k8sClient.Create(ctx, rolebindingObjTargetNs)).To(Succeed())
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
						ServiceAccount:   weaverv1alpha1.ServiceAccount{Name: "default"},
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

			By("Cleanup the source serviceaccount")
			//objectSourceCM := &v1.ConfigMap{}
			err = k8sClient.Get(ctx, namespacedNameServiceAccount, objectServiceAccount)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Delete(ctx, objectServiceAccount)).To(Succeed())

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
