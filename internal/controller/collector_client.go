package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	restclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	clientConfig "sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gmosv1alpha1 "github.com/chtc/gmos-k8s-operator/api/v1alpha1"
)

type ExecOutput struct {
	Stdout string
	Stderr string
}

var ErrPodNotRunning = errors.New("pod not in Running state")

// Utility function to run 'condor_token_create' in the Collector pod
func ExecInCollector(ctx context.Context, pilotSet *gmosv1alpha1.GlideinManagerPilotSet, cmd []string) (*ExecOutput, error) {
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
		Resource("pods").Name(pod.Name).SubResource("exec").VersionedParams(&corev1.PodExecOptions{
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
