package controller

import (
	gmosv1alpha1 "github.com/chtc/gmos-k8s-operator/api/v1alpha1"
)

// "Enum" of the named resources created by the operator for a given
// custom resource. All child resources are prefixed by the name
// of the parent custom resource
type ResourceName string

const (
	RNBase            ResourceName = ""
	RNCollectorTokens ResourceName = "-collector-tokens"
	RNData            ResourceName = "-data"
	RNTokens          ResourceName = "-tokens"
	RNCollector       ResourceName = "-collector"
	RNCollectorSigkey ResourceName = "-collector-sigkey"
	RNCollectorConfig ResourceName = "-collector-cfg"
)

func (rn ResourceName) NameFor(pilotSet *gmosv1alpha1.GlideinManagerPilotSet) string {
	return pilotSet.Name + string(rn)
}
