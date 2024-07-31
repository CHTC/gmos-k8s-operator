package controller

import (
	"context"
	"fmt"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	restclient "k8s.io/client-go/kubernetes"
	clientConfig "sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gmosv1alpha1 "github.com/chtc/gmos-k8s-operator/api/v1alpha1"
)

func ExecInCollector(ctx context.Context, pilotSet *gmosv1alpha1.GlideinManagerPilotSet) error {
	log := log.FromContext(ctx)
	cfg, err := clientConfig.GetConfig()
	if err != nil {
		log.Error(err, "Failed to get client config")
		return err
	}
	log.Info(fmt.Sprintf("Client Config: %+v", cfg))

	restClient, err := restclient.NewForConfig(cfg)
	if err != nil {
		log.Error(err, "Unable to construct new REST client")
		return err
	}

	pods, err := restClient.CoreV1().Pods(pilotSet.Namespace).List(ctx,
		v1.ListOptions{
			LabelSelector: "gmos.chtc.wisc.edu/app=collector",
		})
	if err != nil {
		log.Error(err, "Unable to list pods")
		return err
	}
	for _, p := range pods.Items {
		log.Info(fmt.Sprintf("Found pod with name: %+v", p))
	}
	return nil
}
