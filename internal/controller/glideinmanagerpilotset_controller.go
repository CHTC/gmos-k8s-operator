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

	gmosv1alpha1 "github.com/chtc/gmos-k8s-operator/api/v1alpha1"
)

const pilotSetFinalizer = "gmos.chtc.wisc.edu/finalizer"

type Reconciler interface {
	getClient() client.Client
	getScheme() *runtime.Scheme
}

// GlideinSetCollectionReconciler reconciles a GlideinSetCollection object
type GlideinSetCollectionReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *GlideinSetCollectionReconciler) getClient() client.Client {
	return r.Client
}

func (r *GlideinSetCollectionReconciler) getScheme() *runtime.Scheme {
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
// the GlideinSetCollection object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *GlideinSetCollectionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	log := log.FromContext(ctx)
	log.Info("Running reconcile")

	pilotSet := &gmosv1alpha1.GlideinSetCollection{}

	if err = r.Get(ctx, req.NamespacedName, pilotSet); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("GlideinSetCollection resource not found. It is either deleted or not created.")
			return result, nil
		}
		log.Error(err, "Failed to get GlideinSetCollection")
		return
	}

	// Add a finalizer to the pilotSet if it doesn't exist, allowing us to perform cleanup when the
	// pilotSet is deleted
	if !controllerutil.ContainsFinalizer(pilotSet, pilotSetFinalizer) {
		log.Info("Adding finalizer for GlideinSetCollection")
		if !controllerutil.AddFinalizer(pilotSet, pilotSetFinalizer) {
			log.Error(nil, "Failed to add finalizer to GlideinSetCollection")
		}

		if err = r.Update(ctx, pilotSet); err != nil {
			log.Error(err, "Failed to update CRD to add finalizer")
			return
		}
	}

	// Check if the pilotSet is marked for deletion, remove its dependent resources if so
	if pilotSet.GetDeletionTimestamp() != nil {
		if !controllerutil.ContainsFinalizer(pilotSet, pilotSetFinalizer) {
			return
		}
		log.Info("Running finalizer on GlideinSetCollection before deletion")

		finalizePilotSet(pilotSet)

		// Refresh the Custom Resource post-finalization
		if err = r.Get(ctx, req.NamespacedName, pilotSet); err != nil {
			log.Error(err, "Failed to get updated GlideinSetCollection after running finalizer operations")
			return
		}

		// Remove the finalizer and update the resource
		if !controllerutil.RemoveFinalizer(pilotSet, pilotSetFinalizer) {
			log.Error(nil, "Failed to remove finalizer from GlideinSetCollection")
		}
		if err = r.Update(ctx, pilotSet); err != nil {
			log.Error(err, "Failed to update CRD to remove finalizer")
			return
		}
		return
	}

	if err = createResourcesForPilotSet(r, ctx, pilotSet); err != nil {
		return
	}
	return
}

