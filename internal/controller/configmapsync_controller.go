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
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/k-stz/config-weaver-operator/api/v1alpha1"
	weaverv1alpha1 "github.com/k-stz/config-weaver-operator/api/v1alpha1"
	authorizationapi "k8s.io/api/authorization/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	//allNamespaces is used for determining cluster scoped bindings
	// used to create SAR requests
	allNamespaces                = ""
	ConditionTypeReady           = "Ready"
	ReasonUnknownState           = "UnknownState"
	ReasonReconciliationComplete = "ReconciliationComplete"
)

// ConfigMapSyncReconciler reconciles a ConfigMapSync object
type ConfigMapSyncReconciler struct {
	client.Client // from manager
	Scheme        *runtime.Scheme
	Recorder      record.EventRecorder
	Config        *rest.Config
	// use for lower level requests when client.Client is insufficient
	// For example when creating TokenRequest, a subreosurce of serviceaccount, the go-client has a naive funtion for that
	Clientset kubernetes.Interface //  Clientset struct, returned by kubernetes.NewForConfig(r.Config)
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
	cms := &v1alpha1.ConfigMapSync{}
	if err := r.Get(ctx, req.NamespacedName, cms); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "unable to get configMapSync")
			// other error, requeue with exponential back-off
			return ctrl.Result{}, err
		}
	}
	// we create the DeepCopy right of the bat Because the read CMS comes from
	// the a "shared informer" cache (controller-runtime construct) we create a
	// deepdopy here such that concurrent Reconcile invokation don't share this
	// memory avoiding race-conditions
	cms = cms.DeepCopy()

	// r.removeStaleStatuses() // maybe at this point
	readyCond := NewCondition(ConditionTypeReady, metav1.ConditionUnknown, cms.Generation, ReasonUnknownState, "")
	defer func() {
		r.updateStatus(ctx, cms, readyCond)
	}()
	log.V(1).Info(fmt.Sprint("ConfigMapSync testNum:", cms.Spec.TestNum))
	// So now we have a ConfigMapSync object,
	// First test if spec.serviceAccount is valid
	sa, err := r.getServiceAccountFromCMS(ctx, cms)
	if err != nil {
		readyCond.Reason = "Couldn't get get ServiceAccount"
		readyCond.Message = err.Error()
		return ctrl.Result{}, err
	}
	log.V(1).Info("serviceaccount successfully retrieved", "Content", sa)
	if err := r.validateServiceAccountPermissions(ctx, sa, cms); err != nil {
		// TODO either in the method, or here, set the status.condition indicating the failed
		// SA validation and the reason!
		readyCond.Reason = "serviceaccount permission insufficient"
		readyCond.Message = err.Error()
		return ctrl.Result{}, err
	}

	// Get Service Account Token on whose behalf the configmap synching will take place
	r.getToken(ctx, sa)

	// Testing stuff
	// r.runExperiment(ctx)
	// saObjectKey := client.ObjectKeyFromObject(sa)

	// Next we try to create a configmap
	var configMapsSyncedCondition metav1.Condition = NewCondition("AllTargetsSynced", metav1.ConditionTrue, cms.Generation, "AllSyncsSuccessful", "")
	if err := r.createConfigMaps(ctx, cms); err != nil {
		log.Error(err, "unable to create ConfigMaps; Updating status...")
		configMapsSyncedCondition.Status = metav1.ConditionFalse
		configMapsSyncedCondition.Reason = "SyncsFailed"
		configMapsSyncedCondition.Message = "Failed to attempt to sync all target ConfigMaps"
		meta.SetStatusCondition(&cms.Status.Conditions, configMapsSyncedCondition)
		readyCond.Reason = "ConfigMaps couldn't be synced"
		readyCond.Message = err.Error()
		return ctrl.Result{}, err
	}
	meta.SetStatusCondition(&cms.Status.Conditions, configMapsSyncedCondition)

	// cluster-logging-forwarder code uses this:
	// removeStaleStatuses(r.Forwarder)
	//
	// readyCond := internalobs.NewCondition(obsv1.ConditionTypeReady, obsv1.ConditionUnknown, obsv1.ReasonUnknownState, "")
	// defer func() {
	// 	updateStatus(r.Client, r.Forwarder, readyCond)
	// }()

	// r.updateStatus(ctx, cms)

	// All successfully reconciled, golden path reached
	readyCond.Reason = ReasonReconciliationComplete
	readyCond.Status = metav1.ConditionTrue

	// No error => stops Reconcile
	return ctrl.Result{}, nil

	// To reconcile again after X time
	// thus implementing best practice of
	// return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *ConfigMapSyncReconciler) runExperiment(ctx context.Context) {
	log := log.FromContext(ctx).WithName("EXPERIMENTS")
	log.V(1).Info("starting experiments")
	log.V(1).Info("make request for nodes")

	// 3rd paramter to r.Client.List() interface wanting method ApplyToList(*ListOptions)
	// ListOptions is a struct that can e.g. filter by labels
	nodeList := v1.NodeList{}
	fmt.Println("### r.List:", nodeList)

	// if err := r.List(ctx, &nodeList, ApplyToListFunc(f)); err != nil {
	// 	fmt.Println("### r.List PANICs:", nodeList)

	// 	panic(err)
	// }
	// 	List(ctx context.Context, list ObjectList, opts ...ListOption) error

	if err := r.List(ctx, &nodeList,
		client.MatchingLabels{"kubernetes.io/hostname": "k3d-mycluster-agent-0"}); err != nil {
		fmt.Println("### r.List PANICs:", nodeList)
		panic(err)
	}

	fmt.Println("### NODELIST:", nodeList)

}

