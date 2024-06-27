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

type GlideinManagerPoller struct {
	updateCallbacks map[string]UpdateCallback
	client          *gmosClient.GlideinManagerClient
	gitPullTicker   *time.Ticker
	refreshTicker   *time.Ticker
	doneChan        chan (bool)
}

func NewGlidenManagerPoller(clientName string, managerUrl string) *GlideinManagerPoller {
	client := &gmosClient.GlideinManagerClient{
		HostName:   clientName,
		ManagerUrl: managerUrl,
		WorkDir:    "/tmp",
	}

	poller := &GlideinManagerPoller{
		updateCallbacks: make(map[string]UpdateCallback),
		client:          client,
		doneChan:        make(chan bool),
	}
	return poller
}

func (p *GlideinManagerPoller) StartPolling(pollInterval time.Duration, refreshInterval time.Duration) {
	if p.gitPullTicker != nil || p.refreshTicker != nil {
		return
	}
	p.gitPullTicker = time.NewTicker(pollInterval)
	go func() {
		for {
			select {
			case <-p.doneChan:
				return
			case <-p.gitPullTicker.C:
				p.CheckForGitUpdates()
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
func (p *GlideinManagerPoller) AddCallback(namespace string, callback UpdateCallback) {
	p.updateCallbacks[namespace] = callback
}

func (p *GlideinManagerPoller) HasCallbackForNamespace(namespace string) bool {
	_, exists := p.updateCallbacks[namespace]
	return exists
}

func (p *GlideinManagerPoller) CheckForGitUpdates() {
	log := log.FromContext(context.TODO())
	log.Info(fmt.Sprintf("Checking for git updates from %v", p.client.ManagerUrl))
	repoUpdate, err := p.client.SyncRepo()
	if err != nil {
		log.Error(err, "Unable to check for git update")
		return
	}
	if repoUpdate.Updated() {
		log.Info(fmt.Sprintf("Updated to commit %v", repoUpdate.CurrentCommit))
		for namespace, callback := range p.updateCallbacks {
			if err := callback(repoUpdate); err != nil {
				log.Error(err, fmt.Sprintf("Error occurred while handling repo update for namespace %v", namespace))
			}
		}
	} else {
		log.Info("No updates to repo")
	}

}

func (p *GlideinManagerPoller) StopPolling() {
	p.gitPullTicker.Stop()
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

func AddGlideinManagerWatcher(pilotSet *gmosv1alpha1.GlideinManagerPilotSet, updateCallback UpdateCallback) error {
	log := log.FromContext(context.TODO())
	log.Info(fmt.Sprintf("Updating Glidein Manager Watcher for namespace %v", pilotSet.Namespace))

	clientName, ok := os.LookupEnv("CLIENT_NAME")
	if !ok {
		return errors.New("env var CLIENT_NAME missing")
	}

	if existingPoller, exists := activeGlideinManagerPollers[pilotSet.Spec.GlideinManagerUrl]; !exists {
		log.Info(fmt.Sprintf("No existing watchers for manager %v. Creating for namespace %v", pilotSet.Spec.GlideinManagerUrl, pilotSet.Namespace))
		poller := NewGlidenManagerPoller(clientName, pilotSet.Spec.GlideinManagerUrl)
		poller.AddCallback(pilotSet.Namespace, updateCallback)
		activeGlideinManagerPollers[pilotSet.Spec.GlideinManagerUrl] = poller
		go func() {
			if err := poller.DoHandshakeWithRetry(15, 5*time.Second); err != nil {
				log.Error(err, "Unable to complete handshake with GMOS")
				return
			}
			poller.StartPolling(1*time.Minute, 1*time.Hour)
		}()

	} else if !existingPoller.HasCallbackForNamespace(pilotSet.Namespace) {
		// remove the client from other namespaces
		RemoveGlideinManagerWatcher(pilotSet)
		existingPoller.AddCallback(pilotSet.Namespace, updateCallback)
	}
	return nil
}

func RemoveGlideinManagerWatcher(pilotSet *gmosv1alpha1.GlideinManagerPilotSet) {
	log := log.FromContext(context.TODO())
	log.Info(fmt.Sprintf("Removing glidein manager watcher from namespace %v", pilotSet.Namespace))
	var toDelete *GlideinManagerPoller = nil
	for _, poller := range activeGlideinManagerPollers {
		if poller.HasCallbackForNamespace(pilotSet.Namespace) {
			log.Info(fmt.Sprintf("consumer removed for manager %v", poller.client.ManagerUrl))
			delete(poller.updateCallbacks, pilotSet.Namespace)
			if len(poller.updateCallbacks) == 0 {
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
