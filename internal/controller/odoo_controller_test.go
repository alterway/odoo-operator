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
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	odoov1alpha1 "cloud.alterway.fr/operator/api/v1alpha1"
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
			pvcNames := []string{"data", "addons"}
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
			expectedVolumeMounts := []string{"odoo-data", "odoo-config", "odoo-addons-all", "odoo-logs"}
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

	Context("When specifying resource requirements", func() {
		const resourceName = "test-resource-limits"
		ctx := context.Background()
		typeNamespacedName := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			By("creating the custom resource with resource limits")
			odoo := &odoov1alpha1.Odoo{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: odoov1alpha1.OdooSpec{
					Size: 1,
					Resources: odoov1alpha1.OdooResources{
						Odoo: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
							Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("500Mi")},
						},
						Postgres: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m")},
						},
						Init: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("200Mi")},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, odoo)).To(Succeed())
		})

		AfterEach(func() {
			resource := &odoov1alpha1.Odoo{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			// Clean up jobs if they exist
			job := &batchv1.Job{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-db-init", Namespace: "default"}, job)
			if err == nil {
				k8sClient.Delete(ctx, job)
			}
		})

		It("should propagate resources to pods", func() {
			controllerReconciler := &OdooReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// Run reconcile loop multiple times to create all resources
			for i := 0; i < 10; i++ {
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
				Expect(err).NotTo(HaveOccurred())
			}

			// Verify Postgres StatefulSet resources
			pgSts := &appsv1.StatefulSet{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-postgres", Namespace: "default"}, pgSts)
			}, "10s", "1s").Should(Succeed())
			Expect(pgSts.Spec.Template.Spec.Containers[0].Resources.Requests.Cpu().String()).To(Equal("200m"))

			// Verify Init Job resources
			initJob := &batchv1.Job{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-db-init", Namespace: "default"}, initJob)
			}, "10s", "1s").Should(Succeed())
			Expect(initJob.Spec.Template.Spec.Containers[0].Resources.Limits.Memory().String()).To(Equal("200Mi"))

			// Simulate Job completion
			initJob.Status.Succeeded = 1
			Expect(k8sClient.Status().Update(ctx, initJob)).To(Succeed())

			// Run reconcile again to create Odoo StatefulSet
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Verify Odoo StatefulSet resources
			odooSts := &appsv1.StatefulSet{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: "default"}, odooSts)
			}, "10s", "1s").Should(Succeed())

			container := odooSts.Spec.Template.Spec.Containers[0]
			Expect(container.Name).To(Equal("odoo"))
			Expect(container.Resources.Requests.Cpu().String()).To(Equal("100m"))
			Expect(container.Resources.Limits.Memory().String()).To(Equal("500Mi"))
		})
	})

	Context("When upgrading the Odoo version", func() {
		const resourceName = "test-upgrade"
		ctx := context.Background()
		typeNamespacedName := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			By("creating the custom resource for upgrade test")
			odoo := &odoov1alpha1.Odoo{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: odoov1alpha1.OdooSpec{
					Size:    1,
					Version: "16.0",
					Upgrade: odoov1alpha1.UpgradeSpec{
						Enabled: true,
					},
				},
			}
			Expect(k8sClient.Create(ctx, odoo)).To(Succeed())
		})

		AfterEach(func() {
			resource := &odoov1alpha1.Odoo{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			// Clean up jobs
			jobList := &batchv1.JobList{}
			k8sClient.List(ctx, jobList, client.InNamespace("default"))
			for _, job := range jobList.Items {
				k8sClient.Delete(ctx, &job)
			}
		})

		It("should trigger an upgrade job", func() {
			controllerReconciler := &OdooReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// 1. Initial Deployment
			// Call Reconcile until the DB Init Job is created
			initJob := &batchv1.Job{}
			Eventually(func() error {
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
				if err != nil {
					return err
				}
				return k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-db-init", Namespace: "default"}, initJob)
			}, "10s", "1s").Should(Succeed(), "DB Init Job should be created")

			// Simulate DB Init Job completion
			initJob.Status.Succeeded = 1
			Expect(k8sClient.Status().Update(ctx, initJob)).To(Succeed())

			// Run reconcile again to update status and set CurrentVersion
			Eventually(func() string {
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
				if err != nil {
					return ""
				}
				updatedOdoo := &odoov1alpha1.Odoo{}
				k8sClient.Get(ctx, typeNamespacedName, updatedOdoo)
				return updatedOdoo.Status.CurrentVersion
			}, "10s", "1s").Should(Equal("16.0"), "CurrentVersion should be set to 16.0")

			// 2. Trigger Upgrade
			updatedOdoo := &odoov1alpha1.Odoo{}
			By("Updating Odoo version to 17.0")
			Eventually(func() error {
				err := k8sClient.Get(ctx, typeNamespacedName, updatedOdoo)
				if err != nil {
					return err
				}
				updatedOdoo.Spec.Version = "17.0"
				return k8sClient.Update(ctx, updatedOdoo)
			}, "10s", "1s").Should(Succeed())

			// 3. Verify Upgrade Job
			upgradeJob := &batchv1.Job{}
			upgradeJobName := resourceName + "-upgrade-17-0"

			// Reconcile until Upgrade Job is created
			Eventually(func() error {
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
				if err != nil {
					return err
				}
				return k8sClient.Get(ctx, types.NamespacedName{Name: upgradeJobName, Namespace: "default"}, upgradeJob)
			}, "10s", "1s").Should(Succeed(), "Upgrade Job should be created")

			// 4. Simulate Upgrade Success
			By("Simulating upgrade job success")
			upgradeJob.Status.Succeeded = 1
			Expect(k8sClient.Status().Update(ctx, upgradeJob)).To(Succeed())

			// 5. Verify Final State
			// Reconcile until CurrentVersion is updated
			Eventually(func() string {
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
				if err != nil {
					return ""
				}
				k8sClient.Get(ctx, typeNamespacedName, updatedOdoo)
				return updatedOdoo.Status.CurrentVersion
			}, "10s", "1s").Should(Equal("17.0"), "CurrentVersion should be updated")

			// Verify StatefulSet image
			sts := &appsv1.StatefulSet{}
			Eventually(func() string {
				k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: "default"}, sts)
				if len(sts.Spec.Template.Spec.Containers) > 0 {
					return sts.Spec.Template.Spec.Containers[0].Image
				}
				return ""
			}, "10s", "1s").Should(Equal("odoo:17.0"), "StatefulSet image should be updated")
		})
	})
})
