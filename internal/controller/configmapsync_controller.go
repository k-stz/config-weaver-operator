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

	"github.com/k-stz/config-weaver-operator/api/v1alpha1"
	weaverv1alpha1 "github.com/k-stz/config-weaver-operator/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ConfigMapSyncReconciler reconciles a ConfigMapSync object
type ConfigMapSyncReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=weaver.example.com,resources=configmapsyncs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=weaver.example.com,resources=configmapsyncs/status,verbs=get;update;patch;create
// +kubebuilder:rbac:groups=weaver.example.com,resources=configmapsyncs/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// It runs each time an event occurs on a watched CR/resource and will return some
// value dependingon whether those state match or not
// Every Controller has a Reconciler object with a Reconcile method
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ConfigMapSync object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *ConfigMapSyncReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithName("Reconcile") // prepends name to log lines

	if log.Enabled() {
		log.Info("Reconcile invoked with Request: " + req.String())
	}

	// First lookup Watched ConfigMapSync
	// We need a pointer so r.Get() can write the object to it
	configMapSync := v1alpha1.ConfigMapSync{}
	// TODO: Important, apparently reading the object from Get
	// reads it from the controller-runtime cache NOT from the K8s API
	// This cache might be shared by multiple instances of reconcilation
	// that's why you shouldn't modify the object directly but first
	// create a DeepCopy of it
	err := r.Get(ctx, req.NamespacedName, &configMapSync)
	if err != nil {
		log.Error(err, "Failed Getting configMapSync")
		return ctrl.Result{}, err
	}
	log.Info("ConfigMapSync testNum:" + string(configMapSync.Spec.TestNum))
	// So now we have a ConfigMapSync object, lets
	// try to create a configmap
	cm := r.createConfigMapTest(ctx, &configMapSync)
	log.Info("Creating ConfigMap", cm.Name, cm.Namespace)

	// First Set Owner reference
	log.Info("Attempting to set ownerReference")
	err = ctrl.SetControllerReference(&configMapSync, cm, r.Scheme)
	if err != nil {
		log.Error(err, "Failure setting ownerReference")
	}
	err = r.Create(ctx, cm)
	if err != nil {
		log.Error(err, "ConfigMap couldn't be created")
		// // First Set Owner reference
		// log.Info("Attempting to set ownerReference")
		// err := ctrl.SetControllerReference(configMapSync.ObjectMeta, cm, r.Scheme)
		// if err != nil {
		// 	log.Error(err, "Failure setting ownerReference")
		// }

		// maybe it already exists so lets try to update an existing one
		log.Info("Attempting to r.Update() existing ConfigMap...")
		err = r.Update(ctx, cm)
		if err != nil {
			log.Error(err, "r.Update(ctx, cm) failed", err)
		}
		log.Info("ConfigMap " + cm.Name + " Updated.")
	}

	// condition := metav1.Condition{
	// 	Type:               "Synced",
	// 	Status:             metav1.ConditionStatus("True"),
	// 	ObservedGeneration: 0,                          // TODO implement ObervedGeneration in metadata.generation
	// 	LastTransitionTime: metav1.NewTime(time.Now()), // TODO set correctly
	// 	Reason:             "ConfigMapCreated" + cm.Name,
	// 	Message:            "details about transition go here",
	// }
	// Does changing the status field cause a watch event on the main resource? Probably not
	log.Info("Conditions Before append" + fmt.Sprint(configMapSync.Status.Conditions))

	configMapSyncCopy := configMapSync.DeepCopy()
	//configMapSyncCopy.Status.Conditions = append(configMapSync.Status.Conditions, condition)
	configMapSyncCopy.Status.Test = "Testing Here!"

	log.Info("Conditions After append" + fmt.Sprint(configMapSync.Status.Conditions))

	// Update status
	log.Info("Caling r.Update(ctx, configMapSync) ...")

	// .status should be able to be reconstituted from the state of the world
	// so it's not a good idea to read from the status of the root object. Instead
	// you should reconstruct it every run

	//err = r.Update(ctx, configMapSync)
	err = r.Status().Update(ctx, configMapSyncCopy)
	if err != nil {
		log.Error(err, "Failed UPdating .status of configMapSyncCopy")
	}
	log.Info("Conditions in controller process:" + fmt.Sprint(configMapSyncCopy.Status.Conditions))

	// No error => stops Reconcile
	return ctrl.Result{}, nil

	// To reconcile again after X time
	// thus implementing best practice of
	// return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *ConfigMapSyncReconciler) createConfigMapTest(ctx context.Context, configMapSync *v1alpha1.ConfigMapSync) *v1.ConfigMap {
	log := log.FromContext(ctx).WithName("createConfigMapTest")
	log.Info("Entered")
	configMapSync = configMapSync.DeepCopy()
	testNum := configMapSync.Spec.TestNum
	testNumString := fmt.Sprintf("%d", testNum)

	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "createdbymycontroller",
			Namespace: "default",
		},
		//Data["testData"] = "hi",
		Data: map[string]string{
			"testNum": testNumString,
		},
	}

	// Set Owner Reference!
	return cm
}

// SetupWithManager sets up the controller with the Manager.
func (r *ConfigMapSyncReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// Watch ConfigMapSync CR and trigger reconciliation on
		// Add/Update/Delete events
		For(&weaverv1alpha1.ConfigMapSync{}).
		// Watch the ConfigMap managed by the ConfigMapSync controller , also
		// triggereing reconciliation
		Owns(&v1.ConfigMap{}).
		// You can set many more options here, for example the number
		// of concurrent reconciles (default is one) with:
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		// Furthermore "Predicates" can be added using
		// .WithEvntFilter(<predicate.Predicate>) which
		// can filter events by type (create, update,delete)
		// and content, mainly traffic to API server from
		// Reconcile()
		Complete(r)
}
