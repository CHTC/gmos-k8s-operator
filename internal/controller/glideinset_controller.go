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
	_ "embed"
	"errors"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gmosClient "github.com/chtc/gmos-client/client"
	gmosv1alpha1 "github.com/chtc/gmos-k8s-operator/api/v1alpha1"
)

// GlideinSetReconciler reconciles a GlideinSet object
type GlideinSetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *GlideinSetReconciler) getClient() client.Client {
	return r.Client
}

func (r *GlideinSetReconciler) getScheme() *runtime.Scheme {
	return r.Scheme
}

// Reconcile the state of a GlideinSet Custom Resource in the namespace by updating
// its associated child resources
func (r *GlideinSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	log := log.FromContext(ctx)
	log.Info("Running reconcile")

	// Check the namespace for a PilotSet CRD,
	glideinSet := &gmosv1alpha1.GlideinSet{}

	if err = r.Get(ctx, req.NamespacedName, glideinSet); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("GlideinSet resource not found. It is either deleted or not created.")
			return result, nil
		}
		log.Error(err, "Failed to get GlideinSet")
		return
	}

	// Add a finalizer to the pilotSet if it doesn't exist, allowing us to perform cleanup when the
	// pilotSet is deleted
	if !controllerutil.ContainsFinalizer(glideinSet, pilotSetFinalizer) {
		log.Info("Adding finalizer for GlideinSet")
		if !controllerutil.AddFinalizer(glideinSet, pilotSetFinalizer) {
			log.Error(nil, "Failed to add finalizer to GlideinSet")
		}

		if err = r.Update(ctx, glideinSet); err != nil {
			log.Error(err, "Failed to update GlideinSet to add finalizer")
			return
		}
	}

	// Check if the pilotSet is marked for deletion, remove its dependent resources if so
	if glideinSet.GetDeletionTimestamp() != nil {
		if !controllerutil.ContainsFinalizer(glideinSet, pilotSetFinalizer) {
			return
		}
		log.Info("Running finalizer on GlideinManagerglideinSet before deletion")

		finalizeGlideinSet(glideinSet)

		// Refresh the Custom Resource post-finalization
		if err = r.Get(ctx, req.NamespacedName, glideinSet); err != nil {
			log.Error(err, "Failed to get updated GlideinManagerglideinSet after running finalizer operations")
			return
		}

		// Remove the finalizer and update the resource
		if !controllerutil.RemoveFinalizer(glideinSet, pilotSetFinalizer) {
			log.Error(nil, "Failed to remove finalizer from GlideinManagerglideinSet")
		}
		if err = r.Update(ctx, glideinSet); err != nil {
			log.Error(err, "Failed to update CRD to remove finalizer")
			return
		}
		return
	}

	// Add the deployment and secrets for the pilotSet if it doesn't already exist
	if err = createResourcesForGlideinSet(r, ctx, glideinSet); err != nil {
		return
	}

	// Update the deployment and git-driven secrets for the pilotSet
	if err = updateResourcesForGlideinSet(r, ctx, glideinSet); err != nil {
		return
	}

	glState := &PilotSetReconcileState{reconciler: r, ctx: ctx, resource: glideinSet}
	if err = addGlideinManagerWatcher(glideinSet, glState); err != nil {
		return
	}
	if err = addCollectorClient(glideinSet, glState); err != nil {
		return
	}

	return
}

// Remove the GlideinManager client and Collector client for the namespace when the associated GlideinSet
// custom resource is deleted
func finalizeGlideinSet(glideinSet *gmosv1alpha1.GlideinSet) {
	removeGlideinManagerWatcher(glideinSet)
	removeCollectorClient(glideinSet)
}

// Update the GlideinSet's RemoteManifest field based on new data in its Glidein Manager's git repository
func (pr *PilotSetReconcileState) applyGitUpdate(gitUpdate gmosClient.RepoUpdate) error {
	log := log.FromContext(pr.ctx)
	log.Info("Got repo update!")

	log.Info("Updating GlideinSet remote manifest")
	err := applyUpdateToResource(pr, RNBase, &gmosv1alpha1.GlideinSet{}, &GlideinSetGitUpdater{gitUpdate: &gitUpdate})
	if !updateErrOk(err) {
		return err
	}
	return nil
}

// Update the GlideinSet's children based on new data in its Glidein Manager's
// secret store
func (pu *PilotSetReconcileState) applySecretUpdate(secSource gmosv1alpha1.PilotSetSecretSource, sv gmosClient.SecretValue) error {
	log := log.FromContext(pu.ctx)
	log.Info("Secret updated to version " + sv.Version)
	return applyUpdateToResource(pu, RNTokens, &corev1.Secret{}, &TokenSecretValueUpdater{secSource: &secSource, secValue: &sv})
}

// Retrieve the current
func (pr *PilotSetReconcileState) getGitSyncState() (*gmosv1alpha1.PilotSetNamespaceConfig, error) {
	log := log.FromContext(pr.ctx)
	log.Info("Retrieving current Git sync state from GlideinSet")
	currentConfig := gmosv1alpha1.GlideinSet{}
	if err := getResourceValue(pr, RNBase, &currentConfig); err != nil {
		log.Error(err, fmt.Sprintf("Unable to retrieve Git sync state from GlideinSet %v", pr.resource.GetName()))
		return nil, err
	}
	return currentConfig.RemoteManifest, nil
}

