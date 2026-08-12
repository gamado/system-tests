package tests

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/deployment"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/pod"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/reportxml"

	"github.com/medik8s/system-tests/tests/far-operator/internal/farparams"
	"github.com/medik8s/system-tests/tests/far-operator/internal/farutils"
	"github.com/medik8s/system-tests/tests/internal/helpers"
	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
)

var _ = Describe("FAR Negative -- Misconfiguration",
	Serial, Ordered, ContinueOnFailure,
	Label(labels.OperatorFAR, farparams.Label,
		labels.DisruptionNonDestructive, labels.FrequencyWeekly,
		labels.TierAcceptance, labels.PlatformAny),
	func() {
		var (
			ctx        context.Context
			workerNode string
		)

		BeforeAll(func() {
			ctx = context.Background()

			By("Verifying FAR controller deployment is Ready")

			farDeployment, err := deployment.Pull(
				APIClient, farparams.OperatorDeploymentName, medik8sparams.OperatorNs)
			Expect(err).ToNot(HaveOccurred(), "Failed to get FAR deployment")
			Expect(farDeployment.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(),
				"FAR deployment is not Ready -- webhook will be unreachable")

			By("Verifying MisconfigTestCRName is not a real cluster node")

			node := &corev1.Node{}
			err = APIClient.Get(ctx, client.ObjectKey{Name: farparams.MisconfigTestCRName}, node)
			Expect(k8serrors.IsNotFound(err)).To(BeTrue(),
				"Test requires %q to not be a real node name -- a matching node would trigger fencing",
				farparams.MisconfigTestCRName)

			By("Selecting a worker node not running the FAR controller")

			leaderNode, err := farutils.GetActiveFARControllerNode(ctx, APIClient)
			Expect(err).ToNot(HaveOccurred(), "Failed to get FAR controller leader node")

			selectedNode, err := helpers.SelectWorkerNode(ctx, APIClient, leaderNode)
			Expect(err).ToNot(HaveOccurred(), "Failed to select worker node")
			workerNode = selectedNode.Name
			GinkgoWriter.Printf("Selected worker node: %s (leader on: %s)\n",
				workerNode, leaderNode)

			By("Pre-cleaning stale FAR CRs from previous interrupted runs")

			cleanupFARCR(farparams.MisconfigTestCRName)
			cleanupFARCR(workerNode)
			cleanupFARTCR(farparams.MisconfigFARTName)
		})

		AfterEach(func() {
			cleanupFARCR(farparams.MisconfigTestCRName)

			if workerNode != "" {
				cleanupFARCR(workerNode)
			}

			cleanupFARTCR(farparams.MisconfigFARTName)
		})

		Context("controller log messages", func() {
			It("should log node-not-found error for CR with non-existent node name",
				reportxml.ID("65954"),
				Label(labels.ComponentRemediation),
				func() {
					By("Building FAR CR with name that does not match any cluster node")

					farCR := buildMisconfigFAR(farparams.MisconfigTestCRName,
						farparams.FenceAgentIPMI, nil, nil)

					logBaseline := time.Now()

					By("Creating FAR CR")

					Expect(APIClient.Create(ctx, farCR)).To(Succeed(),
						"Failed to create FAR CR with non-existent node name")

					By("Verifying FAR CR exists")

					created := &unstructured.Unstructured{}
					created.SetGroupVersionKind(farGVK)
					Expect(APIClient.Get(ctx, client.ObjectKey{
						Name:      farparams.MisconfigTestCRName,
						Namespace: medik8sparams.OperatorNs,
					}, created)).To(Succeed(),
						"FAR CR %s should exist after creation", farparams.MisconfigTestCRName)

					By("Waiting for node-not-found message in FAR controller logs")

					Eventually(func() error {
						return findMessageInFARControllerLogs(
							farparams.NodeNotFoundMsg, time.Since(logBaseline))
					}, farparams.LogSearchTimeout, farparams.DefaultPollInterval).Should(Succeed(),
						"%q should appear in FAR controller logs", farparams.NodeNotFoundMsg)
				})

			It("should reject FAR CR with unsupported action",
				reportxml.ID("66090"),
				Label(labels.ComponentWebhook),
				func() {
					By("Creating FAR CR with --action=status (unsupported) targeting real worker node")

					sharedParams := map[string]interface{}{
						"--action": "status",
					}

					farCR := buildMisconfigFAR(workerNode,
						farparams.FenceAgentIPMI, sharedParams, nil)

					err := APIClient.Create(ctx, farCR)
					Expect(err).To(MatchError(ContainSubstring(farparams.UnsupportedActionMsg)),
						"FAR CR with unsupported action should be rejected by webhook")
				})
		})

		Context("webhook rejection", func() {
			It("should reject FAR CR with invalid fence agent name",
				reportxml.ID("71219"),
				Label(labels.ComponentWebhook),
				func() {
					By("Creating FAR CR with unsupported agent (fence_incorrect)")

					farCR := buildMisconfigFAR(workerNode,
						farparams.MisconfigUnsupportedAgent, nil, nil)

					err := APIClient.Create(ctx, farCR)
					Expect(err).To(MatchError(ContainSubstring(farparams.UnsupportedAgentMsg)),
						"FAR CR with unsupported fence agent should be rejected by webhook")

					By("Creating FAR CR with agent name missing fence_ prefix (incorrect_fence)")

					farCR2 := buildMisconfigFAR(workerNode,
						farparams.MisconfigInvalidPrefixAgent, nil, nil)

					err = APIClient.Create(ctx, farCR2)
					Expect(err).To(MatchError(ContainSubstring(farparams.InvalidAgentPatternFARMsg)),
						"FAR CR with invalid agent prefix should be rejected by CRD validation")
				})

			It("should reject FenceAgentsRemediationTemplate with invalid fence agent name",
				reportxml.ID("71220"),
				Label(labels.ComponentWebhook),
				func() {
					By("Creating FARTemplate with unsupported agent (fence_incorrect)")

					fartCR := buildMisconfigFART(farparams.MisconfigFARTName,
						farparams.MisconfigUnsupportedAgent)

					err := APIClient.Create(ctx, fartCR)
					Expect(err).To(MatchError(ContainSubstring(farparams.UnsupportedAgentMsg)),
						"FARTemplate with unsupported fence agent should be rejected by webhook")

					By("Creating FARTemplate with agent name missing fence_ prefix (incorrect_fence)")

					fartCR2 := buildMisconfigFART(farparams.MisconfigFARTName,
						farparams.MisconfigInvalidPrefixAgent)

					err = APIClient.Create(ctx, fartCR2)
					Expect(err).To(MatchError(ContainSubstring(farparams.InvalidAgentPatternFARTMsg)),
						"FARTemplate with invalid agent prefix should be rejected by CRD validation")
				})
		})
	})

