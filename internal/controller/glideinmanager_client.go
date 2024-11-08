// Set of data structures for attaching a "Glidein Manager Poller" to a PilotSet
// resource that polls the Glidein Manager at a regular interval and updates
// the PilotSet based on changes
package controller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	gmosClient "github.com/chtc/gmos-client/client"
	gmosv1alpha1 "github.com/chtc/gmos-k8s-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Interface for a struct that handles receiving Git updates from a Glidein Manager
type GlideinManagerUpdateHandler interface {
	// Update the resources in a namespace based on new data in the Glidein Manager's git repository
	ApplyGitUpdate(gmosClient.RepoUpdate) error

	// Update the resources in a namespace based on new data in the Glidein Manager's secret store
	ApplySecretUpdate(PilotSetSecretSource, gmosClient.SecretValue) error
}

// Struct tracking the sync state of a (set of) K8s resources with the Git config
// data stored in the Glidein Manager
type ResourceSyncState struct {
	// Namespace to which the resource belongs
	namespace string

	// Last commit to which the resource was successfully updated
	currentCommit string

	// Last secret version to which the resource was successfully updated
	currentSecretVersion string

	// Struct with functions that apply changes to the Glidein Manager's data
	// to a resource
	updateHandler GlideinManagerUpdateHandler

	// Hold the latest config for the resource from the upstream glidein manager
	currentConfig PilotSetNamespaceConfig
}

// Helper struct that polls a Glidein Manager Git repo on an interval and passes updated config
// data into a GlideinManagerUpdateHandler implementation. Note that multiple resources can
// be configured via Git data hosted on a single Glidein Manager
type GlideinManagerPoller struct {
	syncStates       map[string]*ResourceSyncState
	client           *gmosClient.GlideinManagerClient
	dataUpdateTicker *time.Ticker
	refreshTicker    *time.Ticker
	doneChan         chan (bool)
}

// Create a new GlideinManagerPoler that polls from the given upstream Git repo
func NewGlidenManagerPoller(clientName string, managerUrl string) *GlideinManagerPoller {
	client := &gmosClient.GlideinManagerClient{
		HostName:   clientName,
		ManagerUrl: managerUrl,
		WorkDir:    "/tmp",
	}

	poller := &GlideinManagerPoller{
		syncStates: make(map[string]*ResourceSyncState),
		client:     client,
		doneChan:   make(chan bool),
	}
	return poller
}

// Start polling a Glidein Manager Git repo at the given interval
func (p *GlideinManagerPoller) StartPolling(pollInterval time.Duration, refreshInterval time.Duration) {
	if p.dataUpdateTicker != nil || p.refreshTicker != nil {
		return
	}
	p.dataUpdateTicker = time.NewTicker(pollInterval)
	go func() {
		for {
			select {
			case <-p.doneChan:
				return
			case <-p.dataUpdateTicker.C:
				p.CheckForGitUpdates()
				p.CheckForSecretUpdates()
			}
		}
	}()

	p.refreshTicker = time.NewTicker(refreshInterval)
	go func() {
		for {
			select {
			case <-p.doneChan:
				return
			case <-p.refreshTicker.C:
				if err := p.DoHandshakeWithRetry(15, 5*time.Second); err != nil {
					return
				}
			}
		}
	}()
}

// Stop polling the upstream Git repo once all watchers have been removed
func (p *GlideinManagerPoller) StopPolling() {
	p.dataUpdateTicker.Stop()
	p.refreshTicker.Stop()
	p.doneChan <- true
}

// Check whether a GlideinManagerUpdateHandler has already been registered for the given resource
func (p *GlideinManagerPoller) HasUpdateHandlerForResource(resource string) bool {
	_, exists := p.syncStates[resource]
	return exists
}

