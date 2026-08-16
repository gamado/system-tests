package tests

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"

	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/deployment"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/reportxml"

	"github.com/medik8s/system-tests/tests/far-operator/internal/farparams"
	"github.com/medik8s/system-tests/tests/far-operator/internal/farutils"
	"github.com/medik8s/system-tests/tests/internal/helpers"
	"github.com/medik8s/system-tests/tests/internal/labels"
	. "github.com/medik8s/system-tests/tests/internal/medik8sinittools"
	"github.com/medik8s/system-tests/tests/internal/medik8sparams"
)

var farGVK = schema.GroupVersionKind{
	Group:   "fence-agents-remediation.medik8s.io",
	Version: "v1alpha1",
	Kind:    "FenceAgentsRemediation",
}

var farTemplateGVK = schema.GroupVersionKind{
	Group:   "fence-agents-remediation.medik8s.io",
	Version: "v1alpha1",
	Kind:    "FenceAgentsRemediationTemplate",
}

var _ = Describe("FAR Destructive Tests",
	Serial,
	Label(labels.OperatorFAR, farparams.Label,
		labels.DisruptionDestructive,
		labels.PlatformAWS, labels.FrequencyWeekly),
	func() {
		var (
			ctx             context.Context
			platform        configv1.PlatformType
			region          string
			fenceAgent      string
			leaderNode      string
			targetNode      *corev1.Node
			sharedParams    map[string]interface{}
			nodeParams      map[string]interface{}
			currentFARTemplateName string
			currentFARName  string

			destructiveSetupDone    bool
			destructiveSetupSkipped bool
		)

		BeforeEach(func() {
			if destructiveSetupSkipped {
				Skip("FAR destructive tests require AWS")
			}

			if destructiveSetupDone {
				return
			}

			ctx = context.Background()

			By("Detecting cluster platform")

			var err error

			platform, region, err = helpers.DetectPlatform(ctx, APIClient)
			Expect(err).ToNot(HaveOccurred())

			if platform != configv1.AWSPlatformType {
				destructiveSetupSkipped = true

				Skip(fmt.Sprintf(
					"FAR destructive tests require AWS, got %s", platform))
			}

			By("Resolving fence agent for platform")

			fenceAgent, _, err = farutils.FenceAgentForPlatform(platform)
			Expect(err).ToNot(HaveOccurred())
			GinkgoWriter.Printf(
				"Platform: %s, Agent: %s, Region: %s\n",
				platform, fenceAgent, region)

			By("Verifying FAR operator deployment is ready")

			farDeployment, err := deployment.Pull(
				APIClient, farparams.OperatorDeploymentName, medik8sparams.OperatorNs)
			Expect(err).ToNot(HaveOccurred(), "Failed to get FAR deployment")
			Expect(farDeployment.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(),
				"FAR deployment is not Ready")

			By("Verifying at least 3 Ready worker nodes")

			workerCount, err := helpers.CountReadyWorkerNodes(ctx, APIClient)
			Expect(err).ToNot(HaveOccurred())
			// 3 workers: FAR leader (excluded from fencing) + target (fenced/rebooted) +
			// at least 1 spare to keep the cluster schedulable while the target is down.
			Expect(workerCount).To(
				BeNumerically(">=", 3),
				"Destructive tests require at least 3 Ready worker nodes")

			By("Reading AWS credentials from CCO Secret")

			awsAccessKey, awsSecretKey, err := farutils.GetAWSCredentials(
				ctx, APIClient, medik8sparams.OperatorNs)
			Expect(err).ToNot(HaveOccurred(),
				"AWS credentials must be provisioned by the "+
					"medik8s-aws-credentials CI step")

			By("Creating shared credentials Secret for FAR SharedSecretName")

			credentialsSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      farparams.SharedCredentialsSecretName,
					Namespace: medik8sparams.OperatorNs,
				},
				StringData: map[string]string{
					"--access-key": awsAccessKey,
					"--secret-key": awsSecretKey,
				},
			}

			err = APIClient.Create(ctx, credentialsSecret)
			if err != nil && !k8serrors.IsAlreadyExists(err) {
				Expect(err).ToNot(HaveOccurred(),
					"Failed to create shared credentials Secret")
			}

			By("Building fence_aws shared parameters")

			sharedParams = map[string]interface{}{
				"--region":          region,
				"--action":          "reboot",
				"--skip-race-check": "",
			}

			By("Building node parameters (--plug = EC2 instance ID)")

			awsNodeParams, err := farutils.BuildAWSNodeParameters(
				ctx, APIClient)
			Expect(err).ToNot(HaveOccurred())

			nodeParams = make(map[string]interface{})

			for paramName, nodeMap := range awsNodeParams {
				inner := make(map[string]interface{}, len(nodeMap))
				for nodeName, val := range nodeMap {
					inner[nodeName] = val
				}

				nodeParams[paramName] = inner
			}

			By("Identifying active FAR controller node")

			Eventually(func() error {
				var leaderErr error

				leaderNode, leaderErr = farutils.GetActiveFARControllerNode(ctx, APIClient)

				return leaderErr
			}, farparams.ControllerHandoverTimeout, farparams.DefaultPollInterval).Should(Succeed(),
				"FAR leader election did not settle")
			GinkgoWriter.Printf("FAR leader is on node: %s\n", leaderNode)

			destructiveSetupDone = true
		})

		JustAfterEach(func() {
			spec := CurrentSpecReport()
			if spec.Failed() {
				GinkgoWriter.Println(
					"Test failed - collecting diagnostics")
				logFARControllerState(ctx, APIClient)
			}

			if currentFARName != "" {
				By("Waiting for FAR CR to reach Succeeded before cleanup")

				pollCtx, pollCancel := context.WithTimeout(ctx, farparams.FARConditionTimeout)
				defer pollCancel()

				if waitErr := wait.PollUntilContextCancel(pollCtx, farparams.DefaultPollInterval, true,
					func(ctx context.Context) (bool, error) {
						farObj := &unstructured.Unstructured{}
						farObj.SetGroupVersionKind(farGVK)

						if err := APIClient.Get(ctx, client.ObjectKey{
							Name:      currentFARName,
							Namespace: medik8sparams.OperatorNs,
						}, farObj); err != nil {
							return false, nil
						}

						conditions, found, condErr := unstructured.NestedSlice(
							farObj.Object, "status", "conditions")
						if condErr != nil {
							GinkgoWriter.Printf(
								"WARNING: failed to read FAR CR conditions: %v\n", condErr)

							return false, nil
						}

						if !found {
							return false, nil
						}

						for _, c := range conditions {
							condMap, ok := c.(map[string]interface{})
							if !ok {
								continue
							}

							if condMap["type"] == farparams.FARConditionSucceeded &&
								condMap["status"] == string(metav1.ConditionTrue) {
								return true, nil
							}
						}

						return false, nil
					},
				); waitErr != nil {
					GinkgoWriter.Printf(
						"WARNING: FAR CR %s did not reach Succeeded within %s: %v\n",
						currentFARName, farparams.FARConditionTimeout, waitErr)
				}

				By("Deleting FAR CR " + currentFARName)
				farNodeName := currentFARName
				deleteRemediationCR(ctx, APIClient, farGVK, currentFARName)
				currentFARName = ""

				By("Verifying FAR NoSchedule taint removed after CR deletion")

				taintCtx, taintCancel := context.WithTimeout(ctx, farparams.FARConditionTimeout)
				defer taintCancel()

				if taintErr := wait.PollUntilContextCancel(taintCtx, farparams.DefaultPollInterval, true,
					func(ctx context.Context) (bool, error) {
						node := &corev1.Node{}
						if err := APIClient.Get(ctx, client.ObjectKey{Name: farNodeName}, node); err != nil {
							return false, nil
						}

						for _, taint := range node.Spec.Taints {
							if taint.Key == farparams.FARNoScheduleTaintKey {
								return false, nil
							}
						}

						return true, nil
					},
				); taintErr != nil {
					GinkgoWriter.Printf(
						"WARNING: FAR taint %s still present on node %s after %s: %v\n",
						farparams.FARNoScheduleTaintKey, farNodeName,
						farparams.FARConditionTimeout, taintErr)
				}
			}

			if currentFARTemplateName != "" {
				By("Safety net: deleting FARTemplate " + currentFARTemplateName)
				deleteRemediationCR(ctx, APIClient, farTemplateGVK, currentFARTemplateName)
				currentFARTemplateName = ""
			}

			if targetNode != nil {
				nodeName := targetNode.Name
				targetNode = nil

				By("Safety net: waiting for node " + nodeName + " to become Ready")

				if err := farutils.WaitForNodeReady(
					ctx, APIClient, nodeName,
					farparams.NodeReadyTimeout, GinkgoWriter.Printf); err != nil {
					GinkgoWriter.Printf(
						"WARNING: safety net: node %s did not become Ready within %s: %v\n",
						nodeName, farparams.NodeReadyTimeout, err)
					AddReportEntry("safety-net-recovery-failed",
						fmt.Sprintf("node %s did not recover: %v", nodeName, err))
				}
			}
		})

		Context("Standalone FAR remediation", func() {
			BeforeEach(func() {
				By("Verifying FAR controller is Ready before test")

				farDeployment, err := deployment.Pull(
					APIClient, farparams.OperatorDeploymentName, medik8sparams.OperatorNs)
				Expect(err).ToNot(HaveOccurred())
				Expect(farDeployment.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(),
					"FAR controller is not Ready - webhook will be unreachable")

				By("Finding active leader node")

				Eventually(func() error {
					var leaderErr error

					leaderNode, leaderErr = farutils.GetActiveFARControllerNode(ctx, APIClient)

					return leaderErr
				}, farparams.ControllerHandoverTimeout, farparams.DefaultPollInterval).Should(Succeed(),
					"FAR leader election did not settle - lease may point to a replaced pod")
				GinkgoWriter.Printf("FAR controller Ready, leader on node: %s\n", leaderNode)
			})

			Context("non-leader worker target", func() {
				var (
					oldBootID   string
					workloadPod *corev1.Pod
				)

				BeforeEach(func() {
					By("Selecting a non-leader worker node")

					var err error

					targetNode, err = helpers.SelectWorkerNode(ctx, APIClient, leaderNode)
					Expect(err).ToNot(HaveOccurred())

					By("Cleaning CRI-O overlay storage on " + targetNode.Name)
					removeWorkloadImage(ctx, targetNode.Name)

					By("Recording boot ID before remediation")

					oldBootID, err = farutils.GetNodeBootIDFromAPI(ctx, APIClient, targetNode.Name)
					Expect(err).ToNot(HaveOccurred())

					By("Creating a test workload pod pinned to " + targetNode.Name)

					workloadPod = &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							GenerateName: "far-workload-test-",
							Namespace:    medik8sparams.OperatorNs,
						},
						Spec: corev1.PodSpec{
							NodeName:      targetNode.Name,
							RestartPolicy: corev1.RestartPolicyAlways,
							Containers: []corev1.Container{{
								Name:    "workload",
								Image:   farparams.WorkloadTestImage,
								Command: []string{"sleep", "infinity"},
							}},
						},
					}

					Expect(APIClient.Create(ctx, workloadPod)).To(Succeed())
					DeferCleanup(func() {
						_ = APIClient.Delete(ctx, workloadPod)
					})

					By("Waiting for workload pod to be Running")

					Eventually(func() corev1.PodPhase {
						pod := &corev1.Pod{}
						if err := APIClient.Get(ctx, client.ObjectKey{
							Name: workloadPod.Name, Namespace: workloadPod.Namespace,
						}, pod); err != nil {
							return corev1.PodPending
						}

						return pod.Status.Phase
					}, farparams.WorkloadPodReadyTimeout, farparams.DefaultPollInterval).Should(Equal(corev1.PodRunning))
				})

				JustAfterEach(func() {
					if CurrentSpecReport().Failed() {
						logPodDiagnostics(ctx, APIClient, workloadPod)

						return
					}

					By("Verifying workload pod was deleted or evicted")

					Eventually(func() bool {
						pod := &corev1.Pod{}
						err := APIClient.Get(ctx, client.ObjectKey{
							Name: workloadPod.Name, Namespace: workloadPod.Namespace,
						}, pod)

						return k8serrors.IsNotFound(err) || pod.DeletionTimestamp != nil
					}, farparams.WorkloadEvictionTimeout, farparams.DefaultPollInterval).Should(BeTrue(),
						"Workload pod was not deleted/evicted after remediation")
				})

				It("should remediate a worker node via standalone FAR CR",
					Label(labels.TierAcceptance, labels.ComponentRemediation),
					reportxml.ID("61229"),
					func() {
						creationTimestamp := targetNode.CreationTimestamp

						By("Creating FAR CR targeting " + targetNode.Name)

						farCR := buildFARUnstructured(targetNode.Name, fenceAgent, sharedParams, nodeParams)
						createFARCR(ctx, APIClient, farCR)

						currentFARName = targetNode.Name

						waitForRemediation(ctx, APIClient, targetNode.Name, oldBootID)

						By("Verifying node was rebooted, not re-created")

						node := &corev1.Node{}
						Expect(APIClient.Get(ctx, client.ObjectKey{Name: targetNode.Name}, node)).To(Succeed())
						Expect(node.CreationTimestamp.Equal(&creationTimestamp)).To(BeTrue(),
							"Node creation timestamp changed - node was re-created instead of rebooted")

						By("Verifying FAR lifecycle events on CR")

						Expect(helpers.WaitForEvents(ctx, APIClient.K8sClient,
							helpers.InvolvedObjectRef{
								Kind:      "FenceAgentsRemediation",
								Name:      targetNode.Name,
								Namespace: medik8sparams.OperatorNs,
								UID:       string(farCR.GetUID()),
							},
							[]helpers.EventExpectation{
								{Reason: farparams.FAREventRemediationStarted, Type: corev1.EventTypeNormal},
								{Reason: farparams.FAREventFenceAgentSucceeded, Type: corev1.EventTypeNormal},
								{Reason: farparams.FAREventRemediationFinished, Type: corev1.EventTypeNormal},
							},
							farparams.FARConditionTimeout, farparams.DefaultPollInterval,
						)).To(Succeed(), "FAR lifecycle events not found on CR %s", targetNode.Name)

						By("Verifying remediation completion event on Node")

						Expect(helpers.WaitForEvents(ctx, APIClient.K8sClient,
							helpers.InvolvedObjectRef{
								Kind: "Node",
								Name: targetNode.Name,
								UID:  string(node.GetUID()),
							},
							[]helpers.EventExpectation{
								{Reason: farparams.FAREventNodeRemediationCompleted, Type: corev1.EventTypeNormal},
							},
							farparams.FARConditionTimeout, farparams.DefaultPollInterval,
						)).To(Succeed(), "NodeRemediationCompleted event not found on node %s", targetNode.Name)
					})

				It("should apply FAR NoSchedule taint during remediation",
					Label(labels.TierAcceptance, labels.ComponentRemediation),
					reportxml.ID("65960"),
					func() {
						By("Creating FAR CR targeting " + targetNode.Name)

						farCR := buildFARUnstructured(targetNode.Name, fenceAgent, sharedParams, nodeParams)
						createFARCR(ctx, APIClient, farCR)

						currentFARName = targetNode.Name

						By("Verifying FAR NoSchedule taint is applied to the node")

						Eventually(func(assertion Gomega) {
							node := &corev1.Node{}
							assertion.Expect(APIClient.Get(ctx, client.ObjectKey{Name: targetNode.Name}, node)).To(Succeed())

							found := false

							for _, taint := range node.Spec.Taints {
								if taint.Key == farparams.FARNoScheduleTaintKey &&
									taint.Effect == corev1.TaintEffectNoSchedule {
									found = true

									break
								}
							}

							assertion.Expect(found).To(BeTrue(),
								"FAR NoSchedule taint %s not found on node %s",
								farparams.FARNoScheduleTaintKey, targetNode.Name)
						}, farparams.FARConditionTimeout, farparams.DefaultPollInterval).Should(Succeed())

						waitForRemediation(ctx, APIClient, targetNode.Name, oldBootID)
					})

				It("should report correct FAR CR status conditions after remediation",
					Label(labels.TierAcceptance, labels.ComponentRemediation),
					reportxml.ID("67015"),
					func() {
						By("Creating FAR CR targeting " + targetNode.Name)

						farCR := buildFARUnstructured(targetNode.Name, fenceAgent, sharedParams, nodeParams)
						createFARCR(ctx, APIClient, farCR)

						currentFARName = targetNode.Name

						waitForRemediation(ctx, APIClient, targetNode.Name, oldBootID)

						By("Verifying FAR CR status conditions")

						expectedConditions := map[string]string{
							farparams.FARConditionProcessing:          string(metav1.ConditionFalse),
							farparams.FARConditionFenceAgentSucceeded: string(metav1.ConditionTrue),
							farparams.FARConditionSucceeded:           string(metav1.ConditionTrue),
						}

						Eventually(func(assertion Gomega) {
							farObj := &unstructured.Unstructured{}
							farObj.SetGroupVersionKind(farGVK)
							assertion.Expect(APIClient.Get(ctx, client.ObjectKey{
								Name: targetNode.Name, Namespace: medik8sparams.OperatorNs,
							}, farObj)).To(Succeed())

							conditions, found, condErr := unstructured.NestedSlice(
								farObj.Object, "status", "conditions")
							assertion.Expect(condErr).ToNot(HaveOccurred())
							assertion.Expect(found).To(BeTrue(), "FAR CR has no status.conditions")

							for condType, expectedStatus := range expectedConditions {
								condFound := false

								for _, c := range conditions {
									condMap, ok := c.(map[string]interface{})
									if !ok {
										continue
									}

									if condMap["type"] == condType {
										condFound = true

										assertion.Expect(condMap["status"]).To(Equal(expectedStatus),
											"Condition %s has unexpected status", condType)

										break
									}
								}

								assertion.Expect(condFound).To(BeTrue(),
									"Condition %s not found in FAR CR status", condType)
							}
						}, farparams.FARConditionTimeout, farparams.DefaultPollInterval).Should(Succeed())
					})

				Context("without --action parameter", func() {
					It("should default to reboot action when --action is omitted",
						Label(labels.TierAcceptance, labels.ComponentRemediation),
						reportxml.ID("66203"),
						func() {
							By("Building shared parameters WITHOUT --action")

							noActionParams := make(map[string]interface{}, len(sharedParams))
							for k, v := range sharedParams {
								if k != "--action" {
									noActionParams[k] = v
								}
							}

							By("Creating FAR CR without explicit action")

							farCR := buildFARUnstructured(targetNode.Name, fenceAgent, noActionParams, nodeParams)
							createFARCR(ctx, APIClient, farCR)

							currentFARName = targetNode.Name

							waitForRemediation(ctx, APIClient, targetNode.Name, oldBootID)
						})
				})
			})

			Context("leader node target", func() {
				It("should remediate the node hosting the active FAR controller",
					Label(labels.TierAcceptance, labels.ComponentRemediation),
					reportxml.ID("70638"),
					func() {
						By("Targeting the active FAR controller node")

						var err error

						activeLeader, err := farutils.GetActiveFARControllerNode(ctx, APIClient)
						Expect(err).ToNot(HaveOccurred())
						Expect(activeLeader).ToNot(BeEmpty())

						node := &corev1.Node{}
						Expect(APIClient.Get(ctx, client.ObjectKey{Name: activeLeader}, node)).To(Succeed())
						targetNode = node

						By("Cleaning CRI-O overlay storage on " + targetNode.Name)
						removeWorkloadImage(ctx, targetNode.Name)

						By("Recording boot ID before remediation")

						oldBootID, err := farutils.GetNodeBootIDFromAPI(ctx, APIClient, targetNode.Name)
						Expect(err).ToNot(HaveOccurred())

						By("Creating a test workload pod pinned to " + targetNode.Name)

						workloadPod := &corev1.Pod{
							ObjectMeta: metav1.ObjectMeta{
								GenerateName: "far-workload-test-",
								Namespace:    medik8sparams.OperatorNs,
							},
							Spec: corev1.PodSpec{
								NodeName:      targetNode.Name,
								RestartPolicy: corev1.RestartPolicyAlways,
								Containers: []corev1.Container{{
									Name:    "workload",
									Image:   farparams.WorkloadTestImage,
									Command: []string{"sleep", "infinity"},
								}},
							},
						}

						Expect(APIClient.Create(ctx, workloadPod)).To(Succeed())
						DeferCleanup(func() {
							_ = APIClient.Delete(ctx, workloadPod)
						})

						By("Waiting for workload pod to be Running")

						Eventually(func() corev1.PodPhase {
							pod := &corev1.Pod{}
							if err := APIClient.Get(ctx, client.ObjectKey{
								Name: workloadPod.Name, Namespace: workloadPod.Namespace,
							}, pod); err != nil {
								return corev1.PodPending
							}

							return pod.Status.Phase
						}, farparams.WorkloadPodReadyTimeout, farparams.DefaultPollInterval).Should(Equal(corev1.PodRunning))

						By("Recording pre-reboot lease holder for failover verification")

						preRebootLease := &coordinationv1.Lease{}
						Expect(APIClient.Get(ctx, client.ObjectKey{
							Name:      farparams.ControllerLeaseName,
							Namespace: medik8sparams.OperatorNs,
						}, preRebootLease)).To(Succeed())
						Expect(preRebootLease.Spec.HolderIdentity).ToNot(BeNil(),
							"Lease has no holder before reboot")
						oldLeaderHolder := *preRebootLease.Spec.HolderIdentity
						GinkgoWriter.Printf("Pre-reboot lease holder: %s\n", oldLeaderHolder)

						By("Creating FAR CR targeting the active controller node " + targetNode.Name)

						farCR := buildFARUnstructured(targetNode.Name, fenceAgent, sharedParams, nodeParams)
						createFARCR(ctx, APIClient, farCR)

						currentFARName = targetNode.Name

						waitForRemediation(ctx, APIClient, targetNode.Name, oldBootID)

						By("Verifying FAR controller replicas recovered")

						farDeployment, err := deployment.Pull(
							APIClient, farparams.OperatorDeploymentName, medik8sparams.OperatorNs)
						Expect(err).ToNot(HaveOccurred())
						Expect(farDeployment.IsReady(medik8sparams.DefaultTimeout)).To(BeTrue(),
							"FAR controller replicas did not recover after leader node reboot")

						By("Verifying controller lease transferred to a different pod")

						Eventually(func(assertion Gomega) {
							lease := &coordinationv1.Lease{}
							assertion.Expect(APIClient.Get(ctx, client.ObjectKey{
								Name:      farparams.ControllerLeaseName,
								Namespace: medik8sparams.OperatorNs,
							}, lease)).To(Succeed())
							assertion.Expect(lease.Spec.HolderIdentity).ToNot(BeNil(),
								"Lease has no holder after leader node reboot")

							if lease.Spec.HolderIdentity != nil {
								assertion.Expect(*lease.Spec.HolderIdentity).ToNot(Equal(oldLeaderHolder),
									"Lease is still held by pre-reboot pod %s", oldLeaderHolder)
							}
						}, farparams.ControllerHandoverTimeout, farparams.DefaultPollInterval).Should(Succeed(),
							"Controller lease did not transfer to a different pod after leader node reboot")

						By("Verifying FAR lifecycle events survived leader failover")

						node = &corev1.Node{}
						Expect(APIClient.Get(ctx, client.ObjectKey{Name: targetNode.Name}, node)).To(Succeed())

						Expect(helpers.WaitForEvents(ctx, APIClient.K8sClient,
							helpers.InvolvedObjectRef{
								Kind:      "FenceAgentsRemediation",
								Name:      targetNode.Name,
								Namespace: medik8sparams.OperatorNs,
								UID:       string(farCR.GetUID()),
							},
							[]helpers.EventExpectation{
								{Reason: farparams.FAREventRemediationStarted, Type: corev1.EventTypeNormal},
								{Reason: farparams.FAREventFenceAgentSucceeded, Type: corev1.EventTypeNormal},
								{Reason: farparams.FAREventRemediationFinished, Type: corev1.EventTypeNormal},
							},
							farparams.FARConditionTimeout, farparams.DefaultPollInterval,
						)).To(Succeed(), "FAR lifecycle events not found on CR after leader failover")

						By("Verifying remediation completion event on Node after failover")

						Expect(helpers.WaitForEvents(ctx, APIClient.K8sClient,
							helpers.InvolvedObjectRef{
								Kind: "Node",
								Name: targetNode.Name,
								UID:  string(node.GetUID()),
							},
							[]helpers.EventExpectation{
								{Reason: farparams.FAREventNodeRemediationCompleted, Type: corev1.EventTypeNormal},
							},
							farparams.FARConditionTimeout, farparams.DefaultPollInterval,
						)).To(Succeed(), "NodeRemediationCompleted event not found on node after leader failover")

						By("Verifying workload pod was evicted from leader node")

						Eventually(func() bool {
							pod := &corev1.Pod{}
							err := APIClient.Get(ctx, client.ObjectKey{
								Name: workloadPod.Name, Namespace: workloadPod.Namespace,
							}, pod)

							return k8serrors.IsNotFound(err) || pod.DeletionTimestamp != nil
						}, farparams.WorkloadEvictionTimeout, farparams.DefaultPollInterval).Should(BeTrue(),
							"Workload pod was not evicted from leader node after remediation")
					})
			})
		})

		Context("NHC+FAR interop", func() {
			// RHWA-1035: 4 NHC+FAR interop tests will be added here.
			// These tests install both NHC and FAR, configure NHC to use
			// FAR as the remediator, then trigger remediation via NHC by
			// stopping kubelet and waiting for NHC to detect the unhealthy
			// node and create a FAR CR automatically.
		})
	})

