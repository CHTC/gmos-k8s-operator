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
	onUpdate UpdateCallback
	client   *gmosClient.GlideinManagerClient
	ticker   *time.Ticker
	doneChan chan (bool)
}

func NewGlidenManagerPoller(clientName, managerUrl string, updateCallback UpdateCallback) *GlideinManagerPoller {
	client := &gmosClient.GlideinManagerClient{
		HostName:   clientName,
		ManagerUrl: managerUrl,
		WorkDir:    "/tmp",
	}

	poller := &GlideinManagerPoller{
		onUpdate: updateCallback,
		client:   client,
		doneChan: make(chan bool),
	}
	return poller
}

func (p *GlideinManagerPoller) StartPolling(interval time.Duration) {
	if p.ticker != nil {
		return
	}
	p.ticker = time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-p.doneChan:
				return
			case <-p.ticker.C:
				p.CheckForGitUpdates()
			}
		}
	}()
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
		if err := p.onUpdate(repoUpdate); err != nil {
			log.Error(err, "Error occurred while handling repo update")
		}
	} else {
		log.Info("No updates to repo")
	}

}

func (p *GlideinManagerPoller) StopPolling() {
	p.ticker.Stop()
	p.doneChan <- true
}

func (p *GlideinManagerPoller) DoHandshakeWithRetry(retries int, delay time.Duration) error {
	errs := []error{}
	fmt.Printf("Doing handshake with Glidein Manager Object Server")
	for i := 0; i < retries; i++ {
		if err := p.client.DoHandshake(8071); err != nil {
			fmt.Printf("handshake with GMOS failed: %v", err)
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
	clientName, ok := os.LookupEnv("CLIENT_NAME")
	if !ok {
		return errors.New("env var CLIENT_NAME missing")
	}

	if _, exists := activeGlideinManagerPollers[pilotSet.Namespace]; !exists {
		poller := NewGlidenManagerPoller(clientName, pilotSet.Spec.GlideinManagerUrl, updateCallback)
		activeGlideinManagerPollers[pilotSet.Namespace] = poller
		go func() {
			if err := poller.DoHandshakeWithRetry(15, 5*time.Second); err != nil {
				fmt.Printf("Unable to complete handshake with GMOS")
				return
			}
			poller.StartPolling(1 * time.Minute)
		}()

	}
	return nil
}

func RemoveGlideinManagerWatcher(pilotSet *gmosv1alpha1.GlideinManagerPilotSet) {
	if poller, exists := activeGlideinManagerPollers[pilotSet.Namespace]; exists {
		delete(activeGlideinManagerPollers, pilotSet.Namespace)
		poller.StopPolling()
	}
}
