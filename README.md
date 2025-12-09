# Odoo Kubernetes Operator

This operator allows you to deploy and manage [Odoo](https://www.odoo.com/) instances on Kubernetes with ease. It handles the lifecycle of the Odoo application, including provisioning PostgreSQL databases (managed or external), handling persistent storage, managing Odoo Enterprise and custom modules from Git repositories, and performing backups and restores.

## Features

*   **Managed Deployment**: Automated deployment of Odoo (StatefulSet) and PostgreSQL (StatefulSet or external).
*   **Custom Modules Management**: 
    *   Install community or custom modules directly from Git repositories.
    *   Support for private repositories via SSH keys.
    *   Automatic updates of modules when configuration changes.
*   **Odoo Enterprise Ready**: Easy configuration to pull Enterprise addons from the official private repository.
*   **Resource Control**: Granular configuration of CPU/Memory for all containers.
*   **Backup & Restore**: Native support for backing up the Odoo database and restoring it (`OdooBackup` and `OdooRestore` CRDs).
*   **Automatic Upgrades**: Triggers migration scripts (`odoo -u ...`) automatically when the Odoo version changes.

## Quick Start

### 1. Install the Operator

```sh
# Install CRDs and deploy the operator manager
make install
make deploy IMG=alterway/odoo-operator:latest
```

### 2. Basic Odoo Community Deployment

Create a simple Odoo Community instance with a managed PostgreSQL database.

```yaml
apiVersion: cloud.alterway.fr/v1alpha1
kind: Odoo
metadata:
  name: odoo-community
  namespace: default
spec:
  version: "19"
  size: 1
  service:
    type: LoadBalancer
  ingress:
    enabled: true
    host: odoo.example.com
    tls: true
  storage:
    data:
      size: "5Gi"
    postgres:
      size: "5Gi"
  modules:
    install:
      - sale
      - website
```

### 3. Odoo Enterprise with Custom Modules

This example demonstrates how to deploy Odoo Enterprise and install custom modules from a private Git repository.

**Prerequisites:**
*   Create a Kubernetes Secret `ssh-key-odoo` containing the SSH private key for the Enterprise repo.
*   Create a Kubernetes Secret `ssh-key-custom` for your private custom repository.

```yaml
apiVersion: cloud.alterway.fr/v1alpha1
kind: Odoo
metadata:
  name: odoo-enterprise
spec:
  version: "18.0"
  enterprise:
    enabled: true
    sshKeySecretRef: ssh-key-odoo # Secret containing 'ssh-privatekey'
  
  modules:
    # Clone custom addons from a private Git repo
    repositories:
      - name: my-custom-addons
        url: git@github.com:my-org/my-odoo-addons.git
        version: "18.0"
        sshKeySecretRef: ssh-key-custom
    
    # List of modules to install (from core, enterprise, or custom repos)
    install:
      - account_accountant # Enterprise module
      - my_custom_module   # From the custom repo
      - sale
```

### 4. Backup and Restore

**Backup:**

```yaml
apiVersion: cloud.alterway.fr/v1alpha1
kind: OdooBackup
metadata:
  name: backup-daily
spec:
  odooRef:
    name: odoo-enterprise
  storageLocation:
    pvc:
      claimName: backup-pvc
```

**Restore:**

```yaml
apiVersion: cloud.alterway.fr/v1alpha1
kind: OdooRestore
metadata:
  name: restore-job
spec:
  odooRef:
    name: odoo-enterprise
  backupSource:
    odooBackupRef:
      name: backup-daily
  restoreMethod: StopAndRestore # Will stop Odoo, restore DB, and restart
```

### 5. High Availability with Redis

To enable scalable session storage, you can configure Odoo to use Redis. The operator can manage a Redis instance for you or connect to an external one.

**Managed Redis:**

```yaml
apiVersion: cloud.alterway.fr/v1alpha1
kind: Odoo
metadata:
  name: odoo-ha
spec:
  version: "19"
  size: 2 # Now you can scale Odoo!
  redis:
    enabled: true
    managed: true
```

**External Redis:**

```yaml
apiVersion: cloud.alterway.fr/v1alpha1
kind: Odoo
metadata:
  name: odoo-external-redis
spec:
  # ...
  redis:
    enabled: true
    managed: false
    host: "my-redis-service.default.svc.cluster.local"
    port: 6379
```

## Configuration Reference

### OdooSpec

| Field | Description | Default |
|-------|-------------|---------|
| `version` | Odoo version tag (e.g., "19", "18.0") | "19" |
| `size` | Number of Odoo replicas | 1 |
| `database` | External DB config. If empty, a managed Postgres is created. | - |
| `enterprise` | Configuration for Enterprise edition (enabled, repo, key). | - |
| `modules` | List of modules to install and external Git repositories. | - |
| `redis` | Configuration for Redis session storage. | - |
| `storage` | PVC configurations for data, logs, addons, and postgres. | - |
| `resources` | Resource requests/limits for containers. | - |

*(See CRD definitions in `api/v1alpha1` for full details)*

## Development

**Prerequisites**
- go version v1.24.6+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

**Run locally:**
```sh
make install
make run
```

**Run tests:**
```sh
make test
```

## License

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