package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	restclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	clientConfig "sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gmosv1alpha1 "github.com/chtc/gmos-k8s-operator/api/v1alpha1"
)

type CollectorUpdateHandler interface {
	ShouldUpdateTokens() (bool, error)
	ApplyTokensUpdate(string, string) error
}

type CollectorClient struct {
	ctx               context.Context
	pilotSet          *gmosv1alpha1.GlideinManagerPilotSet
	updateHandler     CollectorUpdateHandler
	tokenUpdateTicker *time.Ticker
	doneChan          chan (bool)
}

func (cc *CollectorClient) StartPolling(interval time.Duration) {
	if cc.tokenUpdateTicker != nil {
		return
	}

	cc.tokenUpdateTicker = time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-cc.tokenUpdateTicker.C:
				cc.HandleTokenUpdates()
			case <-cc.doneChan:
				return
			}
		}
	}()
}

func (cc *CollectorClient) StopPolling() {
	cc.doneChan <- true
}

func (cc *CollectorClient) HandleTokenUpdates() {
	log := log.FromContext(cc.ctx)
	log.Info(fmt.Sprintf("Checking whether collector tokens are needed in namespace %v", cc.pilotSet.Namespace))
	shouldUpdate, err := cc.updateHandler.ShouldUpdateTokens()
	if err != nil {
		log.Error(err, "Unable to determine whether to update tokens")
		return
	} else if !shouldUpdate {
		log.Info("No need to update tokens")
		return
	}

	glideinToken, err := execInCollector(cc.ctx, cc.pilotSet, []string{
		"condor_token_create", "-identity", "glidein@cluster.local", "-key", "NAMESPACE"})
	pilotToken, err2 := execInCollector(cc.ctx, cc.pilotSet,
		[]string{"condor_token_create", "-identity", "pilot@cluster.local", "-key", "NAMESPACE"})
	if err := errors.Join(err, err2); err != nil {
		log.Error(err, "Unable to generate new tokens for collector")
		return
	}

	if err := cc.updateHandler.ApplyTokensUpdate(glideinToken.Stdout, pilotToken.Stdout); err != nil {
		log.Error(err, "unable to apply token update")
	}

}

func NewCollectorClient(pilotSet *gmosv1alpha1.GlideinManagerPilotSet, updateHandler CollectorUpdateHandler) CollectorClient {
	return CollectorClient{
		ctx:           context.TODO(),
		pilotSet:      pilotSet,
		updateHandler: updateHandler,
		doneChan:      make(chan bool),
	}
}

var collectorClients = make(map[string]*CollectorClient)

type ExecOutput struct {
	Stdout string
	Stderr string
}

var ErrPodNotRunning = errors.New("pod not in Running state")

// Utility function to run 'condor_token_create' in the Collector pod
func execInCollector(ctx context.Context, pilotSet *gmosv1alpha1.GlideinManagerPilotSet, cmd []string) (*ExecOutput, error) {
	log := log.FromContext(ctx)
	cfg, err := clientConfig.GetConfig()
	if err != nil {
		return nil, err
	}

	client, err := restclient.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	// Find the collector pod for the pilotSet based on label selector
	pods, err := client.CoreV1().Pods(pilotSet.Namespace).List(ctx,
		metav1.ListOptions{
			LabelSelector: fmt.Sprintf("gmos.chtc.wisc.edu/app=collector, app.kubernetes.io/instance=%v", pilotSet.Name),
		})
	if err != nil {
		return nil, err
	}
	if len(pods.Items) != 1 {
		return nil, fmt.Errorf("expected 1 collector pod for %v, found %v", pilotSet.Name, len(pods.Items))
	}
	pod := pods.Items[0]
	log.Info(fmt.Sprintf("Found pod with name: %+v", pod.Name))

	if pod.Status.Phase != v1.PodRunning {
		return nil, ErrPodNotRunning
	}

	// Exec into the pod to run condor_token_create
	req := client.CoreV1().RESTClient().Post().Namespace(pilotSet.Namespace).
		Resource("pods").Name(pod.Name).SubResource("exec").VersionedParams(&v1.PodExecOptions{
		Command: cmd,
		Stdout:  true,
		Stderr:  true,
	}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(cfg, "POST", req.URL())
	if err != nil {
		return nil, err
	}

	outbuf := bytes.Buffer{}
	errbuf := bytes.Buffer{}
	if err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &outbuf,
		Stderr: &errbuf,
	}); err != nil {
		return nil, err
	}

	return &ExecOutput{Stdout: outbuf.String(), Stderr: errbuf.String()}, nil
}

func AddCollectorClient(pilotSet *gmosv1alpha1.GlideinManagerPilotSet, updateHandler CollectorUpdateHandler) error {
	ctx := context.TODO()
	log := log.FromContext(ctx)

	if existingClient, exists := collectorClients[pilotSet.Namespace]; !exists {
		log.Info(fmt.Sprintf("Creating new collector client for namespace %v", pilotSet.Namespace))
		newClient := NewCollectorClient(pilotSet, updateHandler)
		newClient.StartPolling(1 * time.Minute)
		collectorClients[pilotSet.Namespace] = &newClient
	} else {
		log.Info(fmt.Sprintf("Collector client already exists for namespace %v, updating", pilotSet.Namespace))
		existingClient.pilotSet = pilotSet
		existingClient.updateHandler = updateHandler
		return nil
	}

	return nil
}

func RemoveCollectorClient(pilotSet *gmosv1alpha1.GlideinManagerPilotSet) {
	ctx := context.TODO()
	log := log.FromContext(ctx)

	if existingClient, exists := collectorClients[pilotSet.Namespace]; exists {
		log.Info(fmt.Sprintf("Removing collector client for namespace %v", pilotSet.Namespace))
		existingClient.StopPolling()
	}
}