func (pr *PilotSetReconcileState) getSecretSyncState() (string, error) {
	log := log.FromContext(pr.ctx)
	sec := corev1.Secret{}
	if err := getResourceValue(pr, RNTokens, &sec); err != nil {
		log.Error(err, "Unable to retrieve upstream version from Secret")
		return "", err
	}
	return sec.Labels["gmos.chtc.wisc.edu/secret-version"], nil
}

// Place a new set of auth tokens from the local collector into Secrets in the namespace
// A separate set of tokens are generated for the Glidein itself and the EP in the glidein
func (pr *PilotSetReconcileState) applyTokensUpdate(glindeinToken string, pilotToken string) error {
	// TODO this occassionally results in the error "the object has been modified; please apply your changes to the latest version and try again"
	err := applyUpdateToResource(pr, RNCollectorTokens, &corev1.Secret{}, &CollectorTokenSecretUpdater{token: glindeinToken})
	err2 := applyUpdateToResource(pr, RNTokens, &corev1.Secret{}, &CollectorTokenSecretUpdater{token: pilotToken})
	return errors.Join(err, err2)
}

// Create the set of resources associated with a single Glidein deployment
// - Secret containing access tokens from the local Collector
// - Secret containing data files from the upstream Git repo
// - Secret containing external Collector access tokens provided by the Glidein Manager
// - Empty deployment with volume mounts for the above secrets
func createResourcesForGlideinSet(r *GlideinSetReconciler, ctx context.Context, glideinSet *gmosv1alpha1.GlideinSet) error {
	log := log.FromContext(ctx)
	log.Info("Got new value for GlideinSet custom resource!")
	psState := &PilotSetReconcileState{reconciler: r, ctx: ctx, resource: glideinSet}

	log.Info("Creating Collector tokens secret if not exists")
	err := createResourceIfNotExists(psState, RNCollectorTokens, &corev1.Secret{}, &EmptySecretCreator{})
	if err != nil {
		return err
	}

	log.Info("Creating Data Secret if not exists")
	err = createResourceIfNotExists(psState, RNData, &corev1.Secret{}, &EmptySecretCreator{})
	if err != nil {
		return err
	}

	log.Info("Creating Access Token Secret if not exists")
	err = createResourceIfNotExists(psState, RNTokens, &corev1.Secret{}, &EmptySecretCreator{})
	if err != nil {
		return err
	}

	log.Info("Creating Deployment if not exists")
	err = createResourceIfNotExists(psState, RNBase, &appsv1.Deployment{}, &PilotSetDeploymentCreator{})
	if err != nil {
		return err
	}

	return nil
}

//go:embed manifests/glideinset/glideinset-deployment.yaml
var glideinsetDeployment string

// Update the set of resources associated with a GlideinSet based on changes to its RemoteManifest
// field supplied from the upstream Git repo
// - Update the Deployment with the image, environment, and volume mounts supplied from Git
func updateResourcesForGlideinSet(r *GlideinSetReconciler, ctx context.Context, glideinSet *gmosv1alpha1.GlideinSet) error {
	log := log.FromContext(ctx)
	glState := &PilotSetReconcileState{reconciler: r, ctx: ctx, resource: glideinSet}

	log.Info("Updating Deployment with changes to CR")
	// err := applyUpdateToResource(glState, "", &appsv1.Deployment{}, &DeploymentPilotSetUpdater{glideinSet: glideinSet})
	// if err != nil {
	// 	log.Error(err, "Unable to update Deployment for GlideinSet")
	// 	return err
	// }

	// if glideinSet.RemoteManifest == nil {
	// 	log.Info("RemoteManifest is unset for deployment.")
	// 	return nil
	// }

	// log.Info("Updating Deployment with changes to RemoteManifest")
	// updater := &DeploymentGitUpdater{manifest: glideinSet.RemoteManifest, collectorUrl: glideinSet.Spec.LocalCollectorUrl}
	// err = applyUpdateToResource(glState, RNBase, &appsv1.Deployment{}, updater)
	// if !updateErrOk(err) {
	// 	log.Error(err, "Unable to update Deployment from RemoteManifest for commit "+glideinSet.RemoteManifest.CurrentCommit)
	// 	return err
	// }

	genericEditor := &TemplatedResourceEditor{templateData: glideinSet, templateYaml: glideinsetDeployment}
	val, err := genericEditor.getInitialResourceValue()
	if err != nil {
		return err
	}

	if err := applyUpdateToResource(glState, RNBase, val, genericEditor); apierrors.IsNotFound(err) {
		if err := createResourceIfNotExists(glState, RNBase, val, genericEditor); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	if glideinSet.RemoteManifest == nil {
		log.Info("RemoteManifest is unset for deployment.")
		return nil
	}

	log.Info("Updating Data Secret with changes to RemoteManifest")
	err = applyUpdateToResource(glState, RNData, &corev1.Secret{}, &DataSecretGitUpdater{manifest: glideinSet.RemoteManifest})
	if !updateErrOk(err) {
		log.Error(err, "Unable to update Secret from RemoteManifest for commit "+glideinSet.RemoteManifest.CurrentCommit)
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
