package controller

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// "Enum" of the named resources created by the operator for a given
// custom resource. All child resources are prefixed by the name
// of the parent custom resource
type ResourceName string

const (
	RNBase            ResourceName = ""
	RNGlideinTokens   ResourceName = "-collector-tokens"
	RNData            ResourceName = "-data"
	RNTokens          ResourceName = "-tokens"
	RNCollector       ResourceName = "-collector"
	RNCollectorSigkey ResourceName = "-collector-sigkey"
	RNCollectorConfig ResourceName = "-collector-cfg"
)

func (rn ResourceName) NameFor(obj metav1.Object) string {
	return obj.GetNamespace() + string(rn)
}