func (r *ConfigMapSyncReconciler) getToken(ctx context.Context, sa *v1.ServiceAccount) (token string, error error) {
	log := log.FromContext(ctx).WithName("[getToken]")
	log.V(1).Info("attempting to retrive Token", "sa", ServiceaAccountUsername(sa))

	//io.k8s.api.authentication.v1.TokenRequest

	return
}

func ServiceaAccountUsername(sa *v1.ServiceAccount) (username string) {
	return fmt.Sprintf("system:serviceaccount:%s:%s", sa.Namespace, sa.Name)
}

func (r *ConfigMapSyncReconciler) getServiceAccountFromCMS(ctx context.Context, cms *v1alpha1.ConfigMapSync) (*v1.ServiceAccount, error) {
	// TODO: add condition serviceAccountFound
	saName := cms.Spec.ServiceAccount.Name //
	log := log.FromContext(ctx).WithName("getServiceAccountFromCMS")
	log.V(1).Info("try retrieving sa from cms.spec.serviceAccount", "name", saName)

	saObjectKey := types.NamespacedName{
		Name:      saName,
		Namespace: cms.ObjectMeta.Namespace,
	}
	sa := &v1.ServiceAccount{}
	if err := r.Get(ctx, saObjectKey, sa); err != nil {
		log.Error(err, "Failed to retreive sa", "service account Name", saName)
		return nil, err
	}
	return sa, nil
}

// Validates whether given SA can access the resorces required
// Return values: nil when successful
// In case of error .status.condition is set with the namespaces that failed the SAR request
//
// Adapted from  https://github.com/openshift/cluster-logging-operator/internal/validations/observability/validate_permissions.go, licensed under the Apache License 2.0
func (r *ConfigMapSyncReconciler) validateServiceAccountPermissions(ctx context.Context, serviceAccount *v1.ServiceAccount, cms *v1alpha1.ConfigMapSync) error {
	log := log.FromContext(ctx).WithName("validateServiceAccountPermissions")
	var err error
	var username = ServiceaAccountUsername(serviceAccount)
	log.V(1).Info("validating sa", "sa", username)

	//readNamespace := cms.GetNamespace()
	processNamespaces := append(cms.Spec.SyncToNamespaces, cms.Spec.Source.Namespace)

	// Perform subject access reviews for each spec'd input
	// failedNamespaces will list all namespaces for which the SAR failed, meaning the given
	var failedNamespaces []string
	for _, ns := range processNamespaces {
		log.V(3).Info("[ValidateServiceAccountPermissionsWriteNamespaces]", "namespace", ns, "username", username)
		// Resource="" means all, while Group="" implies the default "api" containg ConfigMaps
		sar := createSubjectAccessReview(username, ns, "update", "configmaps", "", "")

		if err = r.Create(context.TODO(), sar); err != nil {
			return err
		}
		log.V(3).Info("[ValidateServiceAccountPermissions] SubjectAccessReview AFTER create", "for namespace", ns, "sar.spec", sar.Spec, "sar.status", sar.Status)
		if !sar.Status.Allowed {
			failedNamespaces = append(failedNamespaces, ns)
		}
	}

	if len(failedNamespaces) > 0 {

		errMsg := fmt.Sprintf("insufficient permissions on service account %s. Not authorized to create, update or delete configmaps in the following namespaces %s", username, failedNamespaces)
		return errors.New(errMsg)
	}

	return nil
}

