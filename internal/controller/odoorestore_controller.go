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

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	odoov1alpha1 "cloud.alterway.fr/operator/api/v1alpha1"
)

// OdooRestoreReconciler reconciles a OdooRestore object
type OdooRestoreReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=cloud.alterway.fr,resources=odoorestores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cloud.alterway.fr,resources=odoorestores/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cloud.alterway.fr,resources=odoorestores/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *OdooRestoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the OdooRestore instance
	restore := &odoov1alpha1.OdooRestore{}
	err := r.Get(ctx, req.NamespacedName, restore)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "unable to fetch OdooRestore")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Define the Restore Job
	jobName := restore.Name + "-job"
	job := &batchv1.Job{}
	err = r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: restore.Namespace}, job)

	if err != nil && errors.IsNotFound(err) {
		// Job does not exist, create it
		job = r.jobForRestore(restore)
		log.Info("Creating a new Restore Job", "Job.Namespace", job.Namespace, "Job.Name", job.Name)
		if err := r.Create(ctx, job); err != nil {
			log.Error(err, "Failed to create Restore Job")
			return ctrl.Result{}, err
		}
		// Update status
		restore.Status.Conditions = append(restore.Status.Conditions, metav1.Condition{
			Type:               "Running",
			Status:             metav1.ConditionTrue,
			Reason:             "JobCreated",
			Message:            "Restore Job has been created",
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Update(ctx, restore); err != nil {
			log.Error(err, "Failed to update OdooRestore status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	} else if err != nil {
		log.Error(err, "Failed to get Restore Job")
		return ctrl.Result{}, err
	}

	// Job exists, check status
	if job.Status.Succeeded > 0 {
		// Job completed
		if len(restore.Status.Conditions) == 0 || restore.Status.Conditions[len(restore.Status.Conditions)-1].Type != "Completed" {
			restore.Status.Conditions = append(restore.Status.Conditions, metav1.Condition{
				Type:               "Completed",
				Status:             metav1.ConditionTrue,
				Reason:             "JobSucceeded",
				Message:            "Restore Job completed successfully",
				LastTransitionTime: metav1.Now(),
			})
			if err := r.Status().Update(ctx, restore); err != nil {
				log.Error(err, "Failed to update OdooRestore status")
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *OdooRestoreReconciler) jobForRestore(restore *odoov1alpha1.OdooRestore) *batchv1.Job {
	ls := map[string]string{"app": "odoo-restore", "restore_cr": restore.Name}

	// Dummy job for now
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      restore.Name + "-job",
			Namespace: restore.Namespace,
			Labels:    ls,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{
						{
							Name:    "restore",
							Image:   "busybox",
							Command: []string{"echo", "Restore started... done"},
						},
					},
				},
			},
		},
	}
	_ = ctrl.SetControllerReference(restore, job, r.Scheme)
	return job
}

// SetupWithManager sets up the controller with the Manager.
func (r *OdooRestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&odoov1alpha1.OdooRestore{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
