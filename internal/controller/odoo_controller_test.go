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
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	odoov1alpha1 "odoo.alterway.com/operator/api/v1alpha1"
)

var _ = Describe("Odoo Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		odoo := &odoov1alpha1.Odoo{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind Odoo")
			err := k8sClient.Get(ctx, typeNamespacedName, odoo)
			if err != nil && errors.IsNotFound(err) {
				resource := &odoov1alpha1.Odoo{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: odoov1alpha1.OdooSpec{
						Size: 1,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// Cleanup the Odoo resource and its created dependencies
			By("Cleaning up the Odoo resource and its dependencies")
			// The test environment should handle garbage collection, but explicit deletion is safer.
			// Deleting the Odoo CR should trigger garbage collection for owned resources.
			resource := &odoov1alpha1.Odoo{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}

			// Also, ensure the Job is deleted if it's still around
			dbInitJob := &batchv1.Job{}
			jobKey := types.NamespacedName{Name: resourceName + "-db-init", Namespace: "default"}
			err = k8sClient.Get(ctx, jobKey, dbInitJob)
			if err == nil {
				Expect(k8sClient.Delete(ctx, dbInitJob)).To(Succeed())
			}
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &OdooReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// First reconciliation: Create dependencies (PVCs, Services, Job, etc.)
			// Reconcile for Postgres Secret
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// Reconcile for Postgres PVC
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// Reconcile for Postgres Service
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// Reconcile for Postgres StatefulSet
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// Reconcile for Odoo PVCs
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Reconcile for Job
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Check that the PVCs were created
			pvcNames := []string{"data", "custom-addons", "enterprise-addons"}
			for _, pvcName := range pvcNames {
				pvc := &corev1.PersistentVolumeClaim{}
				pvcKey := types.NamespacedName{Name: resourceName + "-" + pvcName + "-pvc", Namespace: "default"}
				Eventually(func() error {
					return k8sClient.Get(ctx, pvcKey, pvc)
				}, "20s", "2s").Should(Succeed(), "should create the "+pvcName+" pvc")
			}

			// Check that the DB init Job was created
			dbInitJob := &batchv1.Job{}
			jobKey := types.NamespacedName{Name: resourceName + "-db-init", Namespace: "default"}
			Eventually(func() error {
				return k8sClient.Get(ctx, jobKey, dbInitJob)
			}, "20s", "2s").Should(Succeed(), "should create the db-init job")

			// Check that the PVCs are mounted in the Job
			expectedVolumeMounts := []string{"odoo-data", "odoo-config", "enterprise-addons", "custom-addons"}
			actualVolumeMounts := []string{}
			for _, vm := range dbInitJob.Spec.Template.Spec.Containers[0].VolumeMounts {
				actualVolumeMounts = append(actualVolumeMounts, vm.Name)
			}
			Expect(actualVolumeMounts).To(ConsistOf(expectedVolumeMounts))

			// Manually update the Job's status to simulate completion
			By("Simulating the completion of the DB init Job")
			dbInitJob.Status.Succeeded = 1
			Expect(k8sClient.Status().Update(ctx, dbInitJob)).To(Succeed())

			// Second reconciliation: Job is complete, now create the StatefulSet
			By("Reconciling again after Job completion")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Check that the Odoo StatefulSet was created
			odooSts := &appsv1.StatefulSet{}
			stsKey := types.NamespacedName{Name: resourceName, Namespace: "default"}
			Eventually(func() error {
				return k8sClient.Get(ctx, stsKey, odooSts)
			}, "20s", "2s").Should(Succeed(), "should create the odoo statefulset after the job is done")

			// Verify that the init container is no longer present
			Expect(odooSts.Spec.Template.Spec.InitContainers).To(HaveLen(1), "should only have one init container for waiting on DB")
			Expect(odooSts.Spec.Template.Spec.InitContainers[0].Name).To(Equal("wait-for-db"))
		})
	})
})
