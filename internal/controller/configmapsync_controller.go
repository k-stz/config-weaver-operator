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
// +kubebuilder:rbac:groups=weaver.example.com,resources=configmapsyncs/status,verbs=get;update;patch
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
	_ = log.FromContext(ctx)
	// First lookup Watched ConfigMapSync
	configMapSync := &v1alpha1.ConfigMapSync{}
	err := r.Get(ctx, req.NamespacedName, configMapSync)
	if err != nil {
		fmt.Println("Failed Getting configMapSync, err", err)
		return ctrl.Result{}, nil
	}
	fmt.Println("ConfigMapSync content:", configMapSync)

	return ctrl.Result{}, nil
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
