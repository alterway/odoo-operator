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
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	odoov1alpha1 "cloud.alterway.fr/operator/api/v1alpha1"
)

// OdooReconciler reconciles a Odoo object
type OdooReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=cloud.alterway.fr,resources=odoos,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cloud.alterway.fr,resources=odoos/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cloud.alterway.fr,resources=odoos/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services;persistentvolumeclaims;configmaps;secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Odoo object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.4/pkg/reconcile
func (r *OdooReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the Odoo instance
	odoo := &odoov1alpha1.Odoo{}
	err := r.Get(ctx, req.NamespacedName, odoo)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "unable to fetch Odoo")
			return ctrl.Result{}, err
		}
		// Odoo resource not found, maybe deleted, ignore
		return ctrl.Result{}, nil
	}

	// Examine if the object is being deleted
	isOdooMarkedForDeletion := odoo.GetDeletionTimestamp() != nil
	if isOdooMarkedForDeletion {
		if containsString(odoo.GetFinalizers(), odoov1alpha1.OdooFinalizer) {
			// Our finalizer is present, so we clean up any external dependencies
			log.Info("Performing Finalizer Logic for Odoo resource")

			// Check if Ingress TLS is enabled and delete the associated secret
			if odoo.Spec.Ingress.Enabled && odoo.Spec.Ingress.TLS {
				tlsSecretName := odoo.Name + "-tls"
				secret := &corev1.Secret{}
				err := r.Get(ctx, types.NamespacedName{Name: tlsSecretName, Namespace: odoo.Namespace}, secret)
				if err != nil && !errors.IsNotFound(err) {
					log.Error(err, "Failed to get TLS Secret for deletion")
					return ctrl.Result{}, err
				}

				if err == nil {
					log.Info("Deleting TLS Secret created by cert-manager", "Secret.Namespace", secret.Namespace, "Secret.Name", secret.Name)
					if err := r.Delete(ctx, secret); err != nil {
						log.Error(err, "Failed to delete TLS Secret")
						return ctrl.Result{}, err
					}
				}
			}

			// Remove our finalizer from the list and update it.
			odoo.SetFinalizers(removeString(odoo.GetFinalizers(), odoov1alpha1.OdooFinalizer))
			if err := r.Update(ctx, odoo); err != nil {
				log.Error(err, "Failed to remove finalizer from Odoo resource")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer for this CR
	if !containsString(odoo.GetFinalizers(), odoov1alpha1.OdooFinalizer) {
		log.Info("Adding Finalizer for Odoo resource")
		odoo.SetFinalizers(append(odoo.GetFinalizers(), odoov1alpha1.OdooFinalizer))
		if err := r.Update(ctx, odoo); err != nil {
			log.Error(err, "Failed to add finalizer to Odoo resource")
			return ctrl.Result{}, err
		}
	}

	// --- DATABASE ---
	// Determine database host and secret name
	var dbHost, secretName string
	isExternalDB := odoo.Spec.Database.Host != ""

	if isExternalDB {
		dbHost = odoo.Spec.Database.Host
		secretName = odoo.Spec.DatabaseSecretName
		if secretName == "" {
			// In external mode, a secret is required.
			// We could set a condition here to notify the user.
			log.Error(fmt.Errorf("databaseSecretName must be provided when using an external database"), "validation error")
			// For now, let's stop reconciliation
			return ctrl.Result{}, fmt.Errorf("databaseSecretName must be provided when using an external database")
		}
	} else {
		// Managed database mode
		dbHost = fmt.Sprintf("%s-postgres-svc.%s.svc.cluster.local", odoo.Name, odoo.Namespace)
		secretName = odoo.Spec.DatabaseSecretName
		if secretName == "" {
			secretName = odoo.Name + "-postgres-secret" // Default secret name

			// Ensure the default secret exists
			secret := &corev1.Secret{}
			err = r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: odoo.Namespace}, secret)
			if err != nil && errors.IsNotFound(err) {
				sec := r.secretForPostgres(odoo, secretName)
				log.Info("Creating PostgreSQL Secret", "Secret.Namespace", sec.Namespace, "Secret.Name", sec.Name)
				if err := r.Create(ctx, sec); err != nil {
					log.Error(err, "Failed to create PostgreSQL Secret")
					return ctrl.Result{}, err
				}
				return ctrl.Result{Requeue: true}, nil
			} else if err != nil {
				log.Error(err, "Failed to get PostgreSQL Secret")
				return ctrl.Result{}, err
			}
		}

		// Ensure PostgreSQL PVC exists
		pgPvc := &corev1.PersistentVolumeClaim{}
		err = r.Get(ctx, types.NamespacedName{Name: odoo.Name + "-postgres-pvc", Namespace: odoo.Namespace}, pgPvc)
		if err != nil && errors.IsNotFound(err) {
			pvc := r.pvcForPostgres(odoo)
			log.Info("Creating PostgreSQL PVC", "PVC.Namespace", pvc.Namespace, "PVC.Name", pvc.Name)
			if err := r.Create(ctx, pvc); err != nil {
				log.Error(err, "Failed to create PostgreSQL PVC")
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		} else if err != nil {
			log.Error(err, "Failed to get PostgreSQL PVC")
			return ctrl.Result{}, err
		}

		// Ensure PostgreSQL Service exists
		pgSvc := &corev1.Service{}
		err = r.Get(ctx, types.NamespacedName{Name: odoo.Name + "-postgres-svc", Namespace: odoo.Namespace}, pgSvc)
		if err != nil && errors.IsNotFound(err) {
			svc := r.serviceForPostgres(odoo)
			log.Info("Creating PostgreSQL Service", "Service.Namespace", svc.Namespace, "Service.Name", svc.Name)
			if err := r.Create(ctx, svc); err != nil {
				log.Error(err, "Failed to create PostgreSQL Service")
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		} else if err != nil {
			log.Error(err, "Failed to get PostgreSQL Service")
			return ctrl.Result{}, err
		}

		// Ensure PostgreSQL StatefulSet exists
		pgSts := &appsv1.StatefulSet{}
		err = r.Get(ctx, types.NamespacedName{Name: odoo.Name + "-postgres", Namespace: odoo.Namespace}, pgSts)
		if err != nil && errors.IsNotFound(err) {
			sts := r.statefulSetForPostgres(odoo, secretName)
			log.Info("Creating PostgreSQL StatefulSet", "StatefulSet.Namespace", sts.Namespace, "StatefulSet.Name", sts.Name)
			if err := r.Create(ctx, sts); err != nil {
				log.Error(err, "Failed to create PostgreSQL StatefulSet")
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		} else if err != nil {
			log.Error(err, "Failed to get PostgreSQL StatefulSet")
			return ctrl.Result{}, err
		}
	}

	// Create the PVCs if they don't exist
	pvcNames := []string{"data", "addons"}
	for _, pvcName := range pvcNames {
		pvc := &corev1.PersistentVolumeClaim{}
		err = r.Get(ctx, types.NamespacedName{Name: odoo.Name + "-" + pvcName + "-pvc", Namespace: odoo.Namespace}, pvc)
		if err != nil && errors.IsNotFound(err) {
			pvc := r.pvcForOdoo(odoo, pvcName)
			log.Info("Creating a new PVC", "PVC.Namespace", pvc.Namespace, "PVC.Name", pvc.Name)
			if err := r.Create(ctx, pvc); err != nil {
				log.Error(err, "Failed to create new PVC", "PVC.Namespace", pvc.Namespace, "PVC.Name", pvc.Name)
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		} else if err != nil {
			log.Error(err, "Failed to get PVC")
			return ctrl.Result{}, err
		}
	}

	// --- ADDONS DOWNLOAD & SETUP ---
	var repositoriesToClone []odoov1alpha1.GitRepositorySpec
	var requiredSSHSecrets []string

	// Add Enterprise repo if enabled
	if odoo.Spec.Enterprise.Enabled {
		repoURL := odoo.Spec.Enterprise.RepositoryURL
		if repoURL == "" {
			repoURL = "git@github.com:odoo/enterprise.git"
		}

		repositoriesToClone = append(repositoriesToClone, odoov1alpha1.GitRepositorySpec{
			Name:            "enterprise",
			URL:             repoURL,
			Version:         odoo.Spec.Enterprise.Version,
			SSHKeySecretRef: odoo.Spec.Enterprise.SSHKeySecretRef,
		})
		if odoo.Spec.Enterprise.SSHKeySecretRef != "" && indexOf(requiredSSHSecrets, odoo.Spec.Enterprise.SSHKeySecretRef) == -1 {
			requiredSSHSecrets = append(requiredSSHSecrets, odoo.Spec.Enterprise.SSHKeySecretRef)
		}
	}

	// Add custom repositories
	for _, customRepo := range odoo.Spec.Modules.Repositories {
		repositoriesToClone = append(repositoriesToClone, customRepo)
		if customRepo.SSHKeySecretRef != "" && indexOf(requiredSSHSecrets, customRepo.SSHKeySecretRef) == -1 {
			requiredSSHSecrets = append(requiredSSHSecrets, customRepo.SSHKeySecretRef)
		}
	}

	// If there are repositories to clone, manage the download job
	if len(repositoriesToClone) > 0 {
		// Verify all required SSH secrets exist
		for _, secretNameIter := range requiredSSHSecrets {
			sshSecret := &corev1.Secret{}
			err = r.Get(ctx, types.NamespacedName{Name: secretNameIter, Namespace: odoo.Namespace}, sshSecret)
			if err != nil {
				log.Error(err, "Failed to get SSH Key Secret for custom repository", "Secret.Name", secretNameIter)
				return ctrl.Result{}, err
			}
		}

		addonsJobName := odoo.Name + "-addons-download-job"
		addonsJob := &batchv1.Job{}
		err = r.Get(ctx, types.NamespacedName{Name: addonsJobName, Namespace: odoo.Namespace}, addonsJob)

		if err != nil && errors.IsNotFound(err) {
			job := r.jobForAddonsDownload(odoo, repositoriesToClone, requiredSSHSecrets)
			log.Info("Creating Addons Download Job", "Job.Namespace", job.Namespace, "Job.Name", job.Name)
			if err := r.Create(ctx, job); err != nil {
				log.Error(err, "Failed to create Addons Download Job")
				return ctrl.Result{}, err
			}
			r.setOdooCondition(&odoo.Status, metav1.Condition{
				Type:    "Available",
				Status:  metav1.ConditionFalse,
				Reason:  "AddonsInitializing",
				Message: "Downloading custom/enterprise modules...",
			})
			if err := r.Status().Update(ctx, odoo); err != nil {
				log.Error(err, "Failed to update status for Addons Init")
			}
			return ctrl.Result{Requeue: true}, nil
		} else if err != nil {
			log.Error(err, "Failed to get Addons Download Job")
			return ctrl.Result{}, err
		}

		if addonsJob.Status.Succeeded == 0 {
			if addonsJob.Status.Failed > 0 {
				log.Error(fmt.Errorf("Addons Download Job failed"), "Job.Name", addonsJobName)
				r.setOdooCondition(&odoo.Status, metav1.Condition{
					Type:    "Available",
					Status:  metav1.ConditionFalse,
					Reason:  "AddonsInitFailed",
					Message: "Failed to download custom/enterprise modules.",
				})
				if err := r.Status().Update(ctx, odoo); err != nil {
					log.Error(err, "Failed to update status for Addons failure")
				}
				return ctrl.Result{}, fmt.Errorf("addons download job failed")
			}
			log.Info("Addons Download Job is still running", "Job.Name", addonsJobName)
			return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
		}
		// Job succeeded, proceed.
		log.Info("Addons Download Job completed successfully")
	}

	// Create the ConfigMap if it doesn't exist
	cm := &corev1.ConfigMap{}
	err = r.Get(ctx, types.NamespacedName{Name: odoo.Name + "-config", Namespace: odoo.Namespace}, cm)
	if err != nil && errors.IsNotFound(err) {
		dep := r.configMapForOdoo(odoo, dbHost)
		log.Info("Creating a new ConfigMap", "ConfigMap.Namespace", dep.Namespace, "ConfigMap.Name", dep.Name)
		err = r.Create(ctx, dep)
		if err != nil {
			log.Error(err, "Failed to create new ConfigMap", "ConfigMap.Namespace", dep.Namespace, "ConfigMap.Name", dep.Name)
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	} else if err != nil {
		log.Error(err, "Failed to get ConfigMap")
		return ctrl.Result{}, err
	}

	// --- ODOO INITIALIZATION JOB ---
	// Before creating the Odoo StatefulSet, ensure the database is initialized.
	initJob := &batchv1.Job{}
	jobName := odoo.Name + "-db-init"
	err = r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: odoo.Namespace}, initJob)

	if err != nil && errors.IsNotFound(err) {
		// The Job doesn't exist, and the StatefulSet probably doesn't either.
		// Let's create the Job.
		job := r.jobForOdooInit(odoo, dbHost, secretName)
		log.Info("Creating a new DB initialization Job", "Job.Namespace", job.Namespace, "Job.Name", job.Name)
		if err := r.Create(ctx, job); err != nil {
			log.Error(err, "Failed to create new Job")
			return ctrl.Result{}, err
		}

		// Update status to reflect Job creation
		log.Info("Updating Odoo status to DBInitializing")
		odoo.Status.ReadyReplicas = 0
		odoo.Status.Replicas = odoo.Spec.Size
		odoo.Status.Ready = fmt.Sprintf("0/%d", odoo.Spec.Size)
		r.setOdooCondition(&odoo.Status, metav1.Condition{
			Type:    "Available",
			Status:  metav1.ConditionFalse,
			Reason:  "DBInitializing",
			Message: "The database initialization job is being created.",
		})
		if err := r.Status().Update(ctx, odoo); err != nil {
			log.Error(err, "Failed to update Odoo status for DB init job creation")
			return ctrl.Result{}, err
		}

		log.Info("DB initialization Job created, requeuing.")
		return ctrl.Result{Requeue: true}, nil

	} else if err != nil {
		log.Error(err, "Failed to get DB initialization Job")
		return ctrl.Result{}, err
	}

	// If we get here, the Job exists. Let's check its status.
	if initJob.Status.Succeeded == 0 {
		if initJob.Status.Failed > 0 {
			// Job has failed. Report the error and stop reconciliation.
			log.Error(fmt.Errorf("DB initialization Job failed"), "Job details", "Job.Name", initJob.Name)

			// Update status to reflect Job failure
			log.Info("Updating Odoo status to DBInitFailed")
			r.setOdooCondition(&odoo.Status, metav1.Condition{
				Type:    "Available",
				Status:  metav1.ConditionFalse,
				Reason:  "DBInitFailed",
				Message: "The database initialization job has failed.",
			})
			if err := r.Status().Update(ctx, odoo); err != nil {
				log.Error(err, "Failed to update Odoo status for failed DB init job")
			}

			return ctrl.Result{}, fmt.Errorf("DB initialization Job %s failed", initJob.Name)
		}
		// Job is still running. Log it and requeue.
		log.Info("DB initialization Job is still running", "Job.Name", initJob.Name)

		// Update status to reflect running Job
		log.Info("Updating Odoo status to DBInitializing (job running)")
		odoo.Status.ReadyReplicas = 0
		odoo.Status.Replicas = odoo.Spec.Size
		odoo.Status.Ready = fmt.Sprintf("0/%d", odoo.Spec.Size)
		r.setOdooCondition(&odoo.Status, metav1.Condition{
			Type:    "Available",
			Status:  metav1.ConditionFalse,
			Reason:  "DBInitializing",
			Message: "The database initialization job is running.",
		})
		if err := r.Status().Update(ctx, odoo); err != nil {
			log.Error(err, "Failed to update Odoo status for running DB init job")
			return ctrl.Result{}, err
		}

		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil // Check back in 15 seconds
	}

	// If we get here, the Job has completed successfully.
	log.Info("DB initialization Job completed successfully.", "Job.Name", initJob.Name)

	// --- ODOO UPGRADE ---
	// Check if we need to run an upgrade (migration)
	currentVersion := odoo.Status.CurrentVersion
	targetVersion := odoo.Spec.Version
	if targetVersion == "" {
		targetVersion = "19" // default
	}

	// First installation case: if CurrentVersion is empty, we assume the DB init job
	// installed the target version. We just update the status.
	if currentVersion == "" {
		if odoo.Status.CurrentVersion != targetVersion {
			log.Info("First installation detected, setting CurrentVersion", "Version", targetVersion)
			odoo.Status.CurrentVersion = targetVersion
			if err := r.Status().Update(ctx, odoo); err != nil {
				log.Error(err, "Failed to update Odoo status (CurrentVersion)")
				return ctrl.Result{}, err
			}
		}
	} else if currentVersion != targetVersion && odoo.Spec.Upgrade.Enabled {
		// Version mismatch and upgrade enabled -> trigger upgrade job

		// Determine job name
		safeVersion := strings.ReplaceAll(targetVersion, ".", "-")
		upgradeJobName := fmt.Sprintf("%s-upgrade-%s", odoo.Name, safeVersion)
		upgradeJob := &batchv1.Job{}
		err = r.Get(ctx, types.NamespacedName{Name: upgradeJobName, Namespace: odoo.Namespace}, upgradeJob)

		if err != nil && errors.IsNotFound(err) {
			// Job doesn't exist, create it
			job := r.jobForOdooUpgrade(odoo, dbHost, secretName)
			log.Info("Creating a new Upgrade Job", "Job.Namespace", job.Namespace, "Job.Name", job.Name)
			if err := r.Create(ctx, job); err != nil {
				log.Error(err, "Failed to create Upgrade Job")
				return ctrl.Result{}, err
			}

			// Update status
			log.Info("Updating Odoo status to Upgrading")
			r.setOdooCondition(&odoo.Status, metav1.Condition{
				Type:    "Available",
				Status:  metav1.ConditionFalse,
				Reason:  "Upgrading",
				Message: fmt.Sprintf("Upgrading to version %s", targetVersion),
			})
			odoo.Status.Migration = odoov1alpha1.MigrationStatus{
				Phase:     "Running",
				StartTime: &metav1.Time{Time: time.Now()},
			}
			if err := r.Status().Update(ctx, odoo); err != nil {
				log.Error(err, "Failed to update Odoo status for upgrade start")
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil

		} else if err != nil {
			log.Error(err, "Failed to get Upgrade Job")
			return ctrl.Result{}, err
		}

		// Job exists, check status
		if upgradeJob.Status.Succeeded == 0 {
			if upgradeJob.Status.Failed > 0 {
				// Failed
				log.Error(fmt.Errorf("Upgrade Job failed"), "Job.Name", upgradeJobName)
				r.setOdooCondition(&odoo.Status, metav1.Condition{
					Type:    "Available",
					Status:  metav1.ConditionFalse,
					Reason:  "UpgradeFailed",
					Message: "The upgrade job failed.",
				})
				odoo.Status.Migration.Phase = "Failed"
				if err := r.Status().Update(ctx, odoo); err != nil {
					log.Error(err, "Failed to update Odoo status for upgrade failure")
				}
				return ctrl.Result{}, fmt.Errorf("upgrade job failed")
			}
			// Running
			log.Info("Upgrade Job is still running", "Job.Name", upgradeJobName)
			return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
		}

		// Succeeded
		log.Info("Upgrade Job completed successfully", "Job.Name", upgradeJobName)
		odoo.Status.CurrentVersion = targetVersion
		odoo.Status.Migration.Phase = "Succeeded"
		completionTime := metav1.Now()
		odoo.Status.Migration.CompletionTime = &completionTime

		if err := r.Status().Update(ctx, odoo); err != nil {
			log.Error(err, "Failed to update Odoo status after upgrade success")
			return ctrl.Result{}, err
		}
		// Continue to update StatefulSet...
	}

	// Check if the statefulset already exists before setting the status to "Creating".
	// This prevents status flapping on subsequent reconciliations.
	foundSts := &appsv1.StatefulSet{}
	err = r.Get(ctx, types.NamespacedName{Name: odoo.Name, Namespace: odoo.Namespace}, foundSts)
	if err != nil && errors.IsNotFound(err) {
		// Update status to reflect that the main application is now being created
		log.Info("Updating Odoo status to Creating")
		odoo.Status.ReadyReplicas = 0
		odoo.Status.Replicas = odoo.Spec.Size
		odoo.Status.Ready = fmt.Sprintf("0/%d", odoo.Spec.Size)
		r.setOdooCondition(&odoo.Status, metav1.Condition{
			Type:    "Available",
			Status:  metav1.ConditionFalse,
			Reason:  "Creating",
			Message: "Database initialized successfully. Creating Odoo application.",
		})
		if err := r.Status().Update(ctx, odoo); err != nil {
			log.Error(err, "Failed to update Odoo status after DB init job success")
			return ctrl.Result{}, err
		}
	} else if err != nil {
		log.Error(err, "Failed to check for existing StatefulSet before status update")
		return ctrl.Result{}, err
	}

	// Check if the statefulset already exists, if not create a new one
	found := &appsv1.StatefulSet{}
	err = r.Get(ctx, types.NamespacedName{Name: odoo.Name, Namespace: odoo.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		// Define a new statefulset
		dep := r.statefulSetForOdoo(odoo, dbHost, secretName)
		log.Info("Creating a new StatefulSet", "StatefulSet.Namespace", dep.Namespace, "StatefulSet.Name", dep.Name)
		err = r.Create(ctx, dep)
		if err != nil {
			log.Error(err, "Failed to create new StatefulSet", "StatefulSet.Namespace", dep.Namespace, "StatefulSet.Name", dep.Name)
			return ctrl.Result{}, err
		}
		// StatefulSet created successfully - return and requeue
		return ctrl.Result{Requeue: true}, nil
	} else if err != nil {
		log.Error(err, "Failed to get StatefulSet")
		return ctrl.Result{}, err
	}

	// Ensure the statefulset size is the same as the spec
	size := odoo.Spec.Size
	if *found.Spec.Replicas != size {
		found.Spec.Replicas = &size
		err = r.Update(ctx, found)
		if err != nil {
			log.Error(err, "Failed to update StatefulSet", "StatefulSet.Namespace", found.Namespace, "StatefulSet.Name", found.Name)
			return ctrl.Result{}, err
		}
		// Ask to requeue after 1 minute in order to give enough time for the
		// pods be created on the cluster side and the operand be able
		// to do the next update step accurately.
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	// Ensure the statefulset image matches the spec
	// Regenerate the desired statefulset to get the expected image
	desiredSts := r.statefulSetForOdoo(odoo, dbHost, secretName)
	if len(found.Spec.Template.Spec.Containers) > 0 && len(desiredSts.Spec.Template.Spec.Containers) > 0 {
		desiredImage := desiredSts.Spec.Template.Spec.Containers[0].Image
		currentImage := found.Spec.Template.Spec.Containers[0].Image

		if currentImage != desiredImage {
			log.Info("Updating StatefulSet image", "Current", currentImage, "Desired", desiredImage)
			found.Spec.Template.Spec.Containers[0].Image = desiredImage
			err = r.Update(ctx, found)
			if err != nil {
				log.Error(err, "Failed to update StatefulSet image")
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// Determine if log volume is enabled (default is true)
	logVolumeEnabled := odoo.Spec.Logs.VolumeEnabled == nil || *odoo.Spec.Logs.VolumeEnabled

	// Create/Delete the log PVC based on the spec
	logPvc := &corev1.PersistentVolumeClaim{}
	err = r.Get(ctx, types.NamespacedName{Name: odoo.Name + "-logs-pvc", Namespace: odoo.Namespace}, logPvc)
	if err != nil && errors.IsNotFound(err) {
		if logVolumeEnabled {
			pvc := r.pvcForOdoo(odoo, "logs")
			log.Info("Creating a new PVC for logs", "PVC.Namespace", pvc.Namespace, "PVC.Name", pvc.Name)
			if err := r.Create(ctx, pvc); err != nil {
				log.Error(err, "Failed to create log PVC")
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	} else if err != nil {
		log.Error(err, "Failed to get log PVC")
		return ctrl.Result{}, err
	} else if !logVolumeEnabled {
		log.Info("Deleting log PVC as it is disabled", "PVC.Namespace", logPvc.Namespace, "PVC.Name", logPvc.Name)
		if err := r.Delete(ctx, logPvc); err != nil {
			log.Error(err, "Failed to delete log PVC")
			return ctrl.Result{}, err
		}
	}

	// Create the Services if they don't exist
	services := []string{"service", "headless"}
	for _, serviceName := range services {
		svc := &corev1.Service{}
		err = r.Get(ctx, types.NamespacedName{Name: odoo.Name + "-" + serviceName, Namespace: odoo.Namespace}, svc)
		if err != nil && errors.IsNotFound(err) {
			dep := r.serviceForOdoo(odoo, serviceName)
			log.Info("Creating a new Service", "Service.Namespace", dep.Namespace, "Service.Name", dep.Name)
			err = r.Create(ctx, dep)
			if err != nil {
				log.Error(err, "Failed to create new Service", "Service.Namespace", dep.Namespace, "Service.Name", dep.Name)
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		} else if err != nil {
			log.Error(err, "Failed to get Service")
			return ctrl.Result{}, err
		}
	}

	// Reconcile the Ingress
	ingress := &networkingv1.Ingress{}
	err = r.Get(ctx, types.NamespacedName{Name: odoo.Name, Namespace: odoo.Namespace}, ingress)
	if err != nil && errors.IsNotFound(err) {
		// Create Ingress if enabled in spec
		if odoo.Spec.Ingress.Enabled {
			ing := r.ingressForOdoo(odoo)
			log.Info("Creating a new Ingress", "Ingress.Namespace", ing.Namespace, "Ingress.Name", ing.Name)
			if err := r.Create(ctx, ing); err != nil {
				log.Error(err, "Failed to create new Ingress")
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	} else if err != nil {
		log.Error(err, "Failed to get Ingress")
		return ctrl.Result{}, err
	} else if !odoo.Spec.Ingress.Enabled {
		// Delete Ingress if not enabled in spec
		log.Info("Deleting Ingress", "Ingress.Namespace", ingress.Namespace, "Ingress.Name", ingress.Name)
		if err := r.Delete(ctx, ingress); err != nil {
			log.Error(err, "Failed to delete Ingress")
			return ctrl.Result{}, err
		}
	}

	// --- STATUS UPDATE ---
	// At the end of the reconciliation, update the status to reflect the real state of the world.
	defer func() {
		// This defer block ensures that the status is updated even if there's an error earlier.
		// We need to fetch the latest version of the object to avoid race conditions.
		latestOdoo := &odoov1alpha1.Odoo{}
		if err := r.Get(ctx, req.NamespacedName, latestOdoo); err != nil {
			log.Error(err, "Failed to get latest Odoo for status update")
			return
		}

		// Check the status of the Odoo StatefulSet
		odooSts := &appsv1.StatefulSet{}
		err = r.Get(ctx, types.NamespacedName{Name: latestOdoo.Name, Namespace: latestOdoo.Namespace}, odooSts)

		status := &latestOdoo.Status
		status.Replicas = latestOdoo.Spec.Size

		if err != nil {
			if errors.IsNotFound(err) {
				status.ReadyReplicas = 0
				status.Ready = fmt.Sprintf("0/%d", status.Replicas)
				r.setOdooCondition(status, metav1.Condition{
					Type:    "Available",
					Status:  metav1.ConditionFalse,
					Reason:  "Creating",
					Message: "Dependencies are being created.",
				})
			} else {
				status.Ready = "Unknown"
				r.setOdooCondition(status, metav1.Condition{
					Type:    "Available",
					Status:  metav1.ConditionUnknown,
					Reason:  "Error",
					Message: "Failed to get StatefulSet status.",
				})
			}
		} else {
			status.ReadyReplicas = odooSts.Status.ReadyReplicas
			status.Ready = fmt.Sprintf("%d/%d", status.ReadyReplicas, status.Replicas)

			// Determine the reason for the condition
			reason := "Initializing"
			// A generation change means a spec update is in progress.
			if odooSts.Status.ObservedGeneration < odooSts.Generation {
				reason = "Updating"
			} else if odooSts.Status.Replicas > status.Replicas {
				reason = "ScalingDown"
			} else if odooSts.Status.Replicas < status.Replicas {
				reason = "ScalingUp"
			}

			if status.ReadyReplicas < status.Replicas {
				r.setOdooCondition(status, metav1.Condition{
					Type:    "Available",
					Status:  metav1.ConditionFalse,
					Reason:  reason,
					Message: fmt.Sprintf("Waiting for pods: %s.", status.Ready),
				})
			} else {
				r.setOdooCondition(status, metav1.Condition{
					Type:    "Available",
					Status:  metav1.ConditionTrue,
					Reason:  "Ready",
					Message: "All Odoo pods are ready.",
				})
			}
		}

		if err := r.Status().Update(ctx, latestOdoo); err != nil {
			log.Error(err, "Failed to update Odoo status")
		}
	}()

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *OdooReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&odoov1alpha1.Odoo{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&corev1.Service{}).
		Owns(&batchv1.Job{}).
		Named("odoo").
		Complete(r)
}

func (r *OdooReconciler) statefulSetForOdoo(odoo *odoov1alpha1.Odoo, dbHost, secretName string) *appsv1.StatefulSet {
	ls := labelsForOdoo(odoo.Name)
	replicas := odoo.Spec.Size
	odooVersion := odoo.Spec.Version
	if odooVersion == "" {
		odooVersion = "19" // Default version
	}
	odooImage := fmt.Sprintf("odoo:%s", odooVersion)

	// Define environment variables from secret
	envFromSecret := []corev1.EnvVar{
		{Name: "POSTGRES_USER", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secretName}, Key: "user"}}},
		{Name: "POSTGRES_PASSWORD", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secretName}, Key: "password"}}},
	}

	dep := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      odoo.Name,
			Namespace: odoo.Namespace,
			Labels:    ls,
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: odoo.Name + "-headless",
			Replicas:    &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						RunAsUser:  func() *int64 { i := int64(0); return &i }(),
						RunAsGroup: func() *int64 { i := int64(0); return &i }(),
						FSGroup:    func() *int64 { i := int64(0); return &i }(),
					},
					InitContainers: []corev1.Container{
						{
							Name:  "wait-for-db",
							Image: "busybox",
							Command: []string{"sh", "-c",
								"until nc -z $DB_HOST $DB_PORT; do echo waiting for db; sleep 2; done;",
							},
							Env: []corev1.EnvVar{
								{Name: "DB_HOST", Value: dbHost},
								{Name: "DB_PORT", Value: "5432"},
							},
						},
					},
					Containers: []corev1.Container{{
						Name:  "odoo",
						Image: odooImage,
						Ports: []corev1.ContainerPort{
							{ContainerPort: 8069, Name: "web"},
							{ContainerPort: 8072, Name: "longpolling"},
						},
						Resources: odoo.Spec.Resources.Odoo,
						Env:       append([]corev1.EnvVar{{Name: "HOST", Value: dbHost}}, envFromSecret...),
						VolumeMounts: []corev1.VolumeMount{
							{Name: "odoo-data", MountPath: "/var/lib/odoo"},
							{Name: "odoo-config", MountPath: "/etc/odoo/odoo.conf", SubPath: "odoo.conf"},
							{Name: "odoo-addons-all", MountPath: "/mnt/extra-addons"},
						},
					}},
					Volumes: []corev1.Volume{
						{Name: "odoo-config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: odoo.Name + "-config"}}}},
						{Name: "odoo-data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: odoo.Name + "-data-pvc"}}},
						{Name: "odoo-addons-all", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: odoo.Name + "-addons-pvc"}}},
					},
				},
			},
		},
	}

	// Conditionally add the log volume and mounts
	logVolumeEnabled := odoo.Spec.Logs.VolumeEnabled == nil || *odoo.Spec.Logs.VolumeEnabled
	if logVolumeEnabled {
		logVolume := corev1.Volume{
			Name: "odoo-logs",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: odoo.Name + "-logs-pvc",
				},
			},
		}
		dep.Spec.Template.Spec.Volumes = append(dep.Spec.Template.Spec.Volumes, logVolume)

		logVolumeMount := corev1.VolumeMount{
			Name:      "odoo-logs",
			MountPath: "/var/log/odoo",
		}
		// Add to odoo container
		dep.Spec.Template.Spec.Containers[0].VolumeMounts = append(dep.Spec.Template.Spec.Containers[0].VolumeMounts, logVolumeMount)
	}
	ctrl.SetControllerReference(odoo, dep, r.Scheme)
	return dep
}

func (r *OdooReconciler) jobForOdooInit(odoo *odoov1alpha1.Odoo, dbHost, secretName string) *batchv1.Job {
	ls := labelsForOdoo(odoo.Name)
	odooVersion := odoo.Spec.Version
	if odooVersion == "" {
		odooVersion = "19" // Default version
	}
	odooImage := fmt.Sprintf("odoo:%s", odooVersion)

	// Build modules to install string
	modulesToInstall := ""
	if len(odoo.Spec.Modules.Install) > 0 {
		modulesToInstall = "," + strings.Join(odoo.Spec.Modules.Install, ",")
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      odoo.Name + "-db-init",
			Namespace: odoo.Namespace,
			Labels:    ls,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsUser:  func() *int64 { i := int64(0); return &i }(),
						RunAsGroup: func() *int64 { i := int64(0); return &i }(),
						FSGroup:    func() *int64 { i := int64(0); return &i }(),
					},
					InitContainers: []corev1.Container{
						{
							Name:  "init-volumes",
							Image: "busybox",
							// On crée les dossiers s'ils n'existent pas
							Command: []string{"sh", "-c", "mkdir -p /tmp/mount/enterprise-addons /tmp/mount/custom-addons; chmod -R 777 /tmp/mount/"},
							VolumeMounts: []corev1.VolumeMount{
								// On monte le volume SANS SubPath pour accéder à la racine
								{Name: "odoo-addons-all", MountPath: "/tmp/mount"},
							},
						},
						{
							Name:  "wait-for-db",
							Image: "busybox",
							Command: []string{"sh", "-c",
								"until nc -z $DB_HOST $DB_PORT; do echo waiting for db; sleep 2; done;",
							},
							Env: []corev1.EnvVar{
								{Name: "DB_HOST", Value: dbHost},
								{Name: "DB_PORT", Value: "5432"},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "odoo-db-init",
							Image: odooImage,
							Command: []string{"sh", "-c",
								fmt.Sprintf("odoo -c /etc/odoo/odoo.conf -d $POSTGRES_DB -i base%s --stop-after-init -w $POSTGRES_PASSWORD -r $POSTGRES_USER", modulesToInstall),
							},
							Resources: odoo.Spec.Resources.Init,
							Env: []corev1.EnvVar{
								{Name: "HOST", Value: dbHost},
								{Name: "POSTGRES_USER", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secretName}, Key: "user"}}},
								{Name: "POSTGRES_PASSWORD", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secretName}, Key: "password"}}},
								{Name: "POSTGRES_DB", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secretName}, Key: "dbname"}}},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "odoo-data", MountPath: "/var/lib/odoo"},
								{Name: "odoo-config", MountPath: "/etc/odoo/odoo.conf", SubPath: "odoo.conf"},
								{Name: "odoo-addons-all", MountPath: "/mnt/extra-addons"},
							},
						},
					},
					Volumes: []corev1.Volume{
						{Name: "odoo-config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: odoo.Name + "-config"}}}},
						{Name: "odoo-data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: odoo.Name + "-data-pvc"}}},
						{Name: "odoo-addons-all", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: odoo.Name + "-addons-pvc"}}},
					},
				},
			},
		},
	}
	// Conditionally add log volume mount if enabled
	logVolumeEnabled := odoo.Spec.Logs.VolumeEnabled == nil || *odoo.Spec.Logs.VolumeEnabled
	if logVolumeEnabled {
		logVolume := corev1.Volume{
			Name: "odoo-logs",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: odoo.Name + "-logs-pvc",
				},
			},
		}
		job.Spec.Template.Spec.Volumes = append(job.Spec.Template.Spec.Volumes, logVolume)

		logVolumeMount := corev1.VolumeMount{
			Name:      "odoo-logs",
			MountPath: "/var/log/odoo",
		}
		job.Spec.Template.Spec.Containers[0].VolumeMounts = append(job.Spec.Template.Spec.Containers[0].VolumeMounts, logVolumeMount)
	}

	ctrl.SetControllerReference(odoo, job, r.Scheme)
	return job
}