// Add a new GlideinManagerUpdateHandler for the given resource
func (p *GlideinManagerPoller) SetUpdateHandler(resource string, namespace string, updateHandler GlideinManagerUpdateHandler) {
	if !p.HasUpdateHandlerForResource(resource) {
		p.syncStates[resource] = &ResourceSyncState{namespace: namespace}
	}
	p.syncStates[resource].updateHandler = updateHandler
}

// Main resource config update loop:
// - Check whether the current sync state of the resource is behind the latest Git commit
// - If so, read the manifest yaml for the resource's namespace from the on-disk copy of the Git repo
// - Then, update the associated Deployment and Secrets based on changes to the manifest
func (p *GlideinManagerPoller) CheckForGitUpdates() {
	log := log.FromContext(context.TODO())
	log.Info(fmt.Sprintf("Checking for git updates from %v", p.client.ManagerUrl))
	repoUpdate, err := p.client.SyncRepo()
	if err != nil {
		log.Error(err, "Unable to check for git update")
		return
	}
	for resource, syncState := range p.syncStates {
		namespace := syncState.namespace
		if syncState.currentCommit == repoUpdate.CurrentCommit {
			continue
		}
		log.Info(fmt.Sprintf("Updating resource %v to commit %v with updater %+v", resource, repoUpdate.CurrentCommit, syncState))

		config, err := readManifestForNamespace(repoUpdate, namespace)
		if err != nil {
			log.Error(err, fmt.Sprintf("Git repo contains invalid manifest for namespace %v", namespace))
			continue
		}
		syncState.currentConfig = config

		if err := syncState.updateHandler.ApplyGitUpdate(repoUpdate); err != nil {
			log.Error(err, fmt.Sprintf("Error occurred while handling repo update for resource %v", resource))
		} else {
			syncState.currentCommit = repoUpdate.CurrentCommit
		}
	}
}

// Main secret resource value update loop:
// - Check whether the latest version of the credential in the Secret is behind the latest upstream Secret version
// - If so, read the new Secret from the upstream
// - Then, update the associated Secret(s) based on the new Secret value
func (p *GlideinManagerPoller) CheckForSecretUpdates() {
	log := log.FromContext(context.TODO())
	log.Info(fmt.Sprintf("Checking for secret updates from %v", p.client.ManagerUrl))
	for _, syncState := range p.syncStates {
		namespace := syncState.namespace
		secretName := syncState.currentConfig.SecretSource.SecretName
		secretDst := syncState.currentConfig.SecretSource.Dst
		// Only check on namespaces with a secret name and config specified
		if secretName == "" || secretDst == "" {
			continue
		}
		nextSecret, err := p.client.GetSecret(syncState.currentConfig.SecretSource.SecretName)
		if err != nil {
			log.Error(err, fmt.Sprintf("Error occurred while fetching secret for namespace %v", namespace))
			continue
		}
		if nextSecret.Version == syncState.currentSecretVersion {
			continue
		}

		log.Info(fmt.Sprintf("Updating namespace %v to secret %v, version %v", namespace, nextSecret.Name, nextSecret.Version))
		if err := syncState.updateHandler.ApplySecretUpdate(syncState.currentConfig.SecretSource, nextSecret); err != nil {
			log.Error(err, fmt.Sprintf("Error occurred while handling secret update for namespace %v", namespace))
		} else {
			syncState.currentSecretVersion = nextSecret.Version
		}
	}
}

// Perform the Auth handshake with the upstream Glidein Manager Git repo,
// implementing custom retry logic (just keep trying it over and over again until it works)
func (p *GlideinManagerPoller) DoHandshakeWithRetry(retries int, delay time.Duration) error {
	log := log.FromContext(context.TODO())
	log.Info("Doing handshake with Glidein Manager Object Server")
	errs := []error{}
	for i := 0; i < retries; i++ {
		if err := p.client.DoHandshake(8071); err != nil {
			log.Error(err, "handshake with GMOS failed")
			errs = append(errs, err)
			time.Sleep(delay)
		} else {
			return nil
		}
	}
	return errors.Join(errs...)
}

