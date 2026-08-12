# FAR Operator Post-Deployment Tests

Automated tests validating the Fence Agents Remediation (FAR) operator deployment, security posture, and high-availability configuration.

## Prerequisites

- OpenShift cluster with FAR operator installed via OLM
- `KUBECONFIG` set with cluster-admin access
- FAR installed in `openshift-workload-availability` namespace

## Running

```bash
ginkgo --label-filter="far" ./tests/far-operator/...
```

## Tests

### 1. Verify FAR Operator Pod is Running ([OCP-66026](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-66026))

Validates that FAR controller-manager pods are in Running state and the pod count matches the cluster topology (2 on multi-node, 1 on SNO).

- **Operators**: FAR v0.8.0
- **Cluster**: Any topology (MNO or SNO)
- **Storage**: None
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far" --focus="pod is running" ./tests/far-operator/...`
- **Pass criteria**: All pods Running, count matches expected replicas for the topology

### 2. Verify FAR CSV Has Required Annotations ([OCP-70637](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-70637))

Validates that the active FAR ClusterServiceVersion (in Succeeded phase) has all required OLM feature annotations: disconnected support, FIPS compliance, suggested namespace, and feature flags.

- **Operators**: FAR v0.8.0
- **Cluster**: Any topology
- **Storage**: None
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far" --focus="required annotations" ./tests/far-operator/...`
- **Pass criteria**: All required annotations present with expected values on the active CSV

### 3. Verify FAR Controller Replicas and Node Distribution ([OCP-61222](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-61222))

Validates that 2 replicas are running and scheduled on different nodes for high availability. Skipped on SNO clusters where only 1 replica is expected.

- **Operators**: FAR v0.8.0
- **Cluster**: Multi-node only (skips on SNO)
- **Storage**: None
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far" --focus="correct number of replicas" ./tests/far-operator/...`
- **Pass criteria**: 2 ready replicas on 2 different nodes

### 4. Verify FAR Container Security Context ([OCP-89231](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-89231))

Validates the manager container follows the restricted security posture: runAsNonRoot at pod level, runAsUser is not UID 0 when set, allowPrivilegeEscalation=false, capabilities.drop=ALL, readOnlyRootFilesystem=true, and seccompProfile=RuntimeDefault (at container or pod level). Only checks the `manager` container, not sidecars.

- **Operators**: FAR v0.8.0
- **Cluster**: Any topology
- **Storage**: None
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far" --focus="non-root user" ./tests/far-operator/...`
- **Pass criteria**: All security context fields match expected restricted profile

### 5. Verify FAR CRDs Are Installed and Established ([OCP-89548](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-89548))

Validates that both FAR Custom Resource Definitions are registered as cluster-level resources and have the `Established=True` status condition, confirming the API endpoints are active and ready for clients.

- **Operators**: FAR v0.8.0
- **Cluster**: Any topology
- **Storage**: None
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far" --focus="CRDs are installed" ./tests/far-operator/...`
- **Pass criteria**: Both CRDs (`fenceagentsremediations` and `fenceagentsremediationtemplates`) exist with Established=True

### 6. Verify FAR Operator Namespace Has Correct PSA Enforcement Label ([OCP-89549](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-89549))

Validates that the operator namespace (`openshift-workload-availability`) has the correct Pod Security Admission enforcement label set to `privileged`, ensuring the namespace admission policy allows the operator pods to run with required permissions.

- **Operators**: FAR v0.8.0
- **Cluster**: Any topology
- **Storage**: None
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far" --focus="PSA enforcement label" ./tests/far-operator/...`
- **Pass criteria**: Namespace has `pod-security.kubernetes.io/enforce=privileged` label

### 7. Verify FAR Controller Has system-cluster-critical Priority Class ([OCP-66211](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-66211))

Validates that all FAR controller-manager pods have `priorityClassName` set to `system-cluster-critical`, ensuring the controller retains scheduling priority during node pressure events.

- **Operators**: FAR v0.8.0
- **Cluster**: Any topology
- **Storage**: None
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far" --focus="priority class" ./tests/far-operator/...`
- **Pass criteria**: All running FAR pods have `priorityClassName: system-cluster-critical`

### 8. Verify FAR Controller Pod Has Correct Kubernetes Labels ([OCP-66209](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-66209))

Validates that FAR controller-manager pods carry the standard `app.kubernetes.io/name` label with the correct value, ensuring service discovery and monitoring tools can identify FAR pods.

- **Operators**: FAR v0.8.0
- **Cluster**: Any topology
- **Storage**: None
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far" --focus="Kubernetes labels" ./tests/far-operator/...`
- **Pass criteria**: All running FAR pods have `app.kubernetes.io/name=fence-agents-remediation-operator`