func (r *OdooReconciler) jobForOdooUpgrade(odoo *odoov1alpha1.Odoo, dbHost, secretName string) *batchv1.Job {
	ls := labelsForOdoo(odoo.Name)
	odooVersion := odoo.Spec.Version
	// We use the NEW version for the upgrade job
	if odooVersion == "" {
		odooVersion = "19"
	}
	odooImage := fmt.Sprintf("odoo:%s", odooVersion)

	modulesToUpgrade := odoo.Spec.Upgrade.Modules // Use specific upgrade modules if defined
	if modulesToUpgrade == "" {
		// Fallback to install list for upgrade if no specific upgrade modules are provided
		if len(odoo.Spec.Modules.Install) > 0 {
			modulesToUpgrade = strings.Join(odoo.Spec.Modules.Install, ",")
		} else {
			modulesToUpgrade = "all" // Default to all if nothing specified
		}
	}

	// Generate a deterministic name for the upgrade job based on version
	// Note: In a real scenario, we might want a hash of the full spec or a timestamp,
	// but here we bind it to the version change.
	// Clean version string to be a valid DNS label
	safeVersion := strings.ReplaceAll(odooVersion, ".", "-")
	jobName := fmt.Sprintf("%s-upgrade-%s", odoo.Name, safeVersion)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: odoo.Namespace,
			Labels:    ls,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsUser:  func() *int64 { i := int64(0); return &i }(),
						RunAsGroup: func() *int64 { i := int64(0); return &i }(),
						FSGroup:    func() *int64 { i := int64(0); return &i }(),
					},
					InitContainers: []corev1.Container{
						{
							Name:  "wait-for-db",
							Image: "busybox",
							Command: []string{"sh", "-c",
								"until nc -z $DB_HOST $DB_PORT; do echo waiting for db; sleep 2; done;",
							},
							Env: []corev1.EnvVar{
								{Name: "DB_HOST", Value: dbHost},
								{Name: "DB_PORT", Value: "5432"},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "odoo-upgrade",
							Image: odooImage,
							Command: []string{"sh", "-c",
								fmt.Sprintf("odoo -c /etc/odoo/odoo.conf -d $POSTGRES_DB -u %s --stop-after-init -w $POSTGRES_PASSWORD -r $POSTGRES_USER", modulesToUpgrade),
							},
							Resources: odoo.Spec.Resources.Init, // Reuse init resources or add dedicated ones
							Env: []corev1.EnvVar{
								{Name: "HOST", Value: dbHost},
								{Name: "POSTGRES_USER", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secretName}, Key: "user"}}},
								{Name: "POSTGRES_PASSWORD", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secretName}, Key: "password"}}},
								{Name: "POSTGRES_DB", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secretName}, Key: "dbname"}}},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "odoo-data", MountPath: "/var/lib/odoo"},
								{Name: "odoo-config", MountPath: "/etc/odoo/odoo.conf", SubPath: "odoo.conf"},
								{Name: "odoo-addons-all", MountPath: "/mnt/extra-addons"},
							},
						},
					},
					Volumes: []corev1.Volume{
						{Name: "odoo-config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: odoo.Name + "-config"}}}},
						{Name: "odoo-data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: odoo.Name + "-data-pvc"}}},
						{Name: "odoo-addons-all", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: odoo.Name + "-addons-pvc"}}},
					},
				},
			},
		},
	}
	// Conditionally add log volume mount if enabled
	logVolumeEnabled := odoo.Spec.Logs.VolumeEnabled == nil || *odoo.Spec.Logs.VolumeEnabled
	if logVolumeEnabled {
		logVolume := corev1.Volume{
			Name: "odoo-logs",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: odoo.Name + "-logs-pvc",
				},
			},
		}
		job.Spec.Template.Spec.Volumes = append(job.Spec.Template.Spec.Volumes, logVolume)

		logVolumeMount := corev1.VolumeMount{
			Name:      "odoo-logs",
			MountPath: "/var/log/odoo",
		}
		job.Spec.Template.Spec.Containers[0].VolumeMounts = append(job.Spec.Template.Spec.Containers[0].VolumeMounts, logVolumeMount)
	}

	ctrl.SetControllerReference(odoo, job, r.Scheme)
	return job
}

