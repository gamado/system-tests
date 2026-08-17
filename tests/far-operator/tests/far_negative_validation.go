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
		var ctx context.Context

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

			By("Pre-cleaning stale FAR CRs from previous interrupted runs")

			cleanupFARCR(farparams.MisconfigTestCRName)
			cleanupFARCR(farparams.WebhookTestCRName)
			cleanupFARTemplateCR(farparams.MisconfigFARTemplateName)

			DeferCleanup(func() {
				By("Cleaning up log-test FAR CR")
				cleanupFARCR(farparams.MisconfigTestCRName)

				By("Cleaning up webhook-test FAR CR")
				cleanupFARCR(farparams.WebhookTestCRName)

				By("Cleaning up test FARTemplate")
				cleanupFARTemplateCR(farparams.MisconfigFARTemplateName)
			})
		})

		Context("controller log messages", func() {
			It("should log node-not-found error for CR with non-existent node name",
				reportxml.ID("65954"),
				Label(labels.ComponentRemediation),
				func() {
					By("Building FAR CR with name that does not match any cluster node")

					farCR := buildFARUnstructured(
						farparams.MisconfigTestCRName,
						farparams.FenceAgentIPMI,
						ipmiSharedParams(nil),
						ipmiNodeParams(farparams.MisconfigTestCRName))

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
						logWindow := time.Since(logBaseline)

						if err := findMessageInFARControllerLogs(
							farparams.NodeNotFoundMsg, logWindow); err == nil {
							return nil
						}

						return findMessageInFARControllerLogs(
							farparams.NodeNotFoundMsgLegacy, logWindow)
					}, farparams.LogSearchTimeout, farparams.DefaultPollInterval).Should(Succeed(),
						"FAR controller logs should contain %q or %q",
						farparams.NodeNotFoundMsg, farparams.NodeNotFoundMsgLegacy)
				})
		})

		Context("webhook rejection", Label(labels.ComponentWebhook), func() {
			It("should reject FAR CR with unsupported action",
				reportxml.ID("66090"),
				func() {
					By("Creating FAR CR with --action=status (unsupported)")

					farCR := buildFARUnstructured(
						farparams.WebhookTestCRName,
						farparams.FenceAgentIPMI,
						ipmiSharedParams(map[string]interface{}{"--action": "status"}),
						ipmiNodeParams(farparams.WebhookTestCRName))

					err := APIClient.Create(ctx, farCR)
					Expect(err).To(MatchError(ContainSubstring(farparams.UnsupportedActionMsg)),
						"FAR CR with unsupported action should be rejected by webhook")
				})

			It("should reject FAR CR with unsupported fence agent name",
				reportxml.ID("71219"),
				func() {
					By("Creating FAR CR with unsupported agent (fence_incorrect)")

					farCR := buildFARUnstructured(
						farparams.WebhookTestCRName,
						farparams.MisconfigUnsupportedAgent,
						ipmiSharedParams(nil),
						ipmiNodeParams(farparams.WebhookTestCRName))

					err := APIClient.Create(ctx, farCR)
					Expect(err).To(MatchError(ContainSubstring(farparams.UnsupportedAgentMsg)),
						"FAR CR with unsupported fence agent should be rejected by webhook")
				})

			It("should reject FAR CR with agent name missing fence_ prefix",
				reportxml.ID("71219"),
				func() {
					By("Creating FAR CR with agent name missing fence_ prefix (incorrect_fence)")

					farCR := buildFARUnstructured(
						farparams.WebhookTestCRName,
						farparams.MisconfigInvalidPrefixAgent,
						ipmiSharedParams(nil),
						ipmiNodeParams(farparams.WebhookTestCRName))

					err := APIClient.Create(ctx, farCR)
					Expect(err).To(MatchError(ContainSubstring(farparams.InvalidAgentPatternFARMsg)),
						"FAR CR with invalid agent prefix should be rejected by CRD validation")
				})

			It("should reject FARTemplate with unsupported fence agent name",
				reportxml.ID("71220"),
				func() {
					By("Creating FARTemplate with unsupported agent (fence_incorrect)")

					farTemplateCR := buildFARTemplateUnstructured(
						farparams.MisconfigFARTemplateName,
						farparams.MisconfigUnsupportedAgent,
						ipmiSharedParams(nil),
						ipmiNodeParams("placeholder-node"))

					err := APIClient.Create(ctx, farTemplateCR)
					Expect(err).To(MatchError(ContainSubstring(farparams.UnsupportedAgentMsg)),
						"FARTemplate with unsupported fence agent should be rejected by webhook")
				})

			It("should reject FARTemplate with agent name missing fence_ prefix",
				reportxml.ID("71220"),
				func() {
					By("Creating FARTemplate with agent name missing fence_ prefix (incorrect_fence)")

					farTemplateCR := buildFARTemplateUnstructured(
						farparams.MisconfigFARTemplateName,
						farparams.MisconfigInvalidPrefixAgent,
						ipmiSharedParams(nil),
						ipmiNodeParams("placeholder-node"))

					err := APIClient.Create(ctx, farTemplateCR)
					Expect(err).To(MatchError(ContainSubstring(farparams.InvalidAgentPatternFARTemplateMsg)),
						"FARTemplate with invalid agent prefix should be rejected by CRD validation")
				})
		})
	})

// cleanupFARCR safely deletes a FenceAgentsRemediation CR by name.
func cleanupFARCR(name string) {
	GinkgoHelper()

	helpers.DeleteRemediationCR(
		context.Background(), APIClient, farGVK, name, medik8sparams.OperatorNs,
		farparams.DefaultPollInterval, farparams.RemediationCRDeletionTimeout,
		GinkgoWriter.Printf)
}

// cleanupFARTemplateCR safely deletes a FenceAgentsRemediationTemplate CR by name.
func cleanupFARTemplateCR(name string) {
	GinkgoHelper()

	helpers.DeleteRemediationCR(
		context.Background(), APIClient, farTemplateGVK, name, medik8sparams.OperatorNs,
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

// ipmiSharedParams returns default IPMI shared parameters with optional overrides.
func ipmiSharedParams(overrides map[string]interface{}) map[string]interface{} {
	params := map[string]interface{}{
		"--action":   "reboot",
		"--ip":       "192.168.123.1",
		"--lanplus":  "",
		"--password": "password",
		"--username": "admin",
	}

	for k, v := range overrides {
		params[k] = v
	}

	return params
}

// ipmiNodeParams returns default IPMI node parameters for the given node name.
func ipmiNodeParams(nodeName string) map[string]interface{} {
	return map[string]interface{}{
		farparams.NodeIdentifierIPMI: map[string]interface{}{
			nodeName: "6233",
		},
	}
}
