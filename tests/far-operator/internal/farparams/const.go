package farparams

import (
	"time"

	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
)

const (
	// Label is the operator name used in the suite-level Labels array.
	Label = "far"
	// DefaultPollInterval is the polling interval used with Eventually calls.
	DefaultPollInterval = 5 * time.Second
	// BootIDPollInterval is the polling interval for reboot detection via boot ID changes.
	// Longer than DefaultPollInterval because kubelet may lag updating status.nodeInfo.bootID.
	BootIDPollInterval = 10 * time.Second

	// ExpectedReplicas defines the expected number of replicas for FAR controller manager.
	ExpectedReplicas = int32(2)

	// ManagerContainerName is the name of the main controller container in the FAR pod.
	ManagerContainerName = "manager"

	// FenceAgentsRemediationCRDName is the full CRD name for FenceAgentsRemediation.
	FenceAgentsRemediationCRDName = "fenceagentsremediations.fence-agents-remediation.medik8s.io"
	// FenceAgentsRemediationTemplateCRDName is the full CRD name for FenceAgentsRemediationTemplate.
	FenceAgentsRemediationTemplateCRDName = "fenceagentsremediationtemplates.fence-agents-remediation.medik8s.io"

	// PSAEnforceLabelKey is the Pod Security Admission enforcement label key.
	PSAEnforceLabelKey = "pod-security.kubernetes.io/enforce"
	// PSAExpectedLevel is the expected PSA enforcement level for the operator namespace.
	PSAExpectedLevel = "privileged"

	// ExpectedPriorityClassName is the priorityClassName that FAR controller pods must have.
	ExpectedPriorityClassName = "system-cluster-critical"

	// ControllerPodLabelKey is the standard K8s label key for the FAR controller pod.
	ControllerPodLabelKey = "app.kubernetes.io/name"

	// FenceAgentBinaryPrefix is the filename prefix for fence agent binaries in /usr/sbin.
	FenceAgentBinaryPrefix = "fence_"

	// FenceAgentAWS is the fence agent binary for AWS EC2 fencing.
	FenceAgentAWS = "fence_aws"

	// FenceAgentIPMI is the fence agent binary for IPMI fencing.
	FenceAgentIPMI = "fence_ipmilan"

	// NodeIdentifierAWS is the fence agent parameter for AWS instance ID.
	NodeIdentifierAWS = "--plug"

	// NodeIdentifierIPMI is the fence agent parameter for IPMI port.
	NodeIdentifierIPMI = "--ipport"

	// AWSCredentialsSecretName is the Secret name provisioned by the CredentialsRequest.
	AWSCredentialsSecretName = "aws-cloud-fencing-credentials-secret"

	// AWSAccessKeyField is the Secret data key for the AWS access key ID.
	AWSAccessKeyField = "aws_access_key_id"

	// AWSSecretKeyField is the Secret data key for the AWS secret access key.
	AWSSecretKeyField = "aws_secret_access_key"

	// NodeReadyTimeout is how long to wait for a node to become Ready after reboot.
	NodeReadyTimeout = 10 * time.Minute

	// NodeNotReadyTimeout is how long to wait for a node to become NotReady after kubelet stop.
	NodeNotReadyTimeout = 5 * time.Minute

	// NodeRebootTimeout is how long to wait for a node reboot to complete.
	NodeRebootTimeout = 6 * time.Minute

	// OcDebugTimeout is the timeout for oc debug node commands.
	OcDebugTimeout = 60 * time.Second

	// FARConditionTimeout is how long to wait for a FAR CR condition to appear.
	FARConditionTimeout = 2 * time.Minute

	// RemediationCRDeletionTimeout is how long to wait for a FAR/FARTemplate CR to be fully deleted.
	RemediationCRDeletionTimeout = 2 * time.Minute

	// ControllerLeaseName is the FAR leader election lease name (LeaderElectionID in cmd/main.go).
	ControllerLeaseName = "cb305759.medik8s.io"

	// FARConditionProcessing is the condition type for remediation progress.
	FARConditionProcessing = "Processing"
	// FARConditionFenceAgentSucceeded is the condition type for fence agent action result.
	FARConditionFenceAgentSucceeded = "FenceAgentActionSucceeded"
	// FARConditionSucceeded is the condition type for overall remediation outcome.
	FARConditionSucceeded = "Succeeded"

	// FARNoScheduleTaintKey is the taint key applied by FAR during remediation.
	FARNoScheduleTaintKey = "remediation.medik8s.io/fence-agents-remediation"

	// FAREventRemediationStarted is the event reason emitted when remediation begins.
	FAREventRemediationStarted = "RemediationStarted"
	// FAREventFenceAgentSucceeded is the event reason emitted when the fence agent action succeeds.
	FAREventFenceAgentSucceeded = "FenceAgentSucceeded"
	// FAREventRemediationFinished is the event reason emitted when remediation completes.
	FAREventRemediationFinished = "RemediationFinished"
	// FAREventNodeRemediationCompleted is the event reason emitted on the Node when remediation completes.
	FAREventNodeRemediationCompleted = "NodeRemediationCompleted"

	// ControllerHandoverTimeout is how long to wait for controller leadership transfer.
	ControllerHandoverTimeout = 3 * time.Minute
	// WorkloadEvictionTimeout is how long to wait for workload pods to be evicted.
	WorkloadEvictionTimeout = 5 * time.Minute
	// WorkloadPodReadyTimeout is how long to wait for a test workload pod to reach Running.
	WorkloadPodReadyTimeout = 2 * time.Minute

	// FARCRRetryCount is the retry count for FAR/FARTemplate CR spec (matches upstream default).
	FARCRRetryCount = 10
	// FARCRRetryInterval is the retry interval for FAR/FARTemplate CR spec.
	FARCRRetryInterval = "20s"
	// FARCRTimeout is the fence agent command timeout for FAR/FARTemplate CR spec.
	FARCRTimeout = "60s"
	// FARCRRemediationStrategy is the default remediation strategy for FAR CRs.
	FARCRRemediationStrategy = "OutOfServiceTaint"

	// CrioCleanupTimeout is the timeout for the post-remediation CRI-O overlay cleanup.
	CrioCleanupTimeout = 2 * time.Minute

	// SharedCredentialsSecretName is the Secret created by the test suite to hold
	// fence agent credentials in the format expected by SharedSecretName.
	SharedCredentialsSecretName = "far-test-shared-credentials"

	// NodeNotFoundMsg is the controller log message when a FAR CR name doesn't match any node.
	// Current source uses this message; older Konflux builds used NodeNotFoundMsgLegacy.
	NodeNotFoundMsg = "couldn't find node matching remediation"

	// NodeNotFoundMsgLegacy is the old controller log message for node-not-found (pre-v0.8.1).
	// TODO: remove once all Konflux builds use the current message.
	NodeNotFoundMsgLegacy = "Could not find CR's target node"

	// UnsupportedActionMsg is the webhook error when an unsupported action is configured.
	UnsupportedActionMsg = "FAR doesn't support any other action than"

	// UnsupportedAgentMsg is the webhook error when a fence agent binary is not in the container.
	UnsupportedAgentMsg = "unsupported fence agent"

	// InvalidAgentPatternFARMsg is the CRD validation error for FAR CR agent name not matching fence_ prefix.
	InvalidAgentPatternFARMsg = "spec.agent in body should match"

	// InvalidAgentPatternFARTemplateMsg is the CRD validation error for FARTemplate agent name not matching fence_ prefix.
	InvalidAgentPatternFARTemplateMsg = "spec.template.spec.agent in body should match"

	// LogSearchTimeout is the Eventually timeout when polling controller logs for a message.
	LogSearchTimeout = 2 * time.Minute

	// MisconfigTestCRName is the FAR CR name used by the invalid-name misconfiguration test.
	MisconfigTestCRName = "non-existing-node"

	// MisconfigUnsupportedAgent is a fence agent name that passes prefix validation but is not installed.
	MisconfigUnsupportedAgent = "fence_incorrect"

	// MisconfigInvalidPrefixAgent is a fence agent name that fails the fence_ prefix validation.
	MisconfigInvalidPrefixAgent = "incorrect_fence"

	// MisconfigFARTemplateName is the FARTemplate name used by misconfiguration tests.
	MisconfigFARTemplateName = "fenceagentsremediationtemplate-test"

	// WebhookTestCRName is the FAR CR name used by webhook rejection tests.
	// Uses a placeholder (not a real node) since webhook validates agent/action, not node.
	WebhookTestCRName = "far-webhook-test-node"
)

// WorkloadTestImage is the container image used for test workload pods.
var WorkloadTestImage = medik8sparams.WorkloadImage
