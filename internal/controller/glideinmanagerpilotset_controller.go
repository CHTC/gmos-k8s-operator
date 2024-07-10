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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

// GlideinManagerPilotSetReconciler reconciles a GlideinManagerPilotSet object
type GlideinManagerPilotSetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=gmos.chtc.wisc.edu,resources=glideinmanagerpilotsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=gmos.chtc.wisc.edu,resources=glideinmanagerpilotsets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gmos.chtc.wisc.edu,resources=glideinmanagerpilotsets/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete

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

	// Check the namespace for a PilotSet CRD,
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

		r.finalizePilotSet(pilotSet)

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

	// Add the deployment for the pilotSet if it doesn't already exist, or update it if it does

	dep := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: pilotSet.Name, Namespace: pilotSet.Namespace}, dep); err == nil {
		// Deployment found successfully, update it
		updatedDep, err := r.updateDeploymentForPilotSet(dep, pilotSet)
		if err != nil {
			log.Error(err, "Unable to update Deployment for PilotSet")
			return ctrl.Result{}, err
		}
		if updatedDep {
			if err := r.Update(ctx, dep); err != nil {
				log.Error(err, "Failed to update Deployment for PilotSet")
				return ctrl.Result{}, err
			}
		}
		r.addPilotSetCallbacks(ctx, pilotSet)
		log.Info("Updated Deployment for PilotSet")
	} else if apierrors.IsNotFound(err) {
		// Deployment doesn't exist, create it
		tokenSec, err1 := r.makeSecretForPilotSet(pilotSet, "-tokens")
		dataSec, err2 := r.makeSecretForPilotSet(pilotSet, "-data")
		newDep, err3 := r.makeDeploymentForPilotSet(pilotSet)
		if err := errors.Join(err1, err2, err3); err != nil {
			log.Error(err, "Unable to build schema for new resources for pilotSet")
			return ctrl.Result{}, err
		}
		// Deployment depends on secret, create serially
		if err := r.Create(ctx, dataSec); err != nil {
			log.Error(err, "Failed to add data secret to PilotSet")
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, tokenSec); err != nil {
			log.Error(err, "Failed to add access secret to PilotSet")
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, newDep); err != nil {
			log.Error(err, "Failed to add deployment to PilotSet")
			return ctrl.Result{}, err
		}
		r.addPilotSetCallbacks(ctx, pilotSet)
		log.Info("Created new resources for PilotSet")
	} else {
		log.Error(err, "Unable to check status of Deployment for PilotSet")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *GlideinManagerPilotSetReconciler) finalizePilotSet(pilotSet *gmosv1alpha1.GlideinManagerPilotSet) {
	// TODO
	RemoveGlideinManagerWatcher(pilotSet)
}

func (r *GlideinManagerPilotSetReconciler) addPilotSetCallbacks(ctx context.Context, pilotSet *gmosv1alpha1.GlideinManagerPilotSet) {
	log := log.FromContext(ctx)
	AddGlideinManagerWatcher(pilotSet,
		func(ru gmosClient.RepoUpdate) error {
			return r.updateResourcesFromGitCommit(ctx, pilotSet.Name, pilotSet.Namespace, ru)
		},
		func(sv gmosClient.SecretValue) error {
			log.Info("Secret updated to version " + sv.Version)
			sec := &corev1.Secret{}
			return ApplyUpdateToResource(r, ctx, pilotSet.Name+"-tokens", pilotSet.Namespace, sec, &TokenSecretValueUpdater{secValue: &sv})
		})
}

type ResourceUpdater[T client.Object] interface {
	UpdateResourceValue(*GlideinManagerPilotSetReconciler, T) (bool, error)
}

func ApplyUpdateToResource[T client.Object](
	r *GlideinManagerPilotSetReconciler, ctx context.Context, name string, namespace string, resource T, updater ResourceUpdater[T]) error {
	log := log.FromContext(ctx)
	if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, resource); err == nil {
		updated, err := updater.UpdateResourceValue(r, resource)
		if err != nil {
			log.Error(err, "Unable to apply update to resource value")
			return err
		}
		if !updated {
			log.Info("No updates needed for resource")
			return nil
		}
		if err := r.Update(ctx, resource); err != nil {
			log.Error(err, "Unable to post update to resource")
			return err
		}
	} else if apierrors.IsNotFound(err) {
		log.Info("Resource not found, must have been deleted")
	} else {
		return err
	}
	return nil
}

func (r *GlideinManagerPilotSetReconciler) updateResourcesFromGitCommit(ctx context.Context, name string, namespace string, gitUpdate gmosClient.RepoUpdate) error {
	log := log.FromContext(ctx)
	log.Info("Got repo update!")

	log.Info("Updating data Secret")
	sec := &corev1.Secret{}
	if err := ApplyUpdateToResource(r, ctx, name+"-data", namespace, sec, &DataSecretGitUpdater{gitUpdate: &gitUpdate}); err != nil {
		return err
	}

	log.Info("Updating access token Secret")
	sec2 := &corev1.Secret{}
	if err := ApplyUpdateToResource(r, ctx, name+"-tokens", namespace, sec2, &TokenSecretGitUpdater{gitUpdate: &gitUpdate}); err != nil {
		return err
	}

	log.Info("Updating Deployment")
	dep := &appsv1.Deployment{}
	if err := ApplyUpdateToResource(r, ctx, name, namespace, dep, &DeploymentGitUpdater{gitUpdate: &gitUpdate}); err != nil {
		return err
	}

	return nil

}

// SetupWithManager sets up the controller with the Manager.
func (r *GlideinManagerPilotSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gmosv1alpha1.GlideinManagerPilotSet{}).
		Complete(r)
}
