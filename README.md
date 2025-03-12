# Namespace Auditor Controller

A Kubernetes controller for **Statistics Canada** that automatically cleans up Kubeflow namespaces when their associated Entra ID (Azure AD) user accounts no longer exist.

## Features
- **Automated Validation:** Checks user existence via Microsoft Graph API.
- **Grace Period:** Configurable safety window before deletion (default: 48h).
- **Audit Trail:** Annotations track deletion lifecycle.
- **Cluster Native:** Built with controller-runtime.

```mermaid
flowchart TD
    A[User Creates Namespace] --> B[Annotation: owner=...]
    B --> C[Controller Detects Namespace]
    C --> D{User Exists in Entra ID?}
    D -->|Yes| E[Leave Namespace]
    D -->|No| F[Mark with Deletion Timestamp]
    F --> G{Grace Period Expired?}
    G -->|Yes| H[Delete Namespace]
    G -->|No| F
```

## Installation

### 1. Clone & Build
```bash
git clone https://github.com/bryanpaget/namespace-auditor.git
cd namespace-auditor
docker build -t bryanpaget/namespace-auditor:latest .
docker push bryanpaget/namespace-auditor:latest
```

### 2. Azure Credentials
```bash
kubectl create secret generic azure-creds \
  --from-literal=tenantId=<YOUR_TENANT> \
  --from-literal=clientId=<CLIENT_ID> \
  --from-literal=clientSecret=<CLIENT_SECRET>
```

### 3. Deploy
```bash
kubectl apply -f config/rbac/role.yaml
kubectl apply -f config/manager/deployment.yaml
```

## Configuration

| Environment Variable | Description               | Default |
|----------------------|---------------------------|---------|
| `AZURE_TENANT_ID`    | Azure AD Tenant ID        | Required|
| `AZURE_CLIENT_ID`    | Application Client ID     | Required|
| `AZURE_CLIENT_SECRET`| Client Secret             | Required|
| `GRACE_PERIOD`       | Deletion delay duration   | `48h`   |

## Namespace Requirements
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: user-namespace
  annotations:
    owner: "user.name@statcan.gc.ca"  # or user.name@cloud.statcan.ca
```

## Verification

**Check Annotations:**
```bash
kubectl get namespace <NAME> -o jsonpath='{.metadata.annotations}'
```

**Monitor Deletions:**
```bash
kubectl get namespaces -w -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.annotations.owner}{"\n"}{end}'
```

**View Logs:**
```bash
kubectl logs -l app=namespace-auditor --tail=50 -f
```

## Architecture Flow
```mermaid
sequenceDiagram
    participant User
    participant K8s
    participant Controller
    participant AzureAD

    User->>K8s: Create Namespace with Owner Annotation
    K8s->>Controller: Event Trigger
    loop Every 5m
        Controller->>AzureAD: Validate User
        AzureAD-->>Controller: Response
        alt Invalid User
            Controller->>K8s: Add Deletion Annotation
            Controller->>K8s: Delete After Grace Period
        end
    end
```

## Troubleshooting

**Common Errors:**
- `CreateContainerConfigError`: Missing/misconfigured Azure secret
- `Forbidden`: RBAC permissions mismatch
- `ImagePullBackOff`: Incorrect image name/tag

**Debug Commands:**
```bash
kubectl describe pod <AUDITOR_POD>
kubectl get events --sort-by=.metadata.creationTimestamp
kubectl auth can-i delete namespaces --as=system:serviceaccount:default:namespace-auditor
```

## Security

- Rotate Azure credentials quarterly
- Limit controller permissions using RBAC
- Enable Kubernetes audit logging
- Consider namespace isolation for production
- Annotation Validation: Only processes @statcan.gc.ca and @cloud.statcan.ca domains

## Diagrams

### Grace Period Calculation Flow

``` mermaid
flowchart TD
    A[Start] --> B[Parse Annotation Time]
    B --> C{Valid Time?}
    C -->|No| D[Clear Annotation]
    C -->|Yes| E[Calculate Remaining Time]
    E --> F{Time Remaining >0?}
    F -->|Yes| G[Requeue After Remainder]
    F -->|No| H[Delete Namespace]
```

### main() Function Flow

``` mermaid
flowchart TD
    A[Start] --> B[Load Env Vars]
    B --> C{Azure Creds Exist?}
    C -->|No| D[Exit with Error]
    C -->|Yes| E[Create Azure Client]
    E --> F[Create Controller Manager]
    F --> G[Setup Reconciler]
    G --> H[Start Manager]
    H --> I{Error?}
    I -->|Yes| J[Log Error]
    I -->|No| K[Run Until Signal]
```

### Reconcile() Function Detailed Flow

``` mermaid
flowchart TD
    A[Start] --> B[Fetch Namespace]
    B --> C{Exists?}
    C -->|No| D[Return]
    C -->|Yes| E{DeletionTimestamp?}
    E -->|Yes| F[Log & Return]
    E -->|No| G{user-email Label?}
    G -->|No| H[Return]
    G -->|Yes| I[Check Entra ID]
    I --> J{User Exists?}
    J -->|Yes| K{Annotation Present?}
    K -->|Yes| L[Remove Annotation]
    K -->|No| M[Return]
    J -->|No| N{Annotation Exists?}
    N -->|No| O[Add Annotation]
    N -->|Yes| P{Valid Timestamp?}
    P -->|No| Q[Remove Annotation]
    P -->|Yes| R{Grace Expired?}
    R -->|Yes| S[Delete Namespace]
    R -->|No| T[Requeue After Delay]
```

### SetupWithManager() Flow

``` mermaid
flowchart TD
    A[Start] --> B[Create Controller]
    B --> C[Watch Namespaces]
    C --> D[Filter by user-email Label]
    D --> E[Complete Setup]
```

### UserExists() Flow (Azure Check)

``` mermaid
flowchart TD
    A[Start] --> B[Get Azure Token]
    B --> C{Token Valid?}
    C -->|No| D[Return Error]
    C -->|Yes| E[Build Graph API Request]
    E --> F[Send HTTP Request]
    F --> G{Status Code?}
    G -->|200| H[Return True]
    G -->|404| I[Return False]
    G -->|Other| J[Return Error]
```