// Adapted from https://github.com/openshift/cluster-logging-operator/internal/validations/observability/validate_permissions.go, licensed under the Apache License 2.0 just like this code
func createSubjectAccessReview(user, namespace, verb, resource, name, resourceAPIGroup string) *authorizationapi.SubjectAccessReview {
	sar := &authorizationapi.SubjectAccessReview{
		Spec: authorizationapi.SubjectAccessReviewSpec{
			User: user,
		},
	}
	if strings.HasPrefix(resource, "/") {
		sar.Spec.NonResourceAttributes = &authorizationapi.NonResourceAttributes{
			Path: resource,
			Verb: verb,
		}
	} else {
		sar.Spec.ResourceAttributes = &authorizationapi.ResourceAttributes{
			Resource:  resource,
			Namespace: namespace,
			Group:     resourceAPIGroup,
			Verb:      verb,
			Name:      name,
		}
	}
	// fmt.Printf("###SAR for user=%s ns=%s verb=%s resource=%s name=%s APIgrp=%s \n",
	// 	user, namespace, verb, resource, name, resourceAPIGroup)
	// fmt.Println(MustMarshal(sar))
	return sar
}

// updateStatus is intended to be deferred within a single call to r.Reconcile().
// During the reconciliation, individual condition fields in .Status.Conditions
// should be updated using meta.SetStatusCondition, which ensures that existing
// conditions are updated and new ones are appended as needed.
//
// The "Ready" condition is initialized as Unknown at the start of reconciliation
// and is captured by a closure. As reconciliation progresses, this condition is
// updated to reflect the final state (e.g., True, False, etc.).
//
// External impact: This function, when executed (typically via defer), will patch the .Status.Conditions
// back to the Kubernetes API, ensuring the reconciled resource reflects its final status.
func (r *ConfigMapSyncReconciler) updateStatus(ctx context.Context, cms *v1alpha1.ConfigMapSync, ready metav1.Condition) {
	log := log.FromContext(ctx).WithName("[updateStatus]")

	// will add condition if missing; and update if present!
	meta.SetStatusCondition(&cms.Status.Conditions, ready)

	err := r.Status().Update(ctx, cms)
	if err != nil {
		log.Error(err, "failed updating .status.conditions")
	}
}

func NewCondition(conditionType string, status metav1.ConditionStatus, observedGeneration int64, reason, message string) metav1.Condition {
	return metav1.Condition{
		Type:               conditionType,
		Status:             status,
		ObservedGeneration: observedGeneration,
		Reason:             reason,
		Message:            message,
	}
}

// JSONString returns a JSON string of a value, or an error message.
// Indented output for flat json inputs
// Careful: this apparently attempts to print all the fields of an object, even the status field
// interpreting a missing fields as the zero value, even when the object wasn't created yet against
// the api
func MustMarshal(v interface{}) (value string) {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		log.Log.V(0).WithName("MustMarshal").Error(err, "unable to marshal object", "object", v)
		return ""
	}
	return string(out)
}