func (r *OdooReconciler) jobForAddonsDownload(odoo *odoov1alpha1.Odoo, repositories []odoov1alpha1.GitRepositorySpec, sshSecrets []string) *batchv1.Job {
	ls := labelsForOdoo(odoo.Name)
	jobName := odoo.Name + "-addons-download-job"

	var scriptBuilder strings.Builder
	scriptBuilder.WriteString("#!/bin/sh\nset -e\n\n")

	// Setup SSH for all provided secrets
	scriptBuilder.WriteString("mkdir -p /root/.ssh\n")
	scriptBuilder.WriteString("chmod 700 /root/.ssh\n")
	scriptBuilder.WriteString("echo \"StrictHostKeyChecking no\" >> /root/.ssh/config\n")
	for i := range sshSecrets {
		// Mount path for each secret is unique, /etc/ssh-key-<index>
		scriptBuilder.WriteString(fmt.Sprintf("cp /etc/ssh-key-%d/ssh-privatekey /root/.ssh/id_rsa_%d\n", i, i))
		scriptBuilder.WriteString(fmt.Sprintf("chmod 600 /root/.ssh/id_rsa_%d\n", i))
		scriptBuilder.WriteString(fmt.Sprintf("echo \"IdentityFile /root/.ssh/id_rsa_%d\" >> /root/.ssh/config\n", i))
	}
	scriptBuilder.WriteString("\n")

	// Loop through repositories and clone/update
	for _, repo := range repositories {
		repoVersion := repo.Version
		if repoVersion == "" {
			repoVersion = "main" // Default to main branch for custom repos
			// If Enterprise and version is empty, default to Odoo Spec.Version
			if repo.Name == "enterprise" && odoo.Spec.Version != "" {
				repoVersion = odoo.Spec.Version
			}
		}

		// Determine if it's an SSH URL for adding to known_hosts
		isSSH := strings.HasPrefix(repo.URL, "git@")
		if isSSH {
			host := strings.Split(strings.Split(repo.URL, "@")[1], ":")[0]
			scriptBuilder.WriteString(fmt.Sprintf("ssh-keyscan %s >> /root/.ssh/known_hosts\n", host))
		}

		scriptBuilder.WriteString(fmt.Sprintf(`
TARGET_DIR="/mnt/extra-addons/%s" # Each repo gets its own subdirectory

if [ -d "$TARGET_DIR/.git" ]; then
    echo "Repo %s exists, updating..."
    cd "$TARGET_DIR"
    git config core.sshCommand "ssh -i /root/.ssh/id_rsa_%d"
    git fetch origin
    git checkout "%s"
    git pull origin "%s"
else
    echo "Cloning repo %s..."
    git config core.sshCommand "ssh -i /root/.ssh/id_rsa_%d"
    git clone -b "%s" "%s" "$TARGET_DIR"
fi

`, repo.Name, repo.Name, indexOf(sshSecrets, repo.SSHKeySecretRef), repoVersion, repoVersion, repo.Name, indexOf(sshSecrets, repo.SSHKeySecretRef), repoVersion, repo.URL))
	}

	// VolumeMounts for addons PVC
	volumeMounts := []corev1.VolumeMount{
		{Name: "odoo-addons-all", MountPath: "/mnt/extra-addons"},
	}

	// Volumes for addons PVC and SSH secrets
	volumes := []corev1.Volume{
		{
			Name: "odoo-addons-all",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: odoo.Name + "-addons-pvc",
				},
			},
		},
	}

	// Add SSH secrets to volumes and volume mounts
	for i, secretName := range sshSecrets {
		volumes = append(volumes, corev1.Volume{
			Name: fmt.Sprintf("ssh-key-%d", i),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: secretName,
					Items: []corev1.KeyToPath{
						{Key: "ssh-privatekey", Path: "ssh-privatekey"},
					},
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      fmt.Sprintf("ssh-key-%d", i),
			MountPath: fmt.Sprintf("/etc/ssh-key-%d", i),
			ReadOnly:  true,
		})
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: odoo.Namespace,
			Labels:    ls,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					// Ensure PodSecurityContext is set for Restricted
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot:   func() *bool { b := true; return &b }(),
						RunAsUser:      func() *int64 { i := int64(1000); return &i }(), // Non-root user
						FSGroup:        func() *int64 { i := int64(1000); return &i }(),
						SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					},
					Containers: []corev1.Container{
						{
							Name:         "git-clone",
							Image:        "alpine/git", // A lightweight image with git
							Command:      []string{"/bin/sh", "-c", scriptBuilder.String()},
							VolumeMounts: volumeMounts,
							// Reuse init resources for addons download job
							Resources: odoo.Spec.Resources.Init,
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: func() *bool { b := false; return &b }(),
								Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
							},
						},
					},
					Volumes: volumes,
				},
			},
		},
	}

	ctrl.SetControllerReference(odoo, job, r.Scheme)
	return job
}