func buildFARUnstructured(
	nodeName, agent string,
	sharedParams, nodeParams map[string]interface{},
) *unstructured.Unstructured {
	spec := map[string]interface{}{
		"agent":               agent,
		"sharedparameters":    sharedParams,
		"nodeparameters":      nodeParams,
		"retrycount":          farparams.FARCRRetryCount,
		"retryinterval":       farparams.FARCRRetryInterval,
		"timeout":             farparams.FARCRTimeout,
		"remediationStrategy": farparams.FARCRRemediationStrategy,
		"sharedSecretName":    farparams.SharedCredentialsSecretName,
	}

	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "fence-agents-remediation.medik8s.io/v1alpha1",
			"kind":       "FenceAgentsRemediation",
			"metadata": map[string]interface{}{
				"name":      nodeName,
				"namespace": medik8sparams.OperatorNs,
			},
			"spec": spec,
		},
	}
}

//nolint:unused // scaffold helper for upcoming destructive test specs
func buildFARTemplateUnstructured(
	name, agent string,
	sharedParams, nodeParams map[string]interface{},
) *unstructured.Unstructured {
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
						"agent":               agent,
						"sharedparameters":    sharedParams,
						"nodeparameters":      nodeParams,
						"retrycount":          farparams.FARCRRetryCount,
						"retryinterval":       farparams.FARCRRetryInterval,
						"timeout":             farparams.FARCRTimeout,
						"remediationStrategy": farparams.FARCRRemediationStrategy,
						"sharedSecretName":    farparams.SharedCredentialsSecretName,
					},
				},
			},
		},
	}
}

