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

type CollectorUpdateHandler interface {
	ShouldUpdateTokens() (bool, error)
	ApplyTokensUpdate(string, string) error
}

type CollectorClient struct {
	ctx               context.Context
	resource          metav1.Object
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

func AddCollectorClient(resource metav1.Object, updateHandler CollectorUpdateHandler) error {
	ctx := context.TODO()
	log := log.FromContext(ctx)

	namespacedName := resource.GetName() + resource.GetNamespace()

	if existingClient, exists := collectorClients[namespacedName]; !exists {
		log.Info(fmt.Sprintf("Creating new collector client for namespaced name %v", namespacedName))
		newClient := NewCollectorClient(resource, updateHandler)
		newClient.StartPolling(1 * time.Minute)
		collectorClients[resource.GetNamespace()] = &newClient
	} else {
		log.Info(fmt.Sprintf("Collector client already exists for namespaced name %v, updating", namespacedName))
		existingClient.resource = resource
		existingClient.updateHandler = updateHandler
		return nil
	}

	return nil
}

func RemoveCollectorClient(resource metav1.Object) {
	ctx := context.TODO()
	log := log.FromContext(ctx)

	if existingClient, exists := collectorClients[resource.GetNamespace()]; exists {
		log.Info(fmt.Sprintf("Removing collector client for namespace %v", resource.GetNamespace()))
		existingClient.StopPolling()
	}
}