// indexOf is a helper to find the index of a string in a slice.
func indexOf(slice []string, item string) int {
	for i, v := range slice {
		if v == item {
			return i
		}
	}
	return -1
}

func (r *OdooReconciler) configMapForOdoo(odoo *odoov1alpha1.Odoo, dbHost string) *corev1.ConfigMap {
	ls := labelsForOdoo(odoo.Name)

	// Start with default options
	options := map[string]string{
		// "addons_path" will be generated dynamically
		"data_dir":          "/var/lib/odoo",
		"admin_passwd":      "admin_password", // Consider making this configurable via a secret
		"db_maxconn":        "64",
		"db_port":           "5432",
		"db_template":       "template1",
		"limit_memory_hard": "1677721600",
		"limit_memory_soft": "6291456000",
		"limit_request":     "8192",
		"limit_time_cpu":    "600",
		"limit_time_real":   "1200",
		"log_handler":       "[':INFO']",
		"log_level":         "info",
		// "logfile" will be set based on VolumeEnabled
		"gevent_port":      "8072",
		"http_port":        "8069",
		"max_cron_threads": "2",
		"workers":          "0",
	}

	// Dynamically build addons_path
	var addonsPathParts []string
	addonsPathParts = append(addonsPathParts, "/usr/lib/python3/dist-packages/")

	// Add Enterprise path if enabled
	if odoo.Spec.Enterprise.Enabled {
		addonsPathParts = append(addonsPathParts, "/mnt/extra-addons/enterprise")
	}

	// Add custom repositories paths
	for _, repo := range odoo.Spec.Modules.Repositories {
		addonsPathParts = append(addonsPathParts, fmt.Sprintf("/mnt/extra-addons/%s", repo.Name))
	}

	options["addons_path"] = strings.Join(addonsPathParts, ",")

	// Add database options
	options["db_host"] = dbHost
	// db_user and db_password are not set here, Odoo will use environment variables

	// Set logfile based on whether the log volume is enabled
	logVolumeEnabled := odoo.Spec.Logs.VolumeEnabled == nil || *odoo.Spec.Logs.VolumeEnabled
	if logVolumeEnabled {
		options["logfile"] = "/var/log/odoo/odoo.log"
	} else {
		// Empty value means log to stdout
		options["logfile"] = ""
	}

	// Merge with user-provided options from the CR
	for key, value := range odoo.Spec.Options {
		options[key] = value
	}

	// Build the odoo.conf content
	var builder strings.Builder
	builder.WriteString("[options]\n")
	for key, value := range options {
		builder.WriteString(fmt.Sprintf("%s = %s\n", key, value))
	}
	odooConfContent := builder.String()

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      odoo.Name + "-config",
			Namespace: odoo.Namespace,
			Labels:    ls,
		},
		Data: map[string]string{
			"odoo.conf": odooConfContent,
		},
	}

	ctrl.SetControllerReference(odoo, cm, r.Scheme)
	return cm
}

