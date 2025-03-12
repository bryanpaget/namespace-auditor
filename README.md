# Namespace Auditor Controller

A Kubernetes controller for **Statistics Canada** that automatically cleans up Kubeflow namespaces when their associated Entra ID (Azure AD) user accounts no longer exist.

## Features
- **Automated Validation:** Checks user existence via Microsoft Graph API.
- **Grace Period:** Configurable safety window before deletion (default: 48h).
- **Audit Trail:** Annotations track deletion lifecycle.
- **Cluster Native:** Built with controller-runtime.

```mermaid
flowchart TD
    A[User Creates Namespace] --> B[Label: user-email=...]
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
  labels:
    user-email: "user@statcan.gc.ca"  # Mandatory label
```

## Verification

**Check Annotations:**
```bash
kubectl get namespace <NAME> -o jsonpath='{.metadata.annotations}'
```

**Monitor Deletions:**
```bash
kubectl get namespaces -w -L user-email
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

    User->>K8s: Create Labeled Namespace
    K8s->>Controller: Event Trigger
    loop Every 5m
        Controller->>AzureAD: Validate User
        AzureAD-->>Controller: Response
        alt Invalid User
            Controller->>K8s: Annotate Namespace
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
