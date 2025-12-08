/*
Copyright 2025.

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

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	odoov1alpha1 "cloud.alterway.fr/operator/api/v1alpha1"
)

const conditionCompleted = "Completed"

var _ = Describe("OdooBackup Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-backup"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		backup := &odoov1alpha1.OdooBackup{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind OdooBackup")
			err := k8sClient.Get(ctx, typeNamespacedName, backup)
			// Create if not exists
			if err != nil {
				resource := &odoov1alpha1.OdooBackup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: odoov1alpha1.OdooBackupSpec{
						OdooRef: corev1.ObjectReference{
							Name: "test-odoo",
						},
						StorageLocation: odoov1alpha1.StorageLocationSpec{
							PVC: &odoov1alpha1.PVCStorageSpec{
								ClaimName: "backup-pvc",
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &odoov1alpha1.OdooBackup{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				_ = k8sClient.Delete(ctx, resource)
			}
			// Cleanup job
			job := &batchv1.Job{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-job", Namespace: "default"}, job)
			if err == nil {
				_ = k8sClient.Delete(ctx, job)
			}
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &OdooBackupReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// First reconciliation: Create Job
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Check that the Backup Job was created
			job := &batchv1.Job{}
			jobKey := types.NamespacedName{Name: resourceName + "-job", Namespace: "default"}
			Eventually(func() error {
				return k8sClient.Get(ctx, jobKey, job)
			}, "10s", "1s").Should(Succeed(), "should create the backup job")

			// Simulate Job completion
			By("Simulating the completion of the Backup Job")
			job.Status.Succeeded = 1
			Expect(k8sClient.Status().Update(ctx, job)).To(Succeed())

			// Second reconciliation: Update status
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify status
			updatedBackup := &odoov1alpha1.OdooBackup{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, typeNamespacedName, updatedBackup)
				if err != nil {
					return false
				}
				if len(updatedBackup.Status.Conditions) == 0 {
					return false
				}
				return updatedBackup.Status.Conditions[len(updatedBackup.Status.Conditions)-1].Type == conditionCompleted
			}, "10s", "1s").Should(BeTrue(), "should update status to Completed")

			Expect(updatedBackup.Status.LastBackupName).To(Equal(resourceName))
		})
	})
})