func (r *OdooReconciler) pvcForOdoo(odoo *odoov1alpha1.Odoo, name string) *corev1.PersistentVolumeClaim {
	ls := labelsForOdoo(odoo.Name)

	var storageSpec odoov1alpha1.StorageSpec
	var defaultSize string
	var defaultAccessMode corev1.PersistentVolumeAccessMode = corev1.ReadWriteMany

	switch name {
	case "data":
		storageSpec = odoo.Spec.Storage.Data
		defaultSize = "3Gi"
	case "logs":
		storageSpec = odoo.Spec.Storage.Logs
		defaultSize = "2Gi"
	case "addons":
		storageSpec = odoo.Spec.Storage.Addons
		defaultSize = "5Gi"
	}

	// Apply defaults if not specified in the CR
	if storageSpec.Size == "" {
		storageSpec.Size = defaultSize
	}
	if storageSpec.AccessMode == "" {
		storageSpec.AccessMode = defaultAccessMode
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      odoo.Name + "-" + name + "-pvc",
			Namespace: odoo.Namespace,
			Labels:    ls,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{storageSpec.AccessMode},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(storageSpec.Size),
				},
			},
		},
	}

	if storageSpec.StorageClassName != "" {
		pvc.Spec.StorageClassName = &storageSpec.StorageClassName
	}

	ctrl.SetControllerReference(odoo, pvc, r.Scheme)
	return pvc
}