// Fetch source ConfigMap and ensure the OwnerRef is set for and attempting to Update() it
// Otherwise error out
func (r *ConfigMapSyncReconciler) prepareSourceConfigMap(ctx context.Context, cms *v1alpha1.ConfigMapSync) (sourceCM *v1.ConfigMap, error error) {

	var sourceCMSFoundCond metav1.Condition = NewCondition("SourceConfigMapFound", metav1.ConditionTrue, cms.Generation, "", "")

	log := log.FromContext(ctx).WithName("prepareSourceConfigMap")
	log.V(1).Info("attempting to get sourceConfigMap")
	sourceCM, err := r.getSourceConfigMap(ctx, cms)
	if err != nil {
		NewCondition("SourceConfigMapFound", metav1.ConditionTrue, cms.Generation, "ConfigMapMissing", "Source ConfigMap not found in namespace "+cms.Spec.Source.Namespace)
		sourceCMSFoundCond.Status = metav1.ConditionFalse
		sourceCMSFoundCond.Reason = "ConfigMapMissing"
		sourceCMSFoundCond.Message = "Source ConfigMap not found in namespace " + cms.Spec.Source.Namespace

		meta.SetStatusCondition(&cms.Status.Conditions, sourceCMSFoundCond)
		return nil, err
	}
	sourceCMSFoundCond.Reason = "ConfigMapFound"
	meta.SetStatusCondition(&cms.Status.Conditions, sourceCMSFoundCond)

	r.setOwnerMetadata(cms, sourceCM)
	log.V(3).Info("setOwnerMetadata on sourceCM", "sourceCM", sourceCM)

	if err := r.Update(ctx, sourceCM); err != nil {
		log.Error(err, "Failed setting OwnerMetadata on Source ConfigMap")
	}
	return sourceCM, nil
}

func (r *ConfigMapSyncReconciler) createConfigMaps(ctx context.Context, cms *v1alpha1.ConfigMapSync) error {
	log := log.FromContext(ctx).WithName("createConfigMaps")

	nsList := cms.Spec.SyncToNamespaces
	nsListStr := fmt.Sprintf("%s", nsList)

	log.V(2).Info("Entered with SyncToNamespaces" + nsListStr)

	sourceConfigMap, err := r.prepareSourceConfigMap(ctx, cms)
	if err != nil {
		return err
	}

	configMaps := []*v1.ConfigMap{}
	for _, namespace := range cms.Spec.SyncToNamespaces {
		log.V(3).Info("Building ConfigMap for Namespace ", "namespace", namespace)
		cm := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      sourceConfigMap.GetName(),
				Namespace: namespace,
			},
			Data: sourceConfigMap.Data,
		}
		r.setOwnerMetadata(cms, cm)

		configMaps = append(configMaps, cm)
	}

	// Check if configmap already in the Namespace that triggered this reconcile
	log.V(1).Info("create/update ConfigMaps...")
	for i, cm := range configMaps {
		iter := strconv.Itoa(i)
		log.Info(iter + ". Iteration for ns: " + cm.Namespace + " with name:" + cm.Name)
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
		return cm, err
	}
	return cm, nil
}

// Deciding against setting owner reference, as the ConifgMapSync will be namespaced for
// easier Multitenancy implementaiton. Instead, if a cross-namespace GC is needed, it can
// be implemented by the controller by using a magic label that all synced namespaces can share
// Updating an object with an ownerRef that has a namespaced owner will fail
func (r *ConfigMapSyncReconciler) setOwnerRef(owner *v1alpha1.ConfigMapSync, cm *v1.ConfigMap) error {
	//	log := log.FromContext(ctx).WithName("setOwnerRef")
	if err := ctrl.SetControllerReference(owner, cm, r.Scheme); err != nil {
		return err
	}
	return nil
}

// Sets Annotation/label on ConfigMap pointing to the ConfigMapSync Object that manages it.
// This is used in the Watch request-enqueue-logic to trigger reconciliation on the ConfigMap
// by reconciling the ConfigMapSync that is written in the Annotation or Labels set here
//
// setOwnerRef doesn't work anymore for namespaced ConfigMapSync, we need the a custom
// kubebuilder-watcher, which in turn needs this auxiiliary reference via labels/annnotations
// to back refernce a configmap to the namespace and name of the owning ConfigMapSync
//
// Isn't the labels/annotations, only set, you need to call Update separately!
func (r *ConfigMapSyncReconciler) setOwnerMetadata(associatedCMS *v1alpha1.ConfigMapSync, cm *v1.ConfigMap) {
	name := associatedCMS.GetName()
	namespace := associatedCMS.GetNamespace()
	namespacedName := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	// Need to catch whether annotations are nil, else assignment will panic
	if cm.Annotations == nil {
		cm.Annotations = make(map[string]string)
	}
	cm.GetAnnotations()["configmapsync.io/owner-name"] = name
	cm.GetAnnotations()["configmapsync.io/owner-namespace"] = namespace
	// Need to catch whether labels are nil, else assignment will panic
	if cm.Labels == nil {
		cm.Labels = make(map[string]string)
	}
	cm.GetLabels()["cmsOwnerName"] = namespacedName.Name
	cm.GetLabels()["cmsOwnerNamespace"] = namespacedName.Namespace
}

