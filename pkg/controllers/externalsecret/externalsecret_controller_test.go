/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package externalsecret

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	esv1alpha1 "github.com/external-secrets/external-secrets/apis/externalsecrets/v1alpha1"
	"github.com/external-secrets/external-secrets/pkg/provider/fake"
	"github.com/external-secrets/external-secrets/pkg/provider/schema"
)

var fakeProvider *fake.Client

var _ = Describe("ExternalSecret controller", func() {
	const (
		ExternalSecretName             = "test-es"
		ExternalSecretStore            = "test-store"
		ExternalSecretTargetSecretName = "test-secret"
		timeout                        = time.Second * 5
		interval                       = time.Millisecond * 250
	)

	var ExternalSecretNamespace string

	BeforeEach(func() {
		var err error
		ExternalSecretNamespace, err = CreateNamespace("test-ns", k8sClient)
		Expect(err).ToNot(HaveOccurred())
		Expect(k8sClient.Create(context.Background(), &esv1alpha1.SecretStore{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ExternalSecretStore,
				Namespace: ExternalSecretNamespace,
			},
			Spec: esv1alpha1.SecretStoreSpec{
				Provider: &esv1alpha1.SecretStoreProvider{
					AWSSM: &esv1alpha1.AWSSMProvider{},
				},
			},
		})).To(Succeed())

	})
	AfterEach(func() {
		Expect(k8sClient.Delete(context.Background(), &v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: ExternalSecretNamespace,
			},
		}, client.PropagationPolicy(metav1.DeletePropagationBackground)), client.GracePeriodSeconds(0)).To(Succeed())
		Expect(k8sClient.Delete(context.Background(), &esv1alpha1.SecretStore{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ExternalSecretStore,
				Namespace: ExternalSecretNamespace,
			},
		}, client.PropagationPolicy(metav1.DeletePropagationBackground)), client.GracePeriodSeconds(0)).To(Succeed())
	})

	Context("When updating ExternalSecret Status", func() {
		It("should set the condition eventually", func() {
			By("creating an ExternalSecret")
			ctx := context.Background()
			es := &esv1alpha1.ExternalSecret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ExternalSecretName,
					Namespace: ExternalSecretNamespace,
				},
				Spec: esv1alpha1.ExternalSecretSpec{
					SecretStoreRef: esv1alpha1.SecretStoreRef{
						Name: ExternalSecretStore,
					},
					Target: esv1alpha1.ExternalSecretTarget{
						Name: ExternalSecretTargetSecretName,
					},
				},
			}
			Expect(k8sClient.Create(ctx, es)).Should(Succeed())
			esLookupKey := types.NamespacedName{Name: ExternalSecretName, Namespace: ExternalSecretNamespace}
			createdES := &esv1alpha1.ExternalSecret{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, esLookupKey, createdES)
				if err != nil {
					return false
				}
				cond := GetExternalSecretCondition(createdES.Status, esv1alpha1.ExternalSecretReady)
				if cond == nil || cond.Status != v1.ConditionTrue {
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue())

		})
	})

	Context("When syncing ExternalSecret value", func() {
		It("should set the secret value", func() {
			By("creating an ExternalSecret")
			ctx := context.Background()
			const targetProp = "targetProperty"
			const secretVal = "someValue"
			es := &esv1alpha1.ExternalSecret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ExternalSecretName,
					Namespace: ExternalSecretNamespace,
				},
				Spec: esv1alpha1.ExternalSecretSpec{
					SecretStoreRef: esv1alpha1.SecretStoreRef{
						Name: ExternalSecretStore,
					},
					Target: esv1alpha1.ExternalSecretTarget{
						Name: ExternalSecretTargetSecretName,
					},
					Data: []esv1alpha1.ExternalSecretData{
						{
							SecretKey: targetProp,
							RemoteRef: esv1alpha1.ExternalSecretDataRemoteRef{
								Key:      "barz",
								Property: "bang",
							},
						},
					},
				},
			}

			fakeProvider.WithGetSecret([]byte(secretVal), nil)
			Expect(k8sClient.Create(ctx, es)).Should(Succeed())
			secretLookupKey := types.NamespacedName{
				Name:      ExternalSecretTargetSecretName,
				Namespace: ExternalSecretNamespace}
			syncedSecret := &v1.Secret{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, secretLookupKey, syncedSecret)
				if err != nil {
					return false
				}
				v := syncedSecret.Data[targetProp]
				return string(v) == secretVal
			}, timeout, interval).Should(BeTrue())

		})
	})
})

// CreateNamespace creates a new namespace in the cluster.
func CreateNamespace(baseName string, c client.Client) (string, error) {
	genName := fmt.Sprintf("ctrl-test-%v", baseName)
	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: genName,
		},
	}
	var err error
	err = wait.Poll(time.Second, 10*time.Second, func() (bool, error) {
		err = c.Create(context.Background(), ns)
		if err != nil {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return "", err
	}
	return ns.Name, nil
}

func init() {
	fakeProvider = fake.New()
	schema.ForceRegister(fakeProvider, &esv1alpha1.SecretStoreProvider{
		AWSSM: &esv1alpha1.AWSSMProvider{},
	})
}