### 9. Verify FAR Controller Container Includes Expected Fence Agents ([OCP-78407](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-78407))

Validates that the FAR controller container image ships the minimum expected set of fence agent binaries in `/usr/sbin/`. Execs into the container and lists all `fence_*` binaries, then checks that a core subset (fence_aws, fence_azure_arm, fence_gce, fence_ipmilan, fence_kubevirt, fence_redfish) is present.

- **Operators**: FAR v0.8.0
- **Cluster**: Any topology
- **Storage**: None
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far" --focus="fence agents" ./tests/far-operator/...`
- **Pass criteria**: All expected fence agent binaries are present in the container

## Non-Destructive Tests -- Negative Validation and Webhook Rejection

Tests that verify FAR webhook rejection of invalid CRs and controller
behavior with misconfigured resources. No node disruption -- all tests
are pure API-level.

### Prerequisites (Negative Validation)

- FAR operator installed
- `KUBECONFIG` set with cluster-admin access

### 10. Verify Node-Not-Found Error for Non-Existent CR Name ([OCP-65954](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-65954))

Creates a FAR CR with a name that does not match any cluster node. Verifies the controller logs the node-not-found error and does not attempt fencing.

- **Operators**: FAR v0.8.0+
- **Cluster**: Any topology
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far && disruption:nondestructive" --focus="node-not-found" ./tests/far-operator/...`
- **Pass criteria**: FAR CR created successfully, controller log contains "Could not find CR's target node"

### 11. Verify Unsupported Action Rejection ([OCP-66090](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-66090))

Creates a FAR CR with `--action=status` (unsupported). Verifies the webhook rejects the CR at creation time with an error about unsupported action.

- **Operators**: FAR v0.8.0+
- **Cluster**: Any topology
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far && disruption:nondestructive" --focus="unsupported action" ./tests/far-operator/...`
- **Pass criteria**: CR creation rejected with error containing "FAR doesn't support any other action than"

### 12. Verify Invalid Fence Agent Rejection in FAR CR ([OCP-71219](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-71219))

Attempts to create FAR CRs with invalid fence agent names. Two sub-cases: (1) agent name matches `fence_` prefix but binary not in container -- webhook rejects with "unsupported fence agent"; (2) agent name missing `fence_` prefix -- CRD schema validation rejects.

- **Operators**: FAR v0.8.0+
- **Cluster**: Any topology
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far && disruption:nondestructive" --focus="invalid fence agent name" ./tests/far-operator/...`
- **Pass criteria**: First sub-case rejected with "unsupported fence agent", second rejected with "spec.agent in body should match"

### 13. Verify Invalid Fence Agent Rejection in FARTemplate ([OCP-71220](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-71220))

Same validation as OCP-71219 but for FenceAgentsRemediationTemplate CRs. Two sub-cases with the same agent names and expected errors, with the CRD path `spec.template.spec.agent` in the validation message.

- **Operators**: FAR v0.8.0+
- **Cluster**: Any topology
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far && disruption:nondestructive" --focus="FenceAgentsRemediationTemplate with invalid" ./tests/far-operator/...`
- **Pass criteria**: First sub-case rejected with "unsupported fence agent", second rejected with "spec.template.spec.agent in body should match"

## Destructive Tests

Tests that trigger node fencing via `fence_aws` and cause node reboots. Require AWS IPI cluster with 3+ worker nodes and AWS fencing credentials.

### 14. Verify Standalone FAR Remediation ([OCP-61229](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-61229))

Creates a FenceAgentsRemediation CR targeting a worker node. Validates that the fence agent reboots the node and the node object is preserved (not re-created).

- **Operators**: FAR v0.8.0+
- **Cluster**: AWS IPI, 3+ worker nodes
- **Storage**: None
- **Environment**: Connected
- **Standalone**: `ginkgo --label-filter="far && disruption:destructive" --focus="standalone FAR CR" ./tests/far-operator/...`
- **Pass criteria**: Node boot ID changes, node creation timestamp unchanged, node returns to Ready, FAR lifecycle events emitted on CR (RemediationStarted, FenceAgentSucceeded, RemediationFinished) and NodeRemediationCompleted event emitted on Node

### 15. Verify Remediation on Active Controller Node ([OCP-70638](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-70638))

Creates a FAR CR targeting the node hosting the active FAR controller pod. Validates that controller failover occurs and remediation completes despite the leader being fenced.

- **Operators**: FAR v0.8.0+
- **Cluster**: AWS IPI, 3+ worker nodes
- **Storage**: None
- **Environment**: Connected
- **Standalone**: `ginkgo --label-filter="far && disruption:destructive" --focus="active FAR controller" ./tests/far-operator/...`
- **Pass criteria**: Node reboots, node returns to Ready, FAR controller replicas recover, controller lease transfers to a different pod, workload pod evicted, FAR lifecycle events survive leader failover (RemediationStarted, FenceAgentSucceeded, RemediationFinished on CR; NodeRemediationCompleted on Node)