func (r *OdooReconciler) statefulSetForPostgres(odoo *odoov1alpha1.Odoo, secretName string) *appsv1.StatefulSet {
	ls := labelsForOdoo(odoo.Name + "-postgres")
	replicas := int32(1)

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      odoo.Name + "-postgres",
			Namespace: odoo.Namespace,
			Labels:    ls,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "postgres",
						Image: "postgres:15",
						Ports: []corev1.ContainerPort{{
							ContainerPort: 5432,
						}},
						Resources: odoo.Spec.Resources.Postgres,
						Env: []corev1.EnvVar{
							{Name: "POSTGRES_USER", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secretName}, Key: "user"}}},
							{Name: "POSTGRES_PASSWORD", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secretName}, Key: "password"}}},
							{Name: "POSTGRES_DB", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: secretName}, Key: "dbname"}}},
							{Name: "PGDATA", Value: "/var/lib/postgresql/data/pgdata"},
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "postgres-data",
							MountPath: "/var/lib/postgresql/data",
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "postgres-data",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: odoo.Name + "-postgres-pvc",
							},
						},
					}},
				},
			},
		},
	}
	ctrl.SetControllerReference(odoo, sts, r.Scheme)
	return sts
}

func (r *OdooReconciler) pvcForPostgres(odoo *odoov1alpha1.Odoo) *corev1.PersistentVolumeClaim {
	ls := labelsForOdoo(odoo.Name + "-postgres")
	storageSpec := odoo.Spec.Storage.Postgres

	// Apply defaults
	if storageSpec.Size == "" {
		storageSpec.Size = "5Gi"
	}
	if storageSpec.AccessMode == "" {
		storageSpec.AccessMode = corev1.ReadWriteOnce
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      odoo.Name + "-postgres-pvc",
			Namespace: odoo.Namespace,
			Labels:    ls,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{storageSpec.AccessMode},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(storageSpec.Size),
				},
			},
		},
	}

	if storageSpec.StorageClassName != "" {
		pvc.Spec.StorageClassName = &storageSpec.StorageClassName
	}

	ctrl.SetControllerReference(odoo, pvc, r.Scheme)
	return pvc
}

