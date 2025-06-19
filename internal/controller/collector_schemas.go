package controller

import (
	"crypto/rand"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Schema entries for the collector
type CollectorTemplate struct {
	metav1.ObjectMeta
	NamespaceToken []byte
}

func makeCollectorTemplate(obj client.Object) (CollectorTemplate, error) {
	keyLen := 256
	b := make([]byte, keyLen)
	if _, err := rand.Read(b); err != nil {
		return CollectorTemplate{}, err
	}
	return CollectorTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      obj.GetName(),
			Namespace: obj.GetNamespace(),
		},
		NamespaceToken: b,
	}, nil
}

// ResourceUpdater implementation that updates a Secret's data key based on the
// values returned by `condor_token_create` run on a collector
type CollectorTokenSecretUpdater struct {
	token string
	PilotSetReconcileState
}

// Set the collector.tkn
func (ct *CollectorTokenSecretUpdater) updateResourceValue(r Reconciler, sec *corev1.Secret) (bool, error) {
	sec.StringData = map[string]string{
		"collector.tkn": ct.token,
	}

	// Since we're not fully recreating the map, ensure that the initial placeholder key pair is removed
	delete(sec.Data, EMPTY_MAP_KEY)

	return true, nil
}
