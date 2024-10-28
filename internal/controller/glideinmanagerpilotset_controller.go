/*
Copyright 2024.

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
	"errors"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gmosClient "github.com/chtc/gmos-client/client"
	gmosv1alpha1 "github.com/chtc/gmos-k8s-operator/api/v1alpha1"
)

const pilotSetFinalizer = "gmos.chtc.wisc.edu/finalizer"

type Reconciler interface {
	GetClient() client.Client
	GetScheme() *runtime.Scheme
}

// GlideinManagerPilotSetReconciler reconciles a GlideinManagerPilotSet object
type GlideinManagerPilotSetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *GlideinManagerPilotSetReconciler) GetClient() client.Client {
	return r.Client
}

func (r *GlideinManagerPilotSetReconciler) GetScheme() *runtime.Scheme {
	return r.Scheme
}

//+kubebuilder:rbac:groups=gmos.chtc.wisc.edu,namespace=memcached-operator-system,resources=glideinmanagerpilotsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=gmos.chtc.wisc.edu,namespace=memcached-operator-system,resources=glideinmanagerpilotsets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gmos.chtc.wisc.edu,namespace=memcached-operator-system,resources=glideinmanagerpilotsets/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,namespace=memcached-operator-system,resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=apps,namespace=memcached-operator-system,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,namespace=memcached-operator-system,resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,namespace=memcached-operator-system,resources=pods/exec,verbs=create
//+kubebuilder:rbac:groups=core,namespace=memcached-operator-system,resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,namespace=memcached-operator-system,resources=configmaps,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the GlideinManagerPilotSet object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *GlideinManagerPilotSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Running reconcile")

	pilotSet := &gmosv1alpha1.GlideinManagerPilotSet{}

	if err := r.Get(ctx, req.NamespacedName, pilotSet); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("GlideinManagerPilotSet resource not found. It is either deleted or not created.")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get GlideinManagerPilotSet")
		return ctrl.Result{}, err
	}

	// Add a finalizer to the pilotSet if it doesn't exist, allowing us to perform cleanup when the
	// pilotSet is deleted
	if !controllerutil.ContainsFinalizer(pilotSet, pilotSetFinalizer) {
		log.Info("Adding finalizer for GlideinManagerPilotSet")
		if !controllerutil.AddFinalizer(pilotSet, pilotSetFinalizer) {
			log.Error(nil, "Failed to add finalizer to GlideinManagerPilotSet")
		}

		if err := r.Update(ctx, pilotSet); err != nil {
			log.Error(err, "Failed to update CRD to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// Check if the pilotSet is marked for deletion, remove its dependent resources if so
	if pilotSet.GetDeletionTimestamp() != nil {
		if !controllerutil.ContainsFinalizer(pilotSet, pilotSetFinalizer) {
			return ctrl.Result{}, nil
		}
		log.Info("Running finalizer on GlideinManagerPilotSet before deletion")

		FinalizePilotSet(pilotSet)

		// Refresh the Custom Resource post-finalization
		if err := r.Get(ctx, req.NamespacedName, pilotSet); err != nil {
			log.Error(err, "Failed to get updated GlideinManagerPilotSet after running finalizer operations")
			return ctrl.Result{}, err
		}

		// Remove the finalizer and update the resource
		if !controllerutil.RemoveFinalizer(pilotSet, pilotSetFinalizer) {
			log.Error(nil, "Failed to remove finalizer from GlideinManagerPilotSet")
		}
		if err := r.Update(ctx, pilotSet); err != nil {
			log.Error(err, "Failed to update CRD to remove finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if err := CreateResourcesForPilotSet(r, ctx, pilotSet); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// Remove the Glidein Manager Watcher for the namespace when its custom resource is deleted
func FinalizePilotSet(pilotSet *gmosv1alpha1.GlideinManagerPilotSet) {
	// RemoveGlideinManagerWatcher(pilotSet)
	// RemoveCollectorClient(pilotSet)
}

// Struct that captures the state of a PilotSet at the time of Reconciliation.
// (Potentially improperly) held in memory and used to update the PilotSet's child
// resources outside of the Reconciliation loop, eg. upon a git update
type PilotSetReconcileState struct {
	ctx        context.Context
	resource   metav1.Object
	reconciler Reconciler
}

// Check the current state of resources in the PilotSet's namespace to determine
// whether a new set of ID tokens need to be generated via the local collector
func (pr *PilotSetReconcileState) ShouldUpdateTokens() (bool, error) {
	glideinSec := corev1.Secret{}
	epSec := corev1.Secret{}
	glideinErr := pr.reconciler.GetClient().Get(pr.ctx, types.NamespacedName{Name: RNGlideinTokens.NameFor(pr.resource), Namespace: pr.resource.GetNamespace()}, &glideinSec)
	epErr := pr.reconciler.GetClient().Get(pr.ctx, types.NamespacedName{Name: RNTokens.NameFor(pr.resource), Namespace: pr.resource.GetNamespace()}, &epSec)
	if epErr == nil && glideinErr == nil {
		// TODO check token expiration. For now just check whether secret is populated
		for _, sec := range []corev1.Secret{glideinSec, epSec} {
			if _, exists := sec.Data["cluster.tkn"]; !exists {
				return true, nil
			}
		}
		return false, nil
	} else if apierrors.IsNotFound(glideinErr) && apierrors.IsNotFound(epErr) {
		// Need to wait for resources to be recreated in next reconcile loop
		return false, nil
	} else {
		return false, errors.Join(epErr, glideinErr)
	}
}

// Update the PilotSet's children based on new data in its Glidein Manager's
// git repository
func (pr *PilotSetReconcileState) ApplyGitUpdate(gitUpdate gmosClient.RepoUpdate) error {
	log := log.FromContext(pr.ctx)
	log.Info("Got repo update!")

	log.Info("Updating data Secret")
	if err := ApplyUpdateToResource(pr, RNData, &corev1.Secret{}, &DataSecretGitUpdater{gitUpdate: &gitUpdate}); !updateErrOk(err) {
		return err
	}

	// log.Info("Updating access token Secret")
	// if err := ApplyUpdateToResource(pr, RNTokens, &corev1.Secret{}, &TokenSecretGitUpdater{gitUpdate: &gitUpdate}); !updateErrOk(err) {
	// 	return err
	// }

	log.Info("Updating Deployment")
	if err := ApplyUpdateToResource(pr, RNBase, &appsv1.Deployment{}, &DeploymentGitUpdater{gitUpdate: &gitUpdate}); !updateErrOk(err) {
		return err
	}

	return nil
}

// Update the PilotSet's children based on new data in its Glidein Manager's
// secret store
func (pu *PilotSetReconcileState) ApplySecretUpdate(secSource PilotSetSecretSource, sv gmosClient.SecretValue) error {
	log := log.FromContext(pu.ctx)
	log.Info("Secret updated to version " + sv.Version)
	return ApplyUpdateToResource(pu, RNTokens, &corev1.Secret{}, &TokenSecretValueUpdater{secSource: &secSource, secValue: &sv})
}

func CreateResourcesForPilotSet(r *GlideinManagerPilotSetReconciler, ctx context.Context, pilotSet *gmosv1alpha1.GlideinManagerPilotSet) error {
	log := log.FromContext(ctx)
	log.Info("Got new value for PilotSet custom resource!")
	psState := &PilotSetReconcileState{reconciler: r, ctx: ctx, resource: pilotSet}

	// Collector resources
	log.Info("Creating Collector Signing Key if not exists")
	if err := CreateResourceIfNotExists(psState, RNCollectorSigkey, &corev1.Secret{}, &CollectorSigningKeyCreator{}); err != nil {
		return err
	}

	log.Info("Creating Collector ConfigMap if not exists")
	if err := CreateResourceIfNotExists(psState, RNCollectorConfig, &corev1.ConfigMap{}, &CollectorConfigMapCreator{}); err != nil {
		return err
	}

	log.Info("Creating Collector Deployment if not exists")
	if err := CreateResourceIfNotExists(psState, RNCollector, &appsv1.Deployment{}, &CollectorDeploymentCreator{}); err != nil {
		return err
	}

	log.Info("Creating Collector Service if not exists")
	if err := CreateResourceIfNotExists(psState, RNCollector, &corev1.Service{}, &CollectorServiceCreator{}); err != nil {
		return err
	}

	log.Info("Updating GlideinSets")
	if err := recoincileGlideinSets(pilotSet, psState); err != nil {
		return err
	}

	return nil
}

// Create any GlideinSets in the spec that don't exist yet, update ones that already exist,
// and delete ones that don't exist
func recoincileGlideinSets(pilotSet *gmosv1alpha1.GlideinManagerPilotSet, psState *PilotSetReconcileState) error {
	log := log.FromContext(psState.ctx)
	// Create/update all present GlideinSets
	for _, glideinSetSpec := range pilotSet.Spec.GlideinSets {
		glideinSetSpec.GlideinManagerUrl = pilotSet.Spec.GlideinManagerUrl
		gsResource := ResourceName("-" + glideinSetSpec.Name)
		// TODO this double dips API calls by checking for existence on update then checking again on create
		creator := &GlideinSetCreator{spec: &glideinSetSpec}
		err := ApplyUpdateToResource(psState, gsResource, &gmosv1alpha1.GlideinSet{}, creator)
		if apierrors.IsNotFound(err) {
			if err := CreateResourceIfNotExists(psState, gsResource, &gmosv1alpha1.GlideinSet{}, creator); err != nil {
				return err
			}
		}
	}

	// Remove stale GlideinSets
	// TODO
	glideinSetList := gmosv1alpha1.GlideinSetList{}
	psState.reconciler.GetClient().List(
		psState.ctx, &glideinSetList,
		client.InNamespace(pilotSet.Namespace), client.HasLabels([]string{"app.kubernetes.io/instance=" + pilotSet.Name}))

	for _, glideinSet := range glideinSetList.Items {
		log.Info(fmt.Sprintf("Found GlideinSet with name %v via API call!", glideinSet.Name))
		outdated := true
		for _, currentSet := range pilotSet.Spec.GlideinSets {
			if glideinSet.Name == pilotSet.Name+"-"+currentSet.Name {
				outdated = false
				break
			}
		}

		if outdated {
			log.Info(fmt.Sprintf("GlideinSet %v has been removed, deleting it.", glideinSet.Name))
			if err := psState.reconciler.GetClient().Delete(psState.ctx, &glideinSet); err != nil {
				log.Error(err, "Unable to delete stale GlideinSet")
				return err
			}
		}
	}

	return nil
}

func updateErrOk(err error) bool {
	return err == nil || apierrors.IsNotFound(err)
}

// SetupWithManager sets up the controller with the Manager.
func (r *GlideinManagerPilotSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gmosv1alpha1.GlideinManagerPilotSet{}).
		Complete(r)
}