// Triggers reconciliation only on UPDATE events AND when the ConfigMaps Data fields changed!
// Triggers reconciliation for all events, except for Updates, here it triggers only when
// the data field changed. Use it in
//
// Decided to use Filtering based on labels so client filtering on label is possible
// for debugging and anlysis (kubectl get cm -l configmapsync.io.ownership)
var updatePredConfigMap predicate.Funcs = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		// TODO maybe also implement this logic?
		// if ! hasOwnerAnnotations((*v1.ConfigMap)) {
		// 	return false
		// }

		oldObj := e.ObjectOld.(*v1.ConfigMap)
		newObj := e.ObjectNew.(*v1.ConfigMap)

		// Trigger reconciliation only ConfigMap Data changes
		changed := maps.Equal(oldObj.Data, newObj.Data)
		if val, ok := e.ObjectOld.GetLabels()["skip"]; ok && val == "true" {
			fmt.Println("##updatePredConfigMap Predicate: Filtered out event because skip=true label set! ")
			fmt.Println("## ", e.ObjectOld.GetName(), "/", e.ObjectOld.GetNamespace())
			return false
		}
		if !changed {
			return true
		}
		fmt.Println("##updatePredConfigMap Predicate:  filtered out event because .data field didn't change!")
		fmt.Println("## ", e.ObjectOld.GetName(), "/", e.ObjectOld.GetNamespace())
		return false
	},

	// Allow create events
	CreateFunc: func(e event.CreateEvent) bool {
		return true
	},

	// Allow delete events
	DeleteFunc: func(e event.DeleteEvent) bool {
		return true
	},

	// Allow generic events (e.g., external triggers)
	GenericFunc: func(e event.GenericEvent) bool {
		return true
	},
}

// SetupWithManager sets up the controller with the Manager.
func (r *ConfigMapSyncReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// Watch ConfigMapSync CR and trigger reconciliation on
		// Add/Update/Delete events
		For(&weaverv1alpha1.ConfigMapSync{}).
		// Problem: this no longer works for configmaps in namespaces other then the configmapsync object
		// Watch the ConfigMap managed by the ConfigMapSync controller , also
		// triggering reconciliation
		// Ah this only works when an ownerReference is set for the configmap! Only then the
		// watch gets triggered
		//Owns(&v1.ConfigMap{}).
		//		WatchesRawSource(object client.Object, eventHandler handler.TypedEventHandler[client.Object, reconcile.Request], opts ...builder.WatchesOption)
		Watches(&v1.ConfigMap{}, // watch ConfigMaps
			// This allows us to provide a function and implement the mapping between an event
			// and which Reconciler shall receive it! This is exactly what we need!
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				// This function runs on every watch event of a ConfigMap in the cluster
				// prefiltered by a Predicate.
				// We want it to only trigger on specific ConfigMaps, namely those synced
				// by our controllers!
				annotations := obj.GetAnnotations()
				name, nameOk := annotations["configmapsync.io/owner-name"]
				namespace, nsOk := annotations["configmapsync.io/owner-namespace"]

				if nameOk && nsOk {
					// TODO wrap in debug logs of high level
					fmt.Println("##EnqueueRequestsFromMapFunc Enqueueing using new annotation works, name/ns", name, "/", namespace)
					return []reconcile.Request{
						{
							// maps the watched event the reconciler of specified object!
							NamespacedName: types.NamespacedName{
								Name:      name,
								Namespace: namespace,
							},
						},
					}
				}
				// If the label is not present or doesn't match, don't trigger reconciliation!
				return []reconcile.Request{} // = don't trigger reconcile!
			}),
			// Predicate for efficiency
			builder.WithPredicates(updatePredConfigMap),
		).

		// You can set many more options here, for example the number
		// of concurrent reconciles (default is one) with:
		WithOptions(controller.Options{MaxConcurrentReconciles: 0}).
		Complete(r)
}