func waitForRemediation(
	ctx context.Context, k8sClient client.Client,
	nodeName, oldBootID string,
) {
	By("Waiting for node to reboot")

	Expect(farutils.WaitForNodeReboot(
		ctx, k8sClient, nodeName, oldBootID,
		farparams.NodeRebootTimeout, GinkgoWriter.Printf)).To(Succeed(),
		"Node %s did not reboot", nodeName)

	By("Waiting for node to become Ready")

	Expect(farutils.WaitForNodeReady(
		ctx, k8sClient, nodeName,
		farparams.NodeReadyTimeout, GinkgoWriter.Printf)).To(Succeed(),
		"Node %s did not become Ready after reboot", nodeName)
}

func createFARCR(
	ctx context.Context, k8sClient client.Client,
	farCR *unstructured.Unstructured,
) {
	deleteRemediationCR(ctx, k8sClient, farCR.GroupVersionKind(),
		farCR.GetName())

	Eventually(func(assertion Gomega) {
		err := k8sClient.Create(ctx, farCR)
		if err != nil {
			if k8serrors.IsAlreadyExists(err) {
				GinkgoWriter.Printf(
					"INFO: FAR CR %s already exists (prior delete may not have finalized), treating as success\n",
					farCR.GetName())

				return
			}

			assertion.Expect(err).ToNot(HaveOccurred(),
				"Failed to create FAR CR")
		}
	}, farparams.FARConditionTimeout, 10*farparams.DefaultPollInterval).Should(Succeed(),
		"FAR CR creation timed out - webhook may be unreachable")
}

