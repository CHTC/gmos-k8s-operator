package controller

import (
	"context"
	"encoding/base64"
	"os"
	"path"

	"github.com/chtc/gmos-client/client"
	gmosv1alpha1 "github.com/chtc/gmos-k8s-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const sampleManifest = `
version: "1"
manifests:
- namespace: default
  image: "ubuntu:24.04"
  command: [ "cat", "/proc/meminfo" ]
  security:
    user: 3030
    group: 3030
  env:
  - name: FOO
    value: BAR
  volume:
    src: ./dev
    dst: /mnt/glidein-data
  secretSource:
    secretName: sample-token.tkn
    dst: /etc/sample-tokens/sample-token.tkn
`
const sampleConfig = `
- name: bob
  role: baker	
`

func makeTestSourceData(tempDir string) error {
	manifestPth := path.Join(tempDir, "glidein-manifest.yaml")
	if err := os.WriteFile(manifestPth, []byte(sampleManifest), 0644); err != nil {
		return err
	}

	configPth := path.Join(tempDir, "dev", "sample.cfg")
	configParent, _ := path.Split(configPth)
	if err := os.MkdirAll(configParent, 0755); err != nil {
		return err
	}

	if err := os.WriteFile(configPth, []byte(sampleConfig), 0644); err != nil {
		return err
	}

	return nil
}

var _ = Describe("Glidein Manager Watcher Test", Ordered, func() {
	Context("When registering a GlideinSet as a Glidein Manager Watcher", func() {
		const resourceName = "test-watcher-glideinset"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		glideinset := &gmosv1alpha1.GlideinSet{}
		const glideinManagerUrl = "localhost:10010"
		var tempDir string

		getMyUpdateHandler := func() GlideinManagerUpdateHandler {
			resource := gmosv1alpha1.GlideinSet{}
			err := k8sClient.Get(ctx, typeNamespacedName, &resource)
			Expect(err).NotTo(HaveOccurred())
			Expect(activeGlideinManagerPollers).Should(HaveKey(glideinManagerUrl))
			myPoller := activeGlideinManagerPollers[glideinManagerUrl]
			Expect(myPoller.hasUpdateHandlerForResource(namespacedNameFor(&resource))).Should(BeTrue())

			handlerKey := namespacedNameFor(&resource)
			return myPoller.updateHandlers[handlerKey]
		}

		BeforeAll(func() {
			By("creating the custom resource for the Kind GlideinSetCollection with no GlideinSets")
			os.Setenv("CLIENT_NAME", "localhost:8080")
			tempDir = GinkgoT().TempDir()
			makeTestSourceData(tempDir)

			err := k8sClient.Get(ctx, typeNamespacedName, glideinset)
			if err != nil && errors.IsNotFound(err) {
				resource := &gmosv1alpha1.GlideinSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
						Labels:    labelsForPilotSet(resourceName),
					},
					Spec: gmosv1alpha1.GlideinSetSpec{
						Name:              resourceName,
						Size:              5,
						GlideinManagerUrl: glideinManagerUrl,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterAll(func() {
			// Cleanup logic after each test, like removing the resource instance.
			os.Unsetenv("CLIENT_NAME")
			resource := &gmosv1alpha1.GlideinSet{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance GlideinSetCollection")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &GlideinSetReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should update the custom resource based on the manifest file", func() {
			// TODO: Scaffolding a functioning glidein manager in the test framework is quite
			// difficult. For starters, just check that a client is registered.
			handler := getMyUpdateHandler()
			repoUpdate := client.RepoUpdate{
				PreviousCommit: "", // assume no previous commit
				CurrentCommit:  "1",
				Path:           tempDir,
			}

			By("Retrieving the current applied commit version")
			currentUpdate, err := handler.getGitSyncState()
			Expect(err).NotTo(HaveOccurred())
			Expect(currentUpdate).To(BeNil())

			By("Applying the git update")
			err = handler.applyGitUpdate(repoUpdate)
			Expect(err).NotTo(HaveOccurred())

			By("Confirming that the the git update applied")
			currentUpdate, err = handler.getGitSyncState()
			Expect(err).NotTo(HaveOccurred())

			// Fields from the "git" update
			Expect(currentUpdate.CurrentCommit).To(Equal(repoUpdate.CurrentCommit))
			Expect(currentUpdate.RepoPath).To(Equal(repoUpdate.Path))

			// Fields from the manifest file
			// Probably should explicitly specify expected values rather than relying on manifest file
			manifest, err := readManifestForNamespace(repoUpdate, "default", "")
			Expect(err).NotTo(HaveOccurred())

			Expect(currentUpdate.Image).To(Equal(manifest.Image))
			Expect(currentUpdate.Command).To(Equal(manifest.Command))
			Expect(currentUpdate.Env).To(Equal(manifest.Env))
			Expect(currentUpdate.Security).To(Equal(manifest.Security))
			Expect(currentUpdate.Volume).To(Equal(manifest.Volume))
			Expect(currentUpdate.SecretSource).To(Equal(manifest.SecretSource))

		})

		It("Should update a secret with the value of a token specified by the manifest file", func() {
			handler := getMyUpdateHandler()

			secVal := "hello, world!"
			secretUpdate := client.SecretValue{
				Name:    "sample-token.tkn",
				Version: "1",
				Value:   base64.StdEncoding.EncodeToString([]byte(secVal)),
			}
			secretSource := gmosv1alpha1.PilotSetSecretSource{
				SecretName: "sample-token.tkn",
				Dst:        "/etc/sample-tokens/sample-token.tkn",
			}

			By("Retrieving the current applied secret version")
			ver, err := handler.getSecretSyncState()
			Expect(err).NotTo(HaveOccurred())
			Expect(ver).To(Equal(""))

			By("Updating the secret with a new version")
			err = handler.applySecretUpdate(secretSource, secretUpdate)
			Expect(err).NotTo(HaveOccurred())

			ver, err = handler.getSecretSyncState()
			Expect(err).NotTo(HaveOccurred())
			Expect(ver).To(Equal(secretUpdate.Version))

			By("Checking that the secret was updated")
			secName := types.NamespacedName{
				Name:      resourceName + string(RNTokens),
				Namespace: "default",
			}
			secret := corev1.Secret{}
			err = k8sClient.Get(ctx, secName, &secret)
			Expect(err).NotTo(HaveOccurred())
			Expect(secret.Data).Should(HaveKey(secretSource.SecretName))
			Expect(secret.Data[secretSource.SecretName]).To(Equal([]byte(secVal)))
		})

		It("Should update a secret with the value of a config file specified by the manifest file", func() {
			handler := getMyUpdateHandler()
			repoUpdate := client.RepoUpdate{
				PreviousCommit: "", // assume no previous commit
				CurrentCommit:  "1",
				Path:           tempDir,
			}

			By("Applying the git update")
			err := handler.applyGitUpdate(repoUpdate)
			Expect(err).NotTo(HaveOccurred())

			By("Re-reconciling the resource")
			controllerReconciler := &GlideinSetReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			secName := types.NamespacedName{
				Name:      resourceName + string(RNData),
				Namespace: "default",
			}
			secret := corev1.Secret{}
			err = k8sClient.Get(ctx, secName, &secret)
			Expect(secret.Data).Should(HaveKey("sample.cfg"))
			Expect(secret.Data["sample.cfg"]).To(Equal([]byte(sampleConfig)))
		})
	})
})
