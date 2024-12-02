// Set of data structures for attaching a "collector poller" to a PilotSet
// resource that polls the collector at a regular interval and updates
// the PilotSet's credentials secret with new credentials from the Collector

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
)

// Interface for a struct that handles receiving auth tokens from a Collector
type CollectorUpdateHandler interface {
	// Check whether the current set of tokens held by the client have expired
	ShouldUpdateTokens() (bool, error)

	// Update the kubernetes resources managed by the client with a new auth token
	ApplyTokensUpdate(string, string) error
}

// Helper struct that polls a Collector on an interval and passes updated credentials
// into a CollectorUpdateHandler implementation
type CollectorClient struct {
	ctx               context.Context
	resource          metav1.Object
	updateHandler     CollectorUpdateHandler
	tokenUpdateTicker *time.Ticker
	doneChan          chan (bool)
}

// Start polling a collector with the given internval
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

// Stop polling the collector
func (cc *CollectorClient) StopPolling() {
	cc.doneChan <- true
}

// Main credential update loop:
// - Check whether the existing set of credentials have expired
// - Exec into the collector to generate a new set of credentials if needed
// - Update the Secret(s) that store the credentials
func (cc *CollectorClient) HandleTokenUpdates() {
	log := log.FromContext(cc.ctx)
	log.Info(fmt.Sprintf("Checking whether collector tokens are needed in namespace %v", cc.resource.GetNamespace()))
	shouldUpdate, err := cc.updateHandler.ShouldUpdateTokens()
	if err != nil {
		log.Error(err, "Unable to determine whether to update tokens")
		return
	} else if !shouldUpdate {
		log.Info("No need to update tokens")
		return
	}

	glideinToken, err := execInCollector(cc.ctx, cc.resource, []string{
		"condor_token_create", "-identity", "glidein@cluster.local", "-key", "NAMESPACE"})
	pilotToken, err2 := execInCollector(cc.ctx, cc.resource,
		[]string{"condor_token_create", "-identity", "pilot@cluster.local", "-key", "NAMESPACE"})
	if err := errors.Join(err, err2); err != nil {
		log.Error(err, "Unable to generate new tokens for collector")
		return
	}

	if err := cc.updateHandler.ApplyTokensUpdate(glideinToken.Stdout, pilotToken.Stdout); err != nil {
		log.Error(err, "unable to apply token update")
	}

}

func NewCollectorClient(resource metav1.Object, updateHandler CollectorUpdateHandler) CollectorClient {
	return CollectorClient{
		ctx:           context.TODO(),
		resource:      resource,
		updateHandler: updateHandler,
		doneChan:      make(chan bool),
	}
}

// Static map active CollectorClients. Map from namespaced name of client resource
// to its CollectorClient struct
var collectorClients = make(map[string]*CollectorClient)

type ExecOutput struct {
	Stdout string
	Stderr string
}

var ErrPodNotRunning = errors.New("pod not in Running state")

// Utility function to run 'condor_token_create' in the Collector pod
func execInCollector(ctx context.Context, resource metav1.Object, cmd []string) (*ExecOutput, error) {
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
	pods, err := client.CoreV1().Pods(resource.GetNamespace()).List(ctx,
		metav1.ListOptions{
			LabelSelector: "gmos.chtc.wisc.edu/app=collector",
		})
	if err != nil {
		return nil, err
	}
	if len(pods.Items) != 1 {
		return nil, fmt.Errorf("expected 1 collector pod for %v, found %v", resource.GetName(), len(pods.Items))
	}
	pod := pods.Items[0]
	log.Info(fmt.Sprintf("Found pod with name: %+v", pod.Name))

	if pod.Status.Phase != v1.PodRunning {
		return nil, ErrPodNotRunning
	}

	// Exec into the pod to run condor_token_create
	req := client.CoreV1().RESTClient().Post().Namespace(resource.GetNamespace()).
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

// Register a new CollectorUpdateHandler with the Collector associated with the
// given resource
func addCollectorClient(resource metav1.Object, updateHandler CollectorUpdateHandler) error {
	ctx := context.TODO()
	log := log.FromContext(ctx)

	namespacedName := NamespacedNameFor(resource)

	if existingClient, exists := collectorClients[namespacedName]; !exists {
		log.Info(fmt.Sprintf("Creating new collector client for namespaced name %v", namespacedName))
		newClient := NewCollectorClient(resource, updateHandler)
		newClient.StartPolling(1 * time.Minute)
		collectorClients[namespacedName] = &newClient
	} else {
		log.Info(fmt.Sprintf("Collector client already exists for namespaced name %v, updating", namespacedName))
		existingClient.resource = resource
		existingClient.updateHandler = updateHandler
		return nil
	}

	return nil
}

// Remove the watcher associated with the given resource
func RemoveCollectorClient(resource metav1.Object) {
	ctx := context.TODO()
	log := log.FromContext(ctx)

	namespacedName := NamespacedNameFor(resource)

	if existingClient, exists := collectorClients[namespacedName]; exists {
		log.Info(fmt.Sprintf("Removing collector client for namespaced name %v", namespacedName))
		existingClient.StopPolling()
	}
}
