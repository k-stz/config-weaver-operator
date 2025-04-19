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
	"strconv"

	"github.com/k-stz/config-weaver-operator/api/v1alpha1"
	weaverv1alpha1 "github.com/k-stz/config-weaver-operator/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Represents staet of a single reconciliation run
// used to regenerate fresh status for ConfigMapSync.status
type RunState struct {
	cmsDeleted           bool // Set to true, when r.Get() returns nil!
	sourceConfigMapFound bool
	allSynced            bool
}

// ConfigMapSyncReconciler reconciles a ConfigMapSync object
type ConfigMapSyncReconciler struct {
	client.Client // from manager
	Scheme        *runtime.Scheme
	Recorder      record.EventRecorder
	RunState      RunState
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
		log.V(1).Info("Reconcile invoked with Request: " + req.String())
	}
	// TODO: Important, apparently reading the object from Get
	// reads it from the controller-runtime cache NOT from the K8s API
	// This cache might be shared by multiple instances of reconcilation
	// that's why you shouldn't modify the object directly but first
	// create a DeepCopy of it
	cmsFound := true
	configMapSync := v1alpha1.ConfigMapSync{}
	if err := r.Get(ctx, req.NamespacedName, &configMapSync); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "unable to get configMapSync")
			return ctrl.Result{}, err
		}
		cmsFound = false
	}
	// if not found, then we have a deletion
	if cmsFound {
		log.V(1).Info("found configMapSync in " + req.String())
	}

	log.V(1).Info(fmt.Sprint("ConfigMapSync testNum:", configMapSync.Spec.TestNum))
	// So now we have a ConfigMapSync object, lets
	// try to create a configmap
	if err := r.createConfigMaps(ctx, &configMapSync); err != nil {
		log.Error(err, "unable to create ConfigMaps; Updating status...")
		r.updateStatus(ctx, &configMapSync)
		return ctrl.Result{}, err
	}

	r.updateStatus(ctx, &configMapSync)
	// No error => stops Reconcile
	return ctrl.Result{}, nil

	// To reconcile again after X time
	// thus implementing best practice of
	// return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *ConfigMapSyncReconciler) updateStatus(ctx context.Context, cms *v1alpha1.ConfigMapSync) error {
	log := log.FromContext(ctx).WithName("updateStatus")

	if err := r.updateSyncedStatus(ctx, cms); err != nil {
		log.Error(err, "unable to update Status")
		return err
	}

	if err := r.updateSourceFoundStatus(ctx, cms); err != nil {
		log.Error(err, "unable to update Status")
		return err
	}
	return nil
}

func (r *ConfigMapSyncReconciler) updateSyncedStatus(ctx context.Context, cms *v1alpha1.ConfigMapSync) error {
	// TODO set it accodring to state that is tracked in ConfigMapSyncReconciler, for now hardcoded
	newCondition := metav1.Condition{
		Type:               "Synced",
		Status:             metav1.ConditionStatus("True"),
		ObservedGeneration: 0, // TODO implement ObervedGeneration in metadata.generation
		// LastTransitionTime: metav1.NewTime(time.Now()), // Will be set by meta.SetStatusCondition(...)
		Reason:  "SourceConfigMapSynced",
		Message: "Source ConfigMap synced from namespace " + cms.Spec.SourceNamespace,
	}
	return r.updateStatusWithCondition(ctx, newCondition, cms)
}

func (r *ConfigMapSyncReconciler) updateSourceFoundStatus(ctx context.Context, cms *v1alpha1.ConfigMapSync) error {
	// TODO set it accodring to state that is tracked in ConfigMapSyncReconciler, for now hardcoded
	log := log.FromContext(ctx).WithName("Reconcile>updateSourceFoundStatus")

	newCondition := metav1.Condition{
		Type:               "SourceConfigMapFound",
		Status:             metav1.ConditionTrue,
		ObservedGeneration: 0, // TODO implement ObervedGeneration in metadata.generation
		// LastTransitionTime: metav1.NewTime(time.Now()), // Will be set by meta.SetStatusCondition(...)
		Reason:  "ConfgigMapFound",
		Message: "Source ConfigMap found in namespace " + cms.Spec.SourceNamespace,
	}
	log.Info("RunState is sourceConfigMapFound" + fmt.Sprintf("%v", r.RunState.sourceConfigMapFound))
	if !r.RunState.sourceConfigMapFound {

		newCondition.Status = metav1.ConditionFalse // can be True, False or Unknown
		newCondition.Reason = "ConfigMapMissing"
		newCondition.Message = "Source ConfigMap not found in namespace " + cms.Spec.SourceNamespace
	}

	return r.updateStatusWithCondition(ctx, newCondition, cms)
}

func (r *ConfigMapSyncReconciler) updateStatusWithCondition(ctx context.Context, newCondition metav1.Condition, cms *v1alpha1.ConfigMapSync) error {
	log := log.FromContext(ctx).WithName("Reconcile>updateStatus")

	//TODO use SetStatusCondition!
	cmsCopy := cms.DeepCopy()
	meta.SetStatusCondition(&cmsCopy.Status.Conditions, newCondition)
	// .status should be able to be reconstituted from the state of the world
	// so it's not a good idea to read from the status of the root object. Instead
	// you should reconstruct it every run

	// Update Status
	err := r.Status().Update(ctx, cmsCopy)
	if err != nil {
		log.Error(err, "Failed Updating .status of ConfigMapSync DeepCopy")
		return err
	}

	return nil
}

