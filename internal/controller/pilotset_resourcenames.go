package controller

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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

// Util function to prefix a resource name with the name of
// its parent
func (rn ResourceName) nameFor(obj metav1.Object) string {
	return obj.GetName() + string(rn)
}

// Concatinate a resource's namespace to its name for a unique key
func namespacedName(namespace string, name string) string {
	// This is just used for dictionary keys and doesn't need to be a valid k8s object name
	return namespace + "/" + name
}

func namespacedNameFor(obj metav1.Object) string {
	return namespacedName(obj.GetNamespace(), obj.GetName())
}