func (r *OdooReconciler) secretForPostgres(odoo *odoov1alpha1.Odoo, name string) *corev1.Secret {
	ls := labelsForOdoo(odoo.Name + "-postgres")
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: odoo.Namespace,
			Labels:    ls,
		},
		StringData: map[string]string{
			"user":     "odoo",
			"password": "odoo",
			"dbname":   "odoo",
		},
	}
	ctrl.SetControllerReference(odoo, secret, r.Scheme)
	return secret
}

func (r *OdooReconciler) serviceForPostgres(odoo *odoov1alpha1.Odoo) *corev1.Service {
	ls := labelsForOdoo(odoo.Name + "-postgres")
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      odoo.Name + "-postgres-svc",
			Namespace: odoo.Namespace,
			Labels:    ls,
		},
		Spec: corev1.ServiceSpec{
			Selector: ls,
			Ports: []corev1.ServicePort{{
				Port: 5432,
			}},
		},
	}
	ctrl.SetControllerReference(odoo, svc, r.Scheme)
	return svc
}

func (r *OdooReconciler) serviceForOdoo(odoo *odoov1alpha1.Odoo, name string) *corev1.Service {
	ls := labelsForOdoo(odoo.Name)
	var svc *corev1.Service
	if name == "service" {
		serviceType := odoo.Spec.Service.Type
		if serviceType == "" {
			serviceType = corev1.ServiceTypeLoadBalancer
		}

		svc = &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      odoo.Name + "-service",
				Namespace: odoo.Namespace,
				Labels:    ls,
			},
			Spec: corev1.ServiceSpec{
				Selector: ls,
				Type:     serviceType,
				Ports: []corev1.ServicePort{
					{Name: "web", Protocol: corev1.ProtocolTCP, Port: 8069, TargetPort: intstr.FromInt(8069)},
					{Name: "longpolling", Protocol: corev1.ProtocolTCP, Port: 8072, TargetPort: intstr.FromInt(8072)},
				},
			},
		}
	} else { // headless
		svc = &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      odoo.Name + "-headless",
				Namespace: odoo.Namespace,
				Labels:    ls,
			},
			Spec: corev1.ServiceSpec{
				ClusterIP: "None",
				Selector:  ls,
				Ports: []corev1.ServicePort{
					{Name: "web", Port: 8069, TargetPort: intstr.FromInt(8069)},
					{Name: "longpolling", Port: 8072, TargetPort: intstr.FromInt(8072)},
				},
			},
		}
	}

	ctrl.SetControllerReference(odoo, svc, r.Scheme)
	return svc
}

