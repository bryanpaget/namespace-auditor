Namespace Auditor Controller
============================

A Kubernetes controller that automatically cleans up Kubeflow namespaces when their associated Entra ID (Azure AD) user accounts no longer exist.

Features
--------
- Automatically validates user accounts via Microsoft Graph API
- Grace period (default: 48h) before namespace deletion
- Safe namespace marking system with audit annotations
- Kubernetes-native implementation using controller-runtime

Prerequisites
-------------
- Go 1.18+
- Kubernetes cluster (v1.22+)
- kubectl configured with cluster access
- Azure AD App Registration with:
  - Microsoft Graph API permission: User.Read.All
  - Client ID/Secret with admin consent

Installation
------------

1. Clone the repository:
   git clone https://github.com/bryanpaget/namespace-auditor.git
   cd namespace-auditor

2. Build and push the Docker image:
   docker build -t bryanpaget/namespace-auditor:latest .
   docker push bryanpaget/namespace-auditor:latest

3. Deploy to Kubernetes:
   # Create Azure credential secret
   kubectl create secret generic azure-creds \
     --from-literal=tenantId=<AZURE_TENANT_ID> \
     --from-literal=clientId=<AZURE_CLIENT_ID> \
     --from-literal=clientSecret=<AZURE_CLIENT_SECRET>

   # Apply RBAC and deployment
   kubectl apply -f config/rbac/role.yaml
   kubectl apply -f config/manager/deployment.yaml

Configuration
-------------
Environment Variables:
- AZURE_TENANT_ID: Azure AD tenant ID
- AZURE_CLIENT_ID: Azure AD application ID
- AZURE_CLIENT_SECRET: Azure AD client secret
- GRACE_PERIOD: Duration before deletion (default: 48h)

Namespace Requirements:
- Must have label: user-email=<user@domain.com>
- Example namespace manifest:

  apiVersion: v1
  kind: Namespace
  metadata:
    name: user-namespace
    labels:
      user-email: "user@statcan.gc.ca"

Usage
-----
1. Create test namespace:
   kubectl apply -f examples/test-namespace.yaml

2. Check controller logs:
   kubectl logs -l app=namespace-cleaner --follow

3. When the associated Azure AD user is deleted:
   - Namespace will be annotated with deletion timestamp
   - Namespace will be deleted after grace period

Verification
------------
1. Check namespace annotations:
   kubectl get namespace <name> -o jsonpath='{.metadata.annotations}'

2. Monitor deletion timeline:
   kubectl get namespaces -w

3. Audit logs will show:
   - User validation attempts
   - Namespace marking events
   - Final deletion operations