// Static map active CollectorClients. Map from URL of upstream Glidein Manager git repo
// to its CollectorClient struct
var activeGlideinManagerPollers = make(map[string]*GlideinManagerPoller)

// Add a Glidein Manager Watcher for the given Gldiein Manager to the given PilotSet's namespace
//
// Should be Idempotent
func AddGlideinManagerWatcher(glideinSet *gmosv1alpha1.GlideinSet, updateHandler GlideinManagerUpdateHandler) error {
	log := log.FromContext(context.TODO())
	log.Info(fmt.Sprintf("Updating Glidein Manager Watcher for namespace %v", glideinSet.Namespace))

	clientName, ok := os.LookupEnv("CLIENT_NAME")
	if !ok {
		return errors.New("env var CLIENT_NAME missing")
	}

	namespacedName := NamespacedNameFor(glideinSet)
	if existingPoller, exists := activeGlideinManagerPollers[glideinSet.Spec.GlideinManagerUrl]; !exists {
		log.Info(fmt.Sprintf("No existing watchers for manager %v. Creating for namespace %v", glideinSet.Spec.GlideinManagerUrl, glideinSet.Namespace))
		poller := NewGlidenManagerPoller(clientName, glideinSet.Spec.GlideinManagerUrl)
		poller.SetUpdateHandler(namespacedName, glideinSet.Namespace, updateHandler)
		activeGlideinManagerPollers[glideinSet.Spec.GlideinManagerUrl] = poller
		go func() {
			if err := poller.DoHandshakeWithRetry(15, 5*time.Second); err != nil {
				log.Error(err, "Unable to complete handshake with GMOS")
				return
			}
			poller.StartPolling(1*time.Minute, 1*time.Hour)
		}()

	} else if !existingPoller.HasUpdateHandlerForResource(namespacedName) {
		// remove the client from other namespaces
		RemoveGlideinManagerWatcher(glideinSet)
		existingPoller.SetUpdateHandler(namespacedName, glideinSet.Namespace, updateHandler)
	}
	return nil
}

// Utility function to mark a resource out of sync with Git whenever it's directly updated
// via changes to its parent CRD
func MarkResourceOutOfSync(namespacedName string) {
	log := log.FromContext(context.TODO())
	log.Info(fmt.Sprintf("Marking resource %v as out-of-sync", namespacedName))

	for _, poller := range activeGlideinManagerPollers {
		if poller.HasUpdateHandlerForResource(namespacedName) {
			poller.syncStates[namespacedName].currentCommit = ""
			poller.syncStates[namespacedName].currentSecretVersion = ""
			break
		}
	}
}

// Remove the Glidein Manager watcher for a single GlideinSet resource. If no watchers are
// remaining for the Git repo after the removal, remove the poller as well.
func RemoveGlideinManagerWatcher(glideinSet *gmosv1alpha1.GlideinSet) {
	log := log.FromContext(context.TODO())

	namespacedName := NamespacedNameFor(glideinSet)
	log.Info(fmt.Sprintf("Removing glidein manager watcher from namespaced name %v", namespacedName))
	var toDelete *GlideinManagerPoller = nil
	for _, poller := range activeGlideinManagerPollers {
		if poller.HasUpdateHandlerForResource(namespacedName) {
			log.Info(fmt.Sprintf("consumer removed for manager %v", poller.client.ManagerUrl))
			delete(poller.syncStates, namespacedName)
			if len(poller.syncStates) == 0 {
				toDelete = poller
			}
			break
		}
	}
	if toDelete != nil {
		log.Info(fmt.Sprintf("Last consumer removed for manager %v. Removing watcher.", toDelete.client.ManagerUrl))
		delete(activeGlideinManagerPollers, toDelete.client.ManagerUrl)
		toDelete.StopPolling()
	}
}
