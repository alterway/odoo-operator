# Prompt pour la recréation de l'opérateur Odoo

**Role:** Expert Kubernetes & Développeur Go.
**Objectif:** Créer un Kubernetes Operator pour Odoo en utilisant le framework Kubebuilder.

**Contexte du Projet:**
L'opérateur doit gérer le cycle de vie d'instances Odoo sur Kubernetes. Il doit supporter deux modes de fonctionnement pour la base de données :
1.  **Mode Managé :** L'opérateur déploie et gère une instance PostgreSQL dédiée (StatefulSet).
2.  **Mode Externe :** L'opérateur se connecte à une base PostgreSQL existante fournie par l'utilisateur.

**Spécifications Techniques:**
*   **Langage :** Go (v1.24+)
*   **Framework :** Kubebuilder
*   **API Group :** `cloud.alterway.fr`
*   **API Version :** `v1alpha1`
*   **Kind :** `Odoo`

**Détails de la CRD (`OdooSpec`) :**
La CRD doit exposer les champs suivants :
*   `Size` (int32, required, min 1, max 10) : Nombre de réplicas Odoo.
*   `Version` (string, default "19") : Version de l'image Docker Odoo.
*   `Database` (struct) : Configuration pour une DB externe (`Host`, `User`, `Password`).
*   `DatabaseSecretName` (string) : Nom du secret contenant les identifiants DB (`user`, `password`, `dbname`). Obligatoire en mode externe.
*   `Ingress` (struct) : Configuration de l'Ingress (`Enabled`, `Host`, `TLS`, `IngressClassName`, `Annotations`).
*   `Service` (struct) : Configuration du Service (`Type`, default "LoadBalancer").
*   `Storage` (struct) : Configuration des PVCs (`Data`, `Logs`, `CustomAddons`, `EnterpriseAddons`, `Postgres`). Chaque entrée doit permettre de définir `Size`, `StorageClassName`, et `AccessMode`.
*   `Logs` (struct) : `VolumeEnabled` (bool, default true) pour activer/désactiver le PVC de logs.
*   `Options` (map[string]string) : Surcharges pour le fichier de configuration `odoo.conf`.

**Logique du Contrôleur (`Reconcile`) :**
Le contrôleur doit implémenter la logique suivante :

1.  **Gestion de la Base de Données :**
    *   Si `Database.Host` est défini (Mode Externe) : Vérifier la présence du secret `DatabaseSecretName`.
    *   Si Mode Managé :
        *   Créer/Vérifier un Secret par défaut (user/pass/db: odoo/odoo/odoo).
        *   Créer/Vérifier un PVC pour les données Postgres.
        *   Créer/Vérifier un Service pour Postgres (port 5432).
        *   Créer/Vérifier un StatefulSet pour Postgres (image `postgres:15`).

2.  **Volumes (PVCs) :**
    *   Créer les PVCs nécessaires pour Odoo : `data`, `custom-addons`, `enterprise-addons`.
    *   Gérer le PVC `logs` conditionnellement selon `Spec.Logs.VolumeEnabled`.

3.  **Configuration (ConfigMap) :**
    *   Générer un ConfigMap contenant `odoo.conf`.
    *   Inclure des valeurs par défaut (ex: `addons_path`, `limit_memory_hard`, etc.) et fusionner avec `Spec.Options`.

4.  **Initialisation de la DB (Job) :**
    *   Avant de lancer Odoo, créer un Job Kubernetes pour initialiser la base (`odoo -i base`).
    *   Utiliser un init-container (`busybox`) pour attendre que le port DB soit ouvert (`nc -z`).
    *   Mettre à jour le Status de la CR (`DBInitializing`, `DBInitFailed`) selon l'état du Job.

5.  **Déploiement Odoo (StatefulSet) :**
    *   Une fois la DB initialisée, créer/vérifier le StatefulSet Odoo.
    *   Configurer les `VolumeMounts` (data, config, addons, logs).
    *   Configurer les variables d'environnement (`HOST`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, etc. depuis le secret).
    *   Ajouter un init-container pour attendre la disponibilité de la DB.

6.  **Réseau (Service & Ingress) :**
    *   Créer un Service principal (type défini dans la Spec) exposant les ports 8069 (web) et 8072 (longpolling).
    *   Créer un Service Headless pour le StatefulSet.
    *   Si activé, créer un Ingress avec des règles pour `/` (port 8069) et `/websocket` (port 8072). Gérer le TLS si demandé.

7.  **Statut (Status) :**
    *   Mettre à jour `Ready`, `Replicas`, `ReadyReplicas`.
    *   Gérer les `Conditions` : `Available`, `Creating`, `Initializing`, `Ready`.

**Livrables attendus :**
*   Le code complet de `api/v1alpha1/odoo_types.go`.
*   Le code complet de `internal/controller/odoo_controller.go`.
*   Un `Makefile` complet incluant les cibles de build, test, et déploiement.
*   Un `Dockerfile` optimisé (multi-stage build, distroless).
