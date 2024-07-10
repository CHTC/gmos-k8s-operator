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

type UpdateCallback func(gmosClient.RepoUpdate) error
type SecretUpdateCallback func(gmosClient.SecretValue) error

type NamespaceUpdateHandler struct {
	// Last commit to which the namespace was successfully updated
	currentCommit string
	// Callback that runs when a new commit is pulled from the Glidein Manager
	gitUpdateCallback UpdateCallback
	// Secret from the Glidein Manager that should be used in the namespace
	secretName string
	// Last secret version to which the namespace was successfully updated
	currentSecretVersion string
	// Callback that runs when a new secret value is pulled from the Glidein Manager
	secretUpdateCallback SecretUpdateCallback
}

type GlideinManagerPoller struct {
	updateHandlers   map[string]*NamespaceUpdateHandler
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
		updateHandlers: make(map[string]*NamespaceUpdateHandler),
		client:         client,
		doneChan:       make(chan bool),
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
	_, exists := p.updateHandlers[namespace]
	return exists
}

func (p *GlideinManagerPoller) AddGitCallback(namespace string, callback UpdateCallback) {
	if !p.HasUpdateHandlerForNamespace(namespace) {
		p.updateHandlers[namespace] = &NamespaceUpdateHandler{}
	}
	p.updateHandlers[namespace].gitUpdateCallback = callback
}

func (p *GlideinManagerPoller) AddSecretCallback(namespace string, callback SecretUpdateCallback) {
	if !p.HasUpdateHandlerForNamespace(namespace) {
		p.updateHandlers[namespace] = &NamespaceUpdateHandler{}
	}
	p.updateHandlers[namespace].secretUpdateCallback = callback
}

func (p *GlideinManagerPoller) CheckForGitUpdates() {
	log := log.FromContext(context.TODO())
	log.Info(fmt.Sprintf("Checking for git updates from %v", p.client.ManagerUrl))
	repoUpdate, err := p.client.SyncRepo()
	if err != nil {
		log.Error(err, "Unable to check for git update")
		return
	}
	for namespace, updater := range p.updateHandlers {
		if updater.currentCommit == repoUpdate.CurrentCommit {
			continue
		}
		log.Info(fmt.Sprintf("Updating namespace %v to commit %v with updater %+v", namespace, repoUpdate.CurrentCommit, updater))
		if err := updater.gitUpdateCallback(repoUpdate); err != nil {
			log.Error(err, fmt.Sprintf("Error occurred while handling repo update for namespace %v", namespace))
		} else {
			updater.currentCommit = repoUpdate.CurrentCommit
		}
	}
}

func (p *GlideinManagerPoller) CheckForSecretUpdates() {
	log := log.FromContext(context.TODO())
	log.Info(fmt.Sprintf("Checking for secret updates from %v", p.client.ManagerUrl))
	for namespace, updater := range p.updateHandlers {
		// Only check on namespaces with a secret name specified
		if updater.secretName == "" {
			continue
		}
		nextSecret, err := p.client.GetSecret(updater.secretName)
		if err != nil {
			log.Error(err, fmt.Sprintf("Error occurred while fetching secret for namespace %v", namespace))
			continue
		}
		if nextSecret.Version == updater.currentSecretVersion {
			continue
		}

		log.Info(fmt.Sprintf("Updating namespace %v to secret %v, version %v", namespace, nextSecret.Name, nextSecret.Version))
		if err := updater.secretUpdateCallback(nextSecret); err != nil {
			log.Error(err, fmt.Sprintf("Error occurred while handling secret update for namespace %v", namespace))
		} else {
			updater.currentSecretVersion = nextSecret.Version
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

func AddGlideinManagerWatcher(pilotSet *gmosv1alpha1.GlideinManagerPilotSet, gitUpdateCallback UpdateCallback, secretUpdateCallback SecretUpdateCallback) error {
	log := log.FromContext(context.TODO())
	log.Info(fmt.Sprintf("Updating Glidein Manager Watcher for namespace %v", pilotSet.Namespace))

	clientName, ok := os.LookupEnv("CLIENT_NAME")
	if !ok {
		return errors.New("env var CLIENT_NAME missing")
	}

	if existingPoller, exists := activeGlideinManagerPollers[pilotSet.Spec.GlideinManagerUrl]; !exists {
		log.Info(fmt.Sprintf("No existing watchers for manager %v. Creating for namespace %v", pilotSet.Spec.GlideinManagerUrl, pilotSet.Namespace))
		poller := NewGlidenManagerPoller(clientName, pilotSet.Spec.GlideinManagerUrl)
		poller.AddGitCallback(pilotSet.Namespace, gitUpdateCallback)
		poller.AddSecretCallback(pilotSet.Namespace, secretUpdateCallback)
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
		existingPoller.AddGitCallback(pilotSet.Namespace, gitUpdateCallback)
		existingPoller.AddSecretCallback(pilotSet.Namespace, secretUpdateCallback)
	}
	return nil
}

func SetSecretSourceForNamespace(namespace string, secretName string) {
	log := log.FromContext(context.TODO())
	log.Info(fmt.Sprintf("Setting secret source to %v for namespace %v", secretName, namespace))

	for _, poller := range activeGlideinManagerPollers {
		if poller.HasUpdateHandlerForNamespace(namespace) {
			updater := poller.updateHandlers[namespace]
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
			delete(poller.updateHandlers, pilotSet.Namespace)
			if len(poller.updateHandlers) == 0 {
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