func (r *ConfigMapSyncReconciler) prepareSourceConfigMap(ctx context.Context, configMapSync *v1alpha1.ConfigMapSync) (sourceCM *v1.ConfigMap, error error) {
	log := log.FromContext(ctx).WithName("prepareSourceConfigMap")
	sourceCM, err := r.getSourceConfigMap(ctx, configMapSync)
	if err != nil {
		r.RunState.sourceConfigMapFound = false
		return nil, err
	}
	r.RunState.sourceConfigMapFound = true

	if err := r.setOwnerRef(configMapSync, sourceCM); err != nil {
		log.Error(err, "Failed setting OwnerRef on Source ConfigMap in memory")
	}

	if err := r.Update(ctx, sourceCM); err != nil {
		log.Error(err, "Failed setting OwnerRef on Source ConfigMap")
	}
	return sourceCM, nil
}

func (r *ConfigMapSyncReconciler) createConfigMaps(ctx context.Context, configMapSync *v1alpha1.ConfigMapSync) error {
	log := log.FromContext(ctx).WithName("createConfigMaps")

	configMapSync = configMapSync.DeepCopy()
	nsList := configMapSync.Spec.SyncToNamespaces
	nsListStr := fmt.Sprintf("%s", nsList)
	//testNumString := fmt.Sprintf("%d", configMapSync.Spec.TestNum)

	log.V(2).Info("Entered with SyncToNamespaces" + nsListStr)

	sourceConfigMap, err := r.prepareSourceConfigMap(ctx, configMapSync)
	if err != nil {
		return err
	}

	configMaps := []*v1.ConfigMap{}
	for _, namespace := range configMapSync.Spec.SyncToNamespaces {
		fmt.Println("Building ConfigMap for Namespace ", namespace)
		cm := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "createdbymycontroller",
				Namespace: namespace,
			},
			//Data["testData"] = "hi",
			// Data: map[string]string{
			// 	"testNum": testNumString,
			// },
			Data: sourceConfigMap.Data,
		}
		if err := r.setOwnerRef(configMapSync, cm); err != nil {
			log.Error(err, "Failed setting OwnerRef")
		}
		configMaps = append(configMaps, cm)
	}

	// Check if configmap already
	// In the Namespace that triggered this reconcile
	log.V(1).Info("create/update ConfigMaps...")
	for i, cm := range configMaps {
		iter := strconv.Itoa(i)
		log.Info(iter + ". Iteration for ns: " + cm.Namespace)
		nsKey := client.ObjectKey{
			Namespace: cm.Namespace,
			Name:      cm.Name,
		}

		cmCluster := cm.DeepCopy()
		log.V(1).Info(iter + ". r.Get() with Objectkey: " + nsKey.String())
		if err := r.Get(ctx, nsKey, cmCluster); err != nil {
			log.Info(iter + ". r.Get() failed; testing if IsNotFound:")
			if apierrors.IsNotFound(err) {
				err := r.Create(ctx, cm)
				if err != nil {
					log.Error(err, "failed creating configmap in namespace "+cm.Namespace)
					return err
				}
				log.Info(iter + ". ConfigMap IsNotFound => Created.")

			}
		} else {
			log.Info(iter + ".r.Get() successful, current cluster cm testnum:" + cmCluster.Data["testNum"])
		}
		if err := r.Update(ctx, cm); err != nil {
			log.Error(err, iter+". r.Update(ctx, cm) failed")
		}
		log.Info(iter + ".ConfigMap " + cm.Name + " Updated. This iteration finished.")
	}

	return nil
}

func (r *ConfigMapSyncReconciler) getSourceConfigMap(ctx context.Context, cms *v1alpha1.ConfigMapSync) (*v1.ConfigMap, error) {
	cm := &v1.ConfigMap{}
	nsKey := client.ObjectKey{
		Namespace: cms.Spec.Source.Namespace,
		Name:      cms.Spec.Source.Name,
	}
	if err := r.Get(ctx, nsKey, cm); err != nil {
		r.RunState.sourceConfigMapFound = false
		return cm, err
	}
	return cm, nil
}

// Deciding against setting owner reference, as the ConifgMapSync will be namespaced for
// easier Multitenancy implementaiton. Instead, if a cross-namespace GC is needed, it can
// be implemented by the controller by using a magic label that all synced namespaces can share
func (r *ConfigMapSyncReconciler) setOwnerRef(owner *v1alpha1.ConfigMapSync, cm *v1.ConfigMap) error {
	//	log := log.FromContext(ctx).WithName("setOwnerRef")

	if err := ctrl.SetControllerReference(owner, cm, r.Scheme); err != nil {
		return err
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ConfigMapSyncReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// Watch ConfigMapSync CR and trigger reconciliation on
		// Add/Update/Delete events
		For(&weaverv1alpha1.ConfigMapSync{}).
		// Watch the ConfigMap managed by the ConfigMapSync controller , also
		// triggereing reconciliation
		// Ah this only works when an ownerReference is set for the configmap! Only then the
		// watch gets triggered
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