// cleanupFARCR safely deletes a FenceAgentsRemediation CR by name.
func cleanupFARCR(name string) {
	GinkgoHelper()

	helpers.DeleteRemediationCR(
		context.TODO(), APIClient, farGVK, name, medik8sparams.OperatorNs,
		farparams.DefaultPollInterval, farparams.RemediationCRDeletionTimeout,
		GinkgoWriter.Printf)
}

// cleanupFARTCR safely deletes a FenceAgentsRemediationTemplate CR by name.
func cleanupFARTCR(name string) {
	GinkgoHelper()

	helpers.DeleteRemediationCR(
		context.TODO(), APIClient, fartGVK, name, medik8sparams.OperatorNs,
		farparams.DefaultPollInterval, farparams.RemediationCRDeletionTimeout,
		GinkgoWriter.Printf)
}

// findMessageInFARControllerLogs searches all running FAR controller pod logs
// for the given message within the specified time window.
func findMessageInFARControllerLogs(message string, logWindow time.Duration) error {
	farPods, err := pod.List(APIClient, medik8sparams.OperatorNs, metav1.ListOptions{
		LabelSelector: farparams.OperatorControllerPodLabelSelector,
	})
	if err != nil {
		return fmt.Errorf("failed to list FAR controller pods: %w", err)
	}

	runningPods := helpers.FilterRunningPods(farPods)
	if len(runningPods) == 0 {
		return fmt.Errorf("no running FAR controller pods found (total listed: %d)", len(farPods))
	}

	var lastLogErr error

	for _, farPod := range runningPods {
		logStr, logErr := farPod.GetLog(logWindow, farparams.ManagerContainerName)
		if logErr != nil {
			lastLogErr = fmt.Errorf("pod %s: %w", farPod.Object.Name, logErr)

			continue
		}

		if strings.Contains(logStr, message) {
			return nil
		}
	}

	if lastLogErr != nil {
		return fmt.Errorf("message %q not found; last log error: %w", message, lastLogErr)
	}

	return fmt.Errorf("message %q not found in any FAR controller pod logs (last %s)",
		message, logWindow)
}

// misconfigSharedParams returns the default IPMI shared parameters matching the
// Python template (fence-agents-remediation.yaml).
func misconfigSharedParams() map[string]interface{} {
	return map[string]interface{}{
		"--action":   "reboot",
		"--ip":       "192.168.123.1",
		"--lanplus":  "",
		"--password": "password",
		"--username": "admin",
	}
}

// buildMisconfigFAR builds a FenceAgentsRemediation CR matching the Python
// template (fence-agents-remediation.yaml). sharedOverrides lets individual
// tests replace specific shared parameters (e.g. "--action": "status").
func buildMisconfigFAR(
	name, agent string,
	sharedOverrides, nodeParams map[string]interface{},
) *unstructured.Unstructured {
	sharedParams := misconfigSharedParams()

	for k, v := range sharedOverrides {
		sharedParams[k] = v
	}

	if nodeParams == nil {
		nodeParams = map[string]interface{}{
			farparams.NodeIdentifierIPMI: map[string]interface{}{
				name: "6233",
			},
		}
	}

	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "fence-agents-remediation.medik8s.io/v1alpha1",
			"kind":       "FenceAgentsRemediation",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": medik8sparams.OperatorNs,
			},
			"spec": map[string]interface{}{
				"agent":               agent,
				"retrycount":          farparams.FARCRRetryCount,
				"retryinterval":       farparams.FARCRRetryInterval,
				"timeout":             farparams.FARCRTimeout,
				"remediationStrategy": farparams.FARCRRemediationStrategy,
				"sharedparameters":    sharedParams,
				"nodeparameters":      nodeParams,
			},
		},
	}
}

// buildMisconfigFART builds a FenceAgentsRemediationTemplate CR matching the
// Python template (fence-agents-remediation-template.yaml).
func buildMisconfigFART(name, agent string) *unstructured.Unstructured {
	sharedParams := misconfigSharedParams()

	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "fence-agents-remediation.medik8s.io/v1alpha1",
			"kind":       "FenceAgentsRemediationTemplate",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": medik8sparams.OperatorNs,
			},
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{
						"agent":         agent,
						"retrycount":    farparams.FARCRRetryCount,
						"retryinterval": farparams.FARCRRetryInterval,
						"timeout":       farparams.FARCRTimeout,
						"nodeparameters": map[string]interface{}{
							// Placeholder -- webhook rejects before node params are evaluated
							farparams.NodeIdentifierIPMI: map[string]interface{}{
								"placeholder-node": "6233",
							},
						},
						"sharedparameters": sharedParams,
					},
				},
			},
		},
	}
}
