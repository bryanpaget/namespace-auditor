# Namespace Auditor CronJob

A scheduled cleaner for Kubeflow profiles that removes namespaces belonging to invalid StatCan users after a 90-day grace period.

```mermaid
flowchart TD
    A[Daily Cron Trigger] --> B[Get Kubeflow Profiles]
    B --> C{Has owner Annotation?}
    C -->|No| D[Skip]
    C -->|Yes| E[Validate Email Domain]
    E -->|Invalid| F[Skip]
    E -->|Valid| G{User Exists in Entra ID?}
    G -->|Yes| H[Clear Deletion Marker]
    G -->|No| I{Marked for Deletion?}
    I -->|No| J[Add Deletion Timestamp]
    I -->|Yes| K{Grace Period Expired?}
    K -->|Yes| L[Delete Namespace]
    K -->|No| M[Skip]
```

### Key Features

- Runs daily at midnight
- Processes only Kubeflow profile namespaces
- 90-day grace period before deletion
- Automatic cleanup of invalid @statcan.gc.ca/@cloud.statcan.ca accounts

### Configuration

``` yaml
# ConfigMap
grace-period: "2160h"  # 90 days
allowed-domains: "statcan.gc.ca,cloud.statcan.ca"

# Secret
azure-creds:
  tenant-id: <ENTRA_ID>
  client-id: <APP_ID>
  client-secret: <SECRET>
```

### Deployment

``` bash
kubectl apply -f config/configmap.yaml
kubectl apply -f config/secret.yaml
kubectl apply -f cronjob.yaml
```

### Verification

 ``` bash
# Check last execution
kubectl get cronjob namespace-auditor -o jsonpath='{.status.lastScheduleTime}'

# View audit markers
kubectl get ns -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.annotations}{"\n"}{end}'
```
