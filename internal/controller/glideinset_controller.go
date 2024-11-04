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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gmosv1alpha1 "github.com/chtc/gmos-k8s-operator/api/v1alpha1"
)

// GlideinSetReconciler reconciles a GlideinManagerPilotSet object
type GlideinSetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *GlideinSetReconciler) GetClient() client.Client {
	return r.Client
}

func (r *GlideinSetReconciler) GetScheme() *runtime.Scheme {
	return r.Scheme
}

// Reconcile the state of a GlideinSet Custom Resource in the namespace by updating
// its associated child resources
func (r *GlideinSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Running reconcile")

	// Check the namespace for a PilotSet CRD,
	glideinSet := &gmosv1alpha1.GlideinSet{}

	if err := r.Get(ctx, req.NamespacedName, glideinSet); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("GlideinSet resource not found. It is either deleted or not created.")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get GlideinSet")
		return ctrl.Result{}, err
	}

	// Add a finalizer to the pilotSet if it doesn't exist, allowing us to perform cleanup when the
	// pilotSet is deleted
	if !controllerutil.ContainsFinalizer(glideinSet, pilotSetFinalizer) {
		log.Info("Adding finalizer for GlideinManagerPilotSet")
		if !controllerutil.AddFinalizer(glideinSet, pilotSetFinalizer) {
			log.Error(nil, "Failed to add finalizer to GlideinSet")
		}

		if err := r.Update(ctx, glideinSet); err != nil {
			log.Error(err, "Failed to update GlideinSet to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// Check if the pilotSet is marked for deletion, remove its dependent resources if so
	if glideinSet.GetDeletionTimestamp() != nil {
		if !controllerutil.ContainsFinalizer(glideinSet, pilotSetFinalizer) {
			return ctrl.Result{}, nil
		}
		log.Info("Running finalizer on GlideinManagerglideinSet before deletion")

		FinalizeGlideinSet(glideinSet)

		// Refresh the Custom Resource post-finalization
		if err := r.Get(ctx, req.NamespacedName, glideinSet); err != nil {
			log.Error(err, "Failed to get updated GlideinManagerglideinSet after running finalizer operations")
			return ctrl.Result{}, err
		}

		// Remove the finalizer and update the resource
		if !controllerutil.RemoveFinalizer(glideinSet, pilotSetFinalizer) {
			log.Error(nil, "Failed to remove finalizer from GlideinManagerglideinSet")
		}
		if err := r.Update(ctx, glideinSet); err != nil {
			log.Error(err, "Failed to update CRD to remove finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Add the deployment for the pilotSet if it doesn't already exist, or update it if it does
	if err := CreateResourcesForGlideinSet(r, ctx, glideinSet); err != nil {
		return ctrl.Result{}, err
	}
	glState := &PilotSetReconcileState{reconciler: r, ctx: ctx, resource: glideinSet}
	if err := ApplyUpdateToResource(glState, "", &appsv1.Deployment{}, &DeploymentPilotSetUpdater{glideinSet: glideinSet}); err != nil {
		log.Error(err, "Unable to update Deployment for GlideinSet")
		return ctrl.Result{}, err
	}

	AddGlideinManagerWatcher(glideinSet, glState)
	AddCollectorClient(glideinSet, glState)
	return ctrl.Result{}, nil
}

// Remove the GlideinManager client and Collector client for the namespace when the associated GlideinSet
// custom resource is deleted
func FinalizeGlideinSet(glideinSet *gmosv1alpha1.GlideinSet) {
	RemoveGlideinManagerWatcher(glideinSet)
	RemoveCollectorClient(glideinSet)
}

// Place a new set of auth tokens from the local collector into Secrets in the namespace
// A separate set of tokens are generated for the Glidein itself and the EP in the glidein
func (pr *PilotSetReconcileState) ApplyTokensUpdate(glindeinToken string, pilotToken string) error {
	err := ApplyUpdateToResource(pr, RNCollectorTokens, &corev1.Secret{}, &CollectorTokenSecretUpdater{token: glindeinToken})
	err2 := ApplyUpdateToResource(pr, RNTokens, &corev1.Secret{}, &CollectorTokenSecretUpdater{token: pilotToken})
	return errors.Join(err, err2)
}

// Create the set of resources associated with a single Glidein deployment
// - Secret containing access tokens from the local Collector
// - Secret containing data files from the upstream Git repo
// - Secret containing external Collector access tokens provided by the Glidein Manager
// - Empty deployment with volume mounts for the above secrets
func CreateResourcesForGlideinSet(r *GlideinSetReconciler, ctx context.Context, pilotSet *gmosv1alpha1.GlideinSet) error {
	log := log.FromContext(ctx)
	log.Info("Got new value for GlideinSet custom resource!")
	psState := &PilotSetReconcileState{reconciler: r, ctx: ctx, resource: pilotSet}

	log.Info("Creating Collector tokens secret if not exists")
	if err := CreateResourceIfNotExists(psState, RNCollectorTokens, &corev1.Secret{}, &EmptySecretCreator{}); err != nil {
		return err
	}

	log.Info("Creating Data Secret if not exists")
	if err := CreateResourceIfNotExists(psState, RNData, &corev1.Secret{}, &EmptySecretCreator{}); err != nil {
		return err
	}

	log.Info("Creating Access Token Secret if not exists")
	if err := CreateResourceIfNotExists(psState, RNTokens, &corev1.Secret{}, &EmptySecretCreator{}); err != nil {
		return err
	}

	log.Info("Creating Deployment if not exists")
	if err := CreateResourceIfNotExists(psState, RNBase, &appsv1.Deployment{}, &PilotSetDeploymentCreator{}); err != nil {
		return err
	}

	return nil

}

// SetupWithManager sets up the controller with the Manager.
func (r *GlideinSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gmosv1alpha1.GlideinSet{}).
		Complete(r)
}
