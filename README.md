Namespace Auditor
==================

Automated Kubernetes namespace cleaner for Kubeflow profiles with configurable grace periods and domain validation.

Configuration Files
--------------------
1. configmap.yaml - Application settings:
   Location: deploy/configmap.yaml
   Contents:
     data:
       allowed-domains: "company.com,example.org"  # Comma-separated list
       grace-period: "720h"                       # Duration format

2. secret.yaml - Azure AD credentials:
   Location: deploy/secret.yaml
   Contents:
     stringData:
       tenant-id: <AZURE_TENANT_ID>         # Azure directory ID
       client-id: <AZURE_CLIENT_ID>         # Application ID
       client-secret: <AZURE_CLIENT_SECRET> # Client secret value

Deployment Steps
----------------
1. Edit configuration files:
   - Update allowed domains in deploy/configmap.yaml
   - Add Azure credentials to deploy/secret.yaml

2. Apply to cluster:

``` bash
kubectl apply -f deploy/configmap.yaml  # Domain rules
kubectl apply -f deploy/secret.yaml     # Azure credentials
kubectl apply -f deploy/rbac.yaml
kubectl apply -f deploy/cronjob.yaml
```

Azure Credential Management
---------------------------
Production Cluster:
- Credentials stored in secret.yaml
- Accessed via Kubernetes Secret mount

Local Development:
- Export matching environment variables:
``` bash
export AZURE_TENANT_ID=<value-from-secret.yaml>
export AZURE_CLIENT_ID=<value-from-secret.yaml>
export AZURE_CLIENT_SECRET=<value-from-secret.yaml>
```

Monitoring & Validation
-----------------------
Verify configurations:
# Check applied ConfigMap values
kubectl get configmap namespace-auditor-config -o yaml

# Inspect secret metadata (values hidden)
kubectl describe secret azure-creds

Security Notes
--------------
- secret.yaml contains sensitive credentials - never commit to source control
- configmap.yaml stores non-sensitive configuration
- Production deployments should use:
  * SealedSecrets for secret.yaml
  * Namespace restrictions for configmap.yaml
  * Network policies limiting access

Testing Workflows
-----------------
Local Testing (no Azure):
- Uses testdata/config.yaml and testdata/namespaces.yaml
- Run with: make test-local

Cluster Dry Run:
- Enable via cronjob environment variable:
  kubectl set env cronjob/namespace-auditor DRY_RUN="true"

Azure Integration Tests:
- Requires valid secret.yaml credentials
- Run with: AZURE_INTEGRATION=1 make test-integration

Maintenance
-----------
- Review configmap.yaml when adding new allowed domains
- Rotate secret.yaml credentials quarterly
- Monitor cronjob execution logs:
  kubectl logs -l app=namespace-auditor --tail=100