### 16. Verify FAR NoSchedule Taint During Remediation ([OCP-65960](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-65960))

Creates a FAR CR and verifies that the FAR NoSchedule taint is applied to the target node during the remediation process.

- **Operators**: FAR v0.8.0+
- **Cluster**: AWS IPI, 3+ worker nodes
- **Storage**: None
- **Environment**: Connected
- **Standalone**: `ginkgo --label-filter="far && disruption:destructive" --focus="NoSchedule taint" ./tests/far-operator/...`
- **Pass criteria**: FAR taint `remediation.medik8s.io/fence-agents-remediation:NoSchedule` applied during remediation

### 17. Verify FAR CR Status Conditions After Remediation ([OCP-67015](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-67015))

Creates a FAR CR and after remediation completes, verifies the CR status conditions match the expected terminal state: Processing=False, FenceAgentActionSucceeded=True, Succeeded=True.

- **Operators**: FAR v0.8.0+
- **Cluster**: AWS IPI, 3+ worker nodes
- **Storage**: None
- **Environment**: Connected
- **Standalone**: `ginkgo --label-filter="far && disruption:destructive" --focus="status conditions" ./tests/far-operator/...`
- **Pass criteria**: All three FAR CR conditions present with expected values

### 18. Verify FAR Default Reboot Action ([OCP-66203](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-66203))

Creates a FAR CR without the `--action` parameter in shared parameters. Validates that FAR defaults to the reboot action and the node is successfully rebooted.

- **Operators**: FAR v0.8.0+
- **Cluster**: AWS IPI, 3+ worker nodes
- **Storage**: None
- **Environment**: Connected
- **Standalone**: `ginkgo --label-filter="far && disruption:destructive" --focus="action is omitted" ./tests/far-operator/...`
- **Pass criteria**: Node reboots despite no explicit action parameter

### 19. Verify Controller Leadership Handover ([OCP-70636](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-70636))

Deletes the active FAR controller pod and validates that a new pod acquires the controller lease. This test does not fence any nodes; it verifies leader election recovery only.

- **Operators**: FAR v0.8.0+
- **Cluster**: Multi-node, 2+ controller replicas
- **Storage**: None
- **Environment**: Connected or disconnected
- **Standalone**: `ginkgo --label-filter="far" --focus="controller leadership" ./tests/far-operator/...`
- **Pass criteria**: FAR deployment becomes ready, controller lease is held by a different pod

### 20. Verify FAR Operator Survives OCP and Operator Upgrade ([OCP-89717](https://polarion.engineering.redhat.com/polarion/#/project/OSE/workitem?id=OCP-89717))

Validates the full customer upgrade path: install GA FAR from redhat-operators on OCP N-1, upgrade OCP to N, run remediation to confirm GA operator works on upgraded cluster, switch Subscription to Konflux FBC catalog (pre-GA), and run remediation again to confirm upgraded operator works. Each remediation cycle creates a workload pod on the target node, fences the node via fence_aws, and verifies node reboot, recovery, and workload eviction via OutOfServiceTaint.

- **Operators**: FAR GA (from redhat-operators) + FAR pre-GA (from Konflux FBC)
- **Cluster**: AWS IPI, 3+ worker nodes, OCP N-1 at start (upgraded to N during test)
- **Storage**: None
- **Environment**: Connected
- **Labels**: `tier:upgrade`, `disruption:destructive`, `platform:aws`, `frequency:weekly`, `component:olm`
- **Env vars (required)**: `OPENSHIFT_UPGRADE_RELEASE_IMAGE_OVERRIDE` (falls back to `RELEASE_IMAGE_LATEST` if unset)
- **CI prerequisite**: `medik8s-catalogsource` step must run before the test (creates the `medik8s-catalog` CatalogSource)
- **Env vars (optional, have defaults)**: `MEDIK8S_OPERATOR_PACKAGE` (default: `fence-agents-remediation`), `MEDIK8S_TARGET_CHANNEL` (default: `stable`)
- **Standalone**: `ginkgo --label-filter="far && tier:upgrade" ./tests/far-operator/...`
- **Pass criteria**: FAR deployment Ready on OCP N-1, OCP upgrade completes (Progressing=False, Available=True, Degraded=False), FAR deployment Ready after OCP upgrade, FAR CSV in Succeeded phase after catalog switch (new CSV if Konflux version is higher than GA, same CSV if versions match), controller image changes after operator upgrade (skipped on version parity), remediation succeeds after OCP upgrade and after catalog switch (node rebooted via boot ID change, node recovers to Ready), workload pods evicted after each fencing cycle