func (r *OdooReconciler) setOdooCondition(status *odoov1alpha1.OdooStatus, condition metav1.Condition) {
	// Helper function to set a condition on the Odoo status.
	// This function will update an existing condition or add a new one.
	if status.Conditions == nil {
		status.Conditions = make([]metav1.Condition, 0)
	}

	now := metav1.Now()
	condition.LastTransitionTime = now

	for i, c := range status.Conditions {
		if c.Type == condition.Type {
			// Update existing condition only if the status or reason has changed
			if c.Status != condition.Status || c.Reason != condition.Reason {
				status.Conditions[i] = condition
			}
			return
		}
	}
	// Add new condition
	status.Conditions = append(status.Conditions, condition)
}

func (r *OdooReconciler) ingressForOdoo(odoo *odoov1alpha1.Odoo) *networkingv1.Ingress {
	ls := labelsForOdoo(odoo.Name)
	pathType := networkingv1.PathTypePrefix
	ingressClassName := "nginx" // Default value
	if odoo.Spec.Ingress.IngressClassName != nil {
		ingressClassName = *odoo.Spec.Ingress.IngressClassName
	}

	// Define default annotations
	annotations := map[string]string{
		"nginx.ingress.kubernetes.io/proxy-read-timeout":       "720s",
		"nginx.ingress.kubernetes.io/proxy-send-timeout":       "720s",
		"nginx.ingress.kubernetes.io/proxy-body-size":          "512m",
		"nginx.ingress.kubernetes.io/ssl-redirect":             "true",
		"nginx.ingress.kubernetes.io/proxy-max-temp-file-size": "2048m",
	}

	// Merge with annotations from the CR, with CR annotations taking precedence
	for key, value := range odoo.Spec.Ingress.Annotations {
		annotations[key] = value
	}

	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        odoo.Name,
			Namespace:   odoo.Namespace,
			Labels:      ls,
			Annotations: annotations,
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &ingressClassName,
			Rules: []networkingv1.IngressRule{
				{
					Host: odoo.Spec.Ingress.Host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/websocket",
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: odoo.Name + "-service",
											Port: networkingv1.ServiceBackendPort{
												Number: 8072,
											},
										},
									},
								},
								{
									Path:     "/",
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: odoo.Name + "-service",
											Port: networkingv1.ServiceBackendPort{
												Number: 8069,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if odoo.Spec.Ingress.TLS {
		ing.Spec.TLS = []networkingv1.IngressTLS{
			{
				Hosts:      []string{odoo.Spec.Ingress.Host},
				SecretName: odoo.Name + "-tls",
			},
		}
	}

	ctrl.SetControllerReference(odoo, ing, r.Scheme)
	return ing
}

func labelsForOdoo(name string) map[string]string {
	return map[string]string{"app": "odoo", "odoo_cr": name}
}

// containsString checks if a string is present in a slice of strings.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// removeString removes a string from a slice of strings.
func removeString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}