func logFARControllerState(ctx context.Context, k8sClient client.Client) {
	pods := &corev1.PodList{}

	if err := k8sClient.List(ctx, pods,
		client.InNamespace(medik8sparams.OperatorNs),
		client.MatchingLabels(farparams.OperatorControllerPodLabels)); err != nil {
		GinkgoWriter.Printf("WARNING: could not list controller pods: %v\n", err)

		return
	}

	for i := range pods.Items {
		pod := &pods.Items[i]
		ready := false

		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
				ready = true

				break
			}
		}

		GinkgoWriter.Printf("FAR controller pod %s: Phase=%s, Node=%s, Ready=%v\n",
			pod.Name, pod.Status.Phase, pod.Spec.NodeName, ready)
	}
}

func logPodDiagnostics(ctx context.Context, k8sClient client.Client, pod *corev1.Pod) {
	if pod == nil || pod.Name == "" {
		return
	}

	fresh := &corev1.Pod{}

	if err := k8sClient.Get(ctx, client.ObjectKey{
		Name: pod.Name, Namespace: pod.Namespace,
	}, fresh); err != nil {
		GinkgoWriter.Printf("WARNING: could not fetch pod %s for diagnostics: %v\n",
			pod.Name, err)

		return
	}

	GinkgoWriter.Printf("Pod %s diagnostics: Phase=%s, Node=%s\n",
		fresh.Name, fresh.Status.Phase, fresh.Spec.NodeName)

	for _, cond := range fresh.Status.Conditions {
		if cond.Status != corev1.ConditionTrue {
			GinkgoWriter.Printf("  Condition %s=%s: %s (%s)\n",
				cond.Type, cond.Status, cond.Reason, cond.Message)
		}
	}

	for _, ctrStatus := range fresh.Status.ContainerStatuses {
		GinkgoWriter.Printf("  Container %s: Ready=%v, RestartCount=%d\n",
			ctrStatus.Name, ctrStatus.Ready, ctrStatus.RestartCount)

		if ctrStatus.State.Waiting != nil {
			GinkgoWriter.Printf("    Waiting: %s - %s\n",
				ctrStatus.State.Waiting.Reason, ctrStatus.State.Waiting.Message)
		}

		if ctrStatus.State.Terminated != nil {
			GinkgoWriter.Printf("    Terminated: %s (exit %d) - %s\n",
				ctrStatus.State.Terminated.Reason,
				ctrStatus.State.Terminated.ExitCode,
				ctrStatus.State.Terminated.Message)
		}
	}

	eventList := &corev1.EventList{}

	if err := k8sClient.List(ctx, eventList,
		client.InNamespace(pod.Namespace)); err != nil {
		GinkgoWriter.Printf("WARNING: could not list events: %v\n", err)

		return
	}

	GinkgoWriter.Printf("  Events for pod %s:\n", fresh.Name)

	eventFound := false

	for i := range eventList.Items {
		podEvent := &eventList.Items[i]

		if podEvent.InvolvedObject.Name != fresh.Name ||
			podEvent.InvolvedObject.Kind != "Pod" {
			continue
		}

		eventFound = true

		ts := podEvent.LastTimestamp.Format("15:04:05")
		GinkgoWriter.Printf("    [%s] %s %s: %s (x%d)\n",
			ts, podEvent.Type, podEvent.Reason, podEvent.Message, podEvent.Count)
	}

	if !eventFound {
		GinkgoWriter.Println("    (no events found)")
	}
}