// Remove the Glidein Manager Watcher for the namespace when its custom resource is deleted
func finalizePilotSet(pilotSet *gmosv1alpha1.GlideinSetCollection) {
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
func (pr *PilotSetReconcileState) shouldUpdateTokens() (shouldUpdate bool, err error) {
	glideinSec := corev1.Secret{}
	epSec := corev1.Secret{}
	glideinErr := pr.reconciler.getClient().Get(pr.ctx, types.NamespacedName{Name: RNCollectorTokens.nameFor(pr.resource), Namespace: pr.resource.GetNamespace()}, &glideinSec)
	epErr := pr.reconciler.getClient().Get(pr.ctx, types.NamespacedName{Name: RNTokens.nameFor(pr.resource), Namespace: pr.resource.GetNamespace()}, &epSec)
	if epErr == nil && glideinErr == nil {
		// TODO check token expiration. For now just check whether secret is populated
		for _, sec := range []corev1.Secret{glideinSec, epSec} {
			if _, exists := sec.Data["cluster.tkn"]; !exists {
				return true, nil
			}
		}
		return
	} else if apierrors.IsNotFound(glideinErr) && apierrors.IsNotFound(epErr) {
		// Need to wait for resources to be recreated in next reconcile loop
		return
	} else {
		return false, errors.Join(epErr, glideinErr)
	}
}

// Return whether an error is "recoverable" - either nil or not found
func updateErrOk(err error) bool {
	return err == nil || apierrors.IsNotFound(err)
}

// Create the Collector and associated resources for a PilotSet
// - A Secret containing the signing key for the Collector's tokens
// - A ConfigMap containing config.d files for the Collector
// - A Deployment hosting a single replica of a Collector pod
// - A Service for TCP on port 9618 on the Collecor
func createCollectorForPilotSet(r *GlideinSetCollectionReconciler, ctx context.Context, pilotSet *gmosv1alpha1.GlideinSetCollection) error {
	// Collector resources
	log := log.FromContext(ctx)
	psState := &PilotSetReconcileState{reconciler: r, ctx: ctx, resource: pilotSet}

	log.Info("Creating Collector Signing Key if not exists")
	err := createResourceIfNotExists(psState, RNCollectorSigkey, &corev1.Secret{}, &CollectorSigningKeyCreator{})
	if err != nil {
		return err
	}

	log.Info("Creating Collector ConfigMap if not exists")
	err = createResourceIfNotExists(psState, RNCollectorConfig, &corev1.ConfigMap{}, &CollectorConfigMapCreator{})
	if err != nil {
		return err
	}

	log.Info("Creating Collector Deployment if not exists")
	err = createResourceIfNotExists(psState, RNCollector, &appsv1.Deployment{}, &CollectorDeploymentCreator{})
	if err != nil {
		return err
	}

	log.Info("Creating Collector Service if not exists")
	err = createResourceIfNotExists(psState, RNCollector, &corev1.Service{}, &CollectorServiceCreator{})
	if err != nil {
		return err
	}
	return nil
}

// Create or update the Prometheus Monitoring Server and associated resources for a PilotSet
// - A Secret containing the signing key for the Collector's tokens
// - A ConfigMap containing config.d files for the Collector
// - A Deployment hosting a single replica of a Collector pod
// - A Service for TCP on port 9618 on the Collecor
func createMonitoringForPilotSet(r *GlideinSetCollectionReconciler, ctx context.Context, pilotSet *gmosv1alpha1.GlideinSetCollection) error {
	// Collector resources
	log := log.FromContext(ctx)
	psState := &PilotSetReconcileState{reconciler: r, ctx: ctx, resource: pilotSet}

	// Prometheus monitoring resources
	log.Info("Creating Prometheus PushGateway if not exists")
	if err := createResourceIfNotExists(psState, RNPrometheusPushgateway, &appsv1.Deployment{}, &PrometheusPushgatewayDeploymentCreator{}); err != nil {
		return err
	}

	log.Info("Creating Prometheus PushGateway Service if not exists")
	if err := createResourceIfNotExists(psState, RNPrometheusPushgateway, &corev1.Service{}, &PrometheusPushgatewayServiceCreator{}); err != nil {
		return err
	}

	log.Info("Updating Prometheus ConfigMap if exists, creating otherwise")
	configEditor := &PrometheusConfigMapEditor{pilotSet: pilotSet}
	if err := applyUpdateToResource(psState, RNPrometheus, &corev1.ConfigMap{}, configEditor); apierrors.IsNotFound(err) {
		if err := createResourceIfNotExists(psState, RNPrometheus, &corev1.ConfigMap{}, configEditor); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	log.Info("Updating Prometheus Deployment if exists, creating otherwise")
	deploymentEditor := &PrometheusDeploymentEditor{monitoring: pilotSet.Spec.Prometheus}
	if err := applyUpdateToResource(psState, RNPrometheus, &appsv1.Deployment{}, deploymentEditor); apierrors.IsNotFound(err) {
		if err := createResourceIfNotExists(psState, RNPrometheus, &appsv1.Deployment{}, deploymentEditor); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	log.Info("Creating Prometheus Service if not exists")
	if err := createResourceIfNotExists(psState, RNPrometheus, &corev1.Service{}, &PrometheusServiceCreator{}); err != nil {
		return err
	}

	return nil
}

// Create the base set of resources for a GlideinSetCollection:
// - A Collector Deployment with associated config and service
// - A Prometheus monitoring instance with associated
// - The set of GlideinSets associated with the Collector
func createResourcesForPilotSet(r *GlideinSetCollectionReconciler, ctx context.Context, pilotSet *gmosv1alpha1.GlideinSetCollection) error {
	log := log.FromContext(ctx)
	log.Info("Got new value for GlideinSetCollection custom resource!")
	psState := &PilotSetReconcileState{reconciler: r, ctx: ctx, resource: pilotSet}

	// Collector Resources
	if err := createCollectorForPilotSet(r, ctx, pilotSet); err != nil {
		return err
	}

	// Monitoring Resources
	if err := createMonitoringForPilotSet(r, ctx, pilotSet); err != nil {
		return err
	}

	// GlideinSet resources
	log.Info("Updating GlideinSets")
	if err := recoincileGlideinSets(pilotSet, psState); err != nil {
		return err
	}

	return nil
}

// Reconcile the collection of GlideinSets in the namespace with the `spec.GlideinSets` field
// in the GlideinSetCollection
// - Create any GlideinSets in the spec that don't exist yet
// - Update the Spec of any GlideinSet that already exists and has been changed
// - Delete GlideinSets that are no longer included in the list
func recoincileGlideinSets(pilotSet *gmosv1alpha1.GlideinSetCollection, psState *PilotSetReconcileState) error {
	log := log.FromContext(psState.ctx)
	// Create/update all present GlideinSets
	for _, glideinSetSpec := range pilotSet.Spec.GlideinSets {
		glideinSetSpec.GlideinManagerUrl = pilotSet.Spec.GlideinManagerUrl
		glideinSetSpec.LocalCollectorUrl = fmt.Sprintf("%v.%v.svc.cluster.local", RNCollector.nameFor(pilotSet), pilotSet.GetNamespace())
		gsResource := ResourceName("-" + glideinSetSpec.Name)
		// TODO this double dips API calls by checking for existence on update then checking again on create
		creator := &GlideinSetCreator{spec: &glideinSetSpec}
		err := applyUpdateToResource(psState, gsResource, &gmosv1alpha1.GlideinSet{}, creator)
		if apierrors.IsNotFound(err) {
			err = createResourceIfNotExists(psState, gsResource, &gmosv1alpha1.GlideinSet{}, creator)
			if err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}

	// Remove stale GlideinSets
	glideinSetList := gmosv1alpha1.GlideinSetList{}
	psState.reconciler.getClient().List(
		psState.ctx,
		&glideinSetList,
		client.InNamespace(pilotSet.Namespace),
		client.HasLabels([]string{"app.kubernetes.io/instance=" + pilotSet.Name}),
	)

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
			err := psState.reconciler.getClient().Delete(psState.ctx, &glideinSet)
			if err != nil {
				log.Error(err, "Unable to delete stale GlideinSet")
				return err
			}
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GlideinSetCollectionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gmosv1alpha1.GlideinSetCollection{}).
		Complete(r)
}
