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

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the GlideinSet object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
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
		// TODO!
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

	return ctrl.Result{}, nil
}

func CreateResourcesForGlideinSet(r *GlideinSetReconciler, ctx context.Context, pilotSet *gmosv1alpha1.GlideinSet) error {
	log := log.FromContext(ctx)
	log.Info("Got new value for GlideinSet custom resource!")
	psState := &PilotSetReconcileState{reconciler: r, ctx: ctx, resource: pilotSet}

	log.Info("Creating Tokens secret if not exists")
	if err := CreateResourceIfNotExists(psState, RNGlideinTokens, &corev1.Secret{}, &EmptySecretCreator{}); err != nil {
		return err
	}

	// PilotSet
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
