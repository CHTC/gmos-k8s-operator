package controller

import (
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Generic interface for a struct that contains a method which updates the structure of a
// Kubernetes Resource
type ResourceUpdater[T client.Object] interface {
	updateResourceValue(Reconciler, T) (bool, error)
}

// Generic interface for a struct that creates a Kubernetes resource that
// doesn't yet exist
type ResourceCreator[T client.Object] interface {
	setResourceValue(Reconciler, metav1.Object, T) error
}

// Create a new Kubernetes object:
// 1. Check that a resource with the given name doesn't yet exist in the namespace
// 2. Create an initial schema for the object in-memory
// 3. Post the newly created object to k8s via the API
func createResourceIfNotExists[T client.Object](
	reconcileState *PilotSetReconcileState,
	resourceName ResourceName,
	resource T,
	creator ResourceCreator[T],
) error {
	log := log.FromContext(reconcileState.ctx)
	name := resourceName.nameFor(reconcileState.resource)
	err := reconcileState.reconciler.getClient().Get(
		reconcileState.ctx,
		types.NamespacedName{Name: name, Namespace: reconcileState.resource.GetNamespace()},
		resource)
	if err == nil {
		log.Info("Resource already exists, no action needed.")
	} else if apierrors.IsNotFound(err) {
		log.Info("Resource not found, creating it.")
		resource.SetName(name)
		resource.SetNamespace(reconcileState.resource.GetNamespace())
		err := creator.setResourceValue(reconcileState.reconciler, reconcileState.resource, resource)
		if err != nil {
			log.Error(err, "Unable to set value for new resource")
		}
		err = ctrl.SetControllerReference(reconcileState.resource, resource, reconcileState.reconciler.getScheme())
		if err != nil {
			return err
		}
		err = reconcileState.reconciler.getClient().Create(reconcileState.ctx, resource)
		if err != nil {
			log.Error(err, "Unable to create resource")
			return err
		}
		return nil
	} else {
		log.Error(err, "Unable to get resource")
		return err
	}
	return nil
}

// Update an existing Kubernetes object:
// 1. Fetch the object by name via the k8s API
// 2. Modify the object's data in-memory
// 3. Push the updated data back to k8s via the API
func applyUpdateToResource[T client.Object](
	reconcileState *PilotSetReconcileState,
	resourceName ResourceName, resource T,
	resourceUpdater ResourceUpdater[T],
) error {
	log := log.FromContext(reconcileState.ctx)
	name := resourceName.nameFor(reconcileState.resource)
	if resource.GetName() != "" {
		name = resource.GetName()
	}
	log.Info("Applying updates to resource " + name)
	err := reconcileState.reconciler.getClient().Get(
		reconcileState.ctx,
		types.NamespacedName{Name: name, Namespace: reconcileState.resource.GetNamespace()},
		resource,
	)
	if err == nil {
		updated, err := resourceUpdater.updateResourceValue(reconcileState.reconciler, resource)
		if err != nil {
			log.Error(err, "Unable to apply update to resource value: "+name)
			return err
		}
		if !updated {
			log.Info("No updates needed for resource " + name)
			return nil
		}
		err = reconcileState.reconciler.getClient().Update(reconcileState.ctx, resource)
		if err != nil {
			log.Error(err, "Unable to post update to resource "+name)
			return err
		}
		log.Info("Resource updated successfully: " + name)
	} else if apierrors.IsNotFound(err) {
		log.Info("Resource not found, must have been deleted or not created")
		return err
	} else {
		log.Error(err, "Unable to get resource")
		return err
	}
	return nil
}

func getResourceValue[T client.Object](reconcileState *PilotSetReconcileState, resourceName ResourceName, resource T) error {
	log := log.FromContext(reconcileState.ctx)
	name := resourceName.nameFor(reconcileState.resource)
	err := reconcileState.reconciler.getClient().Get(
		reconcileState.ctx,
		types.NamespacedName{Name: name, Namespace: reconcileState.resource.GetNamespace()},
		resource,
	)
	if err != nil {
		log.Error(err, fmt.Sprintf("Unable to retrieve Git sync state from GlideinSet %v", name))
		return err
	}
	return nil
}

// Create a new Kubernetes object:
// 1. Check that a resource with the given name doesn't yet exist in the namespace
// 2. Create an initial schema for the object in-memory
// 3. Post the newly created object to k8s via the API
func createRuntimeObjectIfNotExists(
	reconcileState *PilotSetReconcileState,
	resourceName ResourceName,
	resource client.Object,
	creator ResourceCreator[client.Object],
) error {
	log := log.FromContext(reconcileState.ctx)
	name := resourceName.nameFor(reconcileState.resource)
	err := reconcileState.reconciler.getClient().Get(
		reconcileState.ctx,
		types.NamespacedName{Name: name, Namespace: reconcileState.resource.GetNamespace()},
		resource)
	if err == nil {
		log.Info("Resource already exists, no action needed.")
	} else if apierrors.IsNotFound(err) {
		log.Info("Resource not found, creating it.")
		resource.SetName(name)
		resource.SetNamespace(reconcileState.resource.GetNamespace())
		err := creator.setResourceValue(reconcileState.reconciler, reconcileState.resource, resource)
		if err != nil {
			log.Error(err, "Unable to set value for new resource")
		}
		err = ctrl.SetControllerReference(reconcileState.resource, resource, reconcileState.reconciler.getScheme())
		if err != nil {
			return err
		}
		err = reconcileState.reconciler.getClient().Create(reconcileState.ctx, resource)
		if err != nil {
			log.Error(err, "Unable to create resource")
			return err
		}
		return nil
	} else {
		log.Error(err, "Unable to get resource")
		return err
	}
	return nil
}
