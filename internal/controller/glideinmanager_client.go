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

type UpdateHandler interface {
	ApplyGitUpdate(gmosClient.RepoUpdate) error
	ApplySecretUpdate(gmosClient.SecretValue) error
}

type NamespaceSyncState struct {
	// Last commit to which the namespace was successfully updated
	currentCommit string

	// Secret from the Glidein Manager that should be used in the namespace
	secretName string

	// Last secret version to which the namespace was successfully updated
	currentSecretVersion string

	// Struct with functions that apply changes to the Glidein Manager's data
	// to a namespace
	updateHandler UpdateHandler
}

type GlideinManagerPoller struct {
	syncStates       map[string]*NamespaceSyncState
	client           *gmosClient.GlideinManagerClient
	dataUpdateTicker *time.Ticker
	refreshTicker    *time.Ticker
	doneChan         chan (bool)
}

func NewGlidenManagerPoller(clientName string, managerUrl string) *GlideinManagerPoller {
	client := &gmosClient.GlideinManagerClient{
		HostName:   clientName,
		ManagerUrl: managerUrl,
		WorkDir:    "/tmp",
	}

	poller := &GlideinManagerPoller{
		syncStates: make(map[string]*NamespaceSyncState),
		client:     client,
		doneChan:   make(chan bool),
	}
	return poller
}

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

func (p *GlideinManagerPoller) HasUpdateHandlerForNamespace(namespace string) bool {
	_, exists := p.syncStates[namespace]
	return exists
}

func (p *GlideinManagerPoller) SetUpdateHandler(namespace string, updateHandler UpdateHandler) {
	if !p.HasUpdateHandlerForNamespace(namespace) {
		p.syncStates[namespace] = &NamespaceSyncState{}
	}
	p.syncStates[namespace].updateHandler = updateHandler
}

func (p *GlideinManagerPoller) CheckForGitUpdates() {
	log := log.FromContext(context.TODO())
	log.Info(fmt.Sprintf("Checking for git updates from %v", p.client.ManagerUrl))
	repoUpdate, err := p.client.SyncRepo()
	if err != nil {
		log.Error(err, "Unable to check for git update")
		return
	}
	for namespace, syncState := range p.syncStates {
		if syncState.currentCommit == repoUpdate.CurrentCommit {
			continue
		}
		log.Info(fmt.Sprintf("Updating namespace %v to commit %v with updater %+v", namespace, repoUpdate.CurrentCommit, syncState))
		if err := syncState.updateHandler.ApplyGitUpdate(repoUpdate); err != nil {
			log.Error(err, fmt.Sprintf("Error occurred while handling repo update for namespace %v", namespace))
		} else {
			syncState.currentCommit = repoUpdate.CurrentCommit
		}
	}
}

func (p *GlideinManagerPoller) CheckForSecretUpdates() {
	log := log.FromContext(context.TODO())
	log.Info(fmt.Sprintf("Checking for secret updates from %v", p.client.ManagerUrl))
	for namespace, syncState := range p.syncStates {
		// Only check on namespaces with a secret name specified
		if syncState.secretName == "" {
			continue
		}
		nextSecret, err := p.client.GetSecret(syncState.secretName)
		if err != nil {
			log.Error(err, fmt.Sprintf("Error occurred while fetching secret for namespace %v", namespace))
			continue
		}
		if nextSecret.Version == syncState.currentSecretVersion {
			continue
		}

		log.Info(fmt.Sprintf("Updating namespace %v to secret %v, version %v", namespace, nextSecret.Name, nextSecret.Version))
		if err := syncState.updateHandler.ApplySecretUpdate(nextSecret); err != nil {
			log.Error(err, fmt.Sprintf("Error occurred while handling secret update for namespace %v", namespace))
		} else {
			syncState.currentSecretVersion = nextSecret.Version
		}
	}
}

func (p *GlideinManagerPoller) StopPolling() {
	p.dataUpdateTicker.Stop()
	p.refreshTicker.Stop()
	p.doneChan <- true
}

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

var activeGlideinManagerPollers = make(map[string]*GlideinManagerPoller)

// Add a Glidein Manager Watcher for the given Gldiein Manager to the given PilotSet's namespace
//
// Should be Idempotent
func AddGlideinManagerWatcher(pilotSet *gmosv1alpha1.GlideinManagerPilotSet, updateHandler UpdateHandler) error {
	log := log.FromContext(context.TODO())
	log.Info(fmt.Sprintf("Updating Glidein Manager Watcher for namespace %v", pilotSet.Namespace))

	clientName, ok := os.LookupEnv("CLIENT_NAME")
	if !ok {
		return errors.New("env var CLIENT_NAME missing")
	}

	if existingPoller, exists := activeGlideinManagerPollers[pilotSet.Spec.GlideinManagerUrl]; !exists {
		log.Info(fmt.Sprintf("No existing watchers for manager %v. Creating for namespace %v", pilotSet.Spec.GlideinManagerUrl, pilotSet.Namespace))
		poller := NewGlidenManagerPoller(clientName, pilotSet.Spec.GlideinManagerUrl)
		poller.SetUpdateHandler(pilotSet.Namespace, updateHandler)
		activeGlideinManagerPollers[pilotSet.Spec.GlideinManagerUrl] = poller
		go func() {
			if err := poller.DoHandshakeWithRetry(15, 5*time.Second); err != nil {
				log.Error(err, "Unable to complete handshake with GMOS")
				return
			}
			poller.StartPolling(1*time.Minute, 1*time.Hour)
		}()

	} else if !existingPoller.HasUpdateHandlerForNamespace(pilotSet.Namespace) {
		// remove the client from other namespaces
		RemoveGlideinManagerWatcher(pilotSet)
		existingPoller.SetUpdateHandler(pilotSet.Namespace, updateHandler)
	}
	return nil
}

func SetSecretSourceForNamespace(namespace string, secretName string) {
	log := log.FromContext(context.TODO())
	log.Info(fmt.Sprintf("Setting secret source to %v for namespace %v", secretName, namespace))

	for _, poller := range activeGlideinManagerPollers {
		if poller.HasUpdateHandlerForNamespace(namespace) {
			updater := poller.syncStates[namespace]
			updater.secretName = secretName
			break
		}
	}
}

func RemoveGlideinManagerWatcher(pilotSet *gmosv1alpha1.GlideinManagerPilotSet) {
	log := log.FromContext(context.TODO())
	log.Info(fmt.Sprintf("Removing glidein manager watcher from namespace %v", pilotSet.Namespace))
	var toDelete *GlideinManagerPoller = nil
	for _, poller := range activeGlideinManagerPollers {
		if poller.HasUpdateHandlerForNamespace(pilotSet.Namespace) {
			log.Info(fmt.Sprintf("consumer removed for manager %v", poller.client.ManagerUrl))
			delete(poller.syncStates, pilotSet.Namespace)
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