func removeWorkloadImage(ctx context.Context, nodeName string) {
	GinkgoWriter.Printf("Removing workload image from node %s to prevent corrupt overlay layers\n", nodeName)

	output, err := helpers.RunOnNode(
		ctx, nodeName, farparams.CrioCleanupTimeout,
		"bash", "-c",
		"crictl rmi "+farparams.WorkloadTestImage+" 2>/dev/null; "+
			"echo done",
	)
	if err != nil {
		GinkgoWriter.Printf(
			"WARNING: image removal on node %s failed: %v (output: %s)\n",
			nodeName, err, output)

		return
	}

	GinkgoWriter.Printf("Workload image removed from node %s (output: %s)\n",
		nodeName, output)
}

func deleteRemediationCR(
	ctx context.Context, k8sClient client.Client,
	gvk schema.GroupVersionKind, name string,
) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)

	key := client.ObjectKey{Name: name, Namespace: medik8sparams.OperatorNs}

	if waitErr := wait.PollUntilContextTimeout(
		ctx, farparams.DefaultPollInterval, farparams.RemediationCRDeletionTimeout, true,
		func(ctx context.Context) (bool, error) {
			if err := k8sClient.Get(ctx, key, obj); err != nil {
				if k8serrors.IsNotFound(err) {
					return true, nil
				}

				return false, nil
			}

			if delErr := k8sClient.Delete(ctx, obj); delErr != nil {
				if k8serrors.IsNotFound(delErr) {
					return true, nil
				}

				return false, nil
			}

			return false, nil
		},
	); waitErr != nil {
		GinkgoWriter.Printf(
			"Warning: %s %s not fully deleted within %s: %v\n",
			gvk.Kind, name, farparams.RemediationCRDeletionTimeout, waitErr)
	}
}
