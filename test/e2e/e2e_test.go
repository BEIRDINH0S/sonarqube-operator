//go:build e2e
// +build e2e

/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/BEIRDINH0S/sonarqube-operator/test/utils"
)

// namespace where the operator is deployed
const namespace = "sonarqube-operator-system"

// serviceAccountName created for the project
const serviceAccountName = "sonarqube-operator-controller-manager"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "sonarqube-operator-controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "sonarqube-operator-metrics-binding"

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	// Before running the tests, set up the environment by creating the namespace,
	// enforce the restricted security policy to the namespace, installing CRDs,
	// and deploying the controller.
	BeforeAll(func() {
		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("labeling the namespace to enforce the restricted security policy")
		cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
			"pod-security.kubernetes.io/enforce=restricted")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager")
		cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", managerImage))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")
	})

	// After all tests have been executed, clean up by undeploying the controller, uninstalling CRDs,
	// and deleting the namespace.
	AfterAll(func() {
		By("cleaning up the curl pod for metrics")
		cmd := exec.Command("kubectl", "delete", "pod", "curl-metrics", "-n", namespace, "--ignore-not-found")
		_, _ = utils.Run(cmd)

		By("cleaning up the metrics ClusterRoleBinding")
		cmd = exec.Command("kubectl", "delete", "clusterrolebinding", metricsRoleBindingName, "--ignore-not-found")
		_, _ = utils.Run(cmd)

		By("undeploying the controller-manager")
		cmd = exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace)
		_, _ = utils.Run(cmd)
	})

	// After each test, check for failures and collect logs, events,
	// and pod descriptions for debugging.
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}

			By("Fetching Kubernetes events")
			cmd = exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}

			By("Fetching curl-metrics logs")
			cmd = exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
			metricsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Metrics logs:\n %s", metricsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get curl-metrics logs: %s", err)
			}

			By("Fetching controller manager pod description")
			cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", namespace)
			podDescription, err := utils.Run(cmd)
			if err == nil {
				fmt.Println("Pod description:\n", podDescription)
			} else {
				fmt.Println("Failed to describe controller pod")
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Manager", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				// Get the name of the controller-manager pod
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				// Validate the pod's status
				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status")
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})

		It("should ensure the metrics endpoint is serving metrics", func() {
			By("creating a ClusterRoleBinding for the service account to allow access to metrics")
			cmd := exec.Command("kubectl", "create", "clusterrolebinding", metricsRoleBindingName,
				"--clusterrole=sonarqube-operator-metrics-reader",
				fmt.Sprintf("--serviceaccount=%s:%s", namespace, serviceAccountName),
			)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterRoleBinding")

			By("validating that the metrics service is available")
			cmd = exec.Command("kubectl", "get", "service", metricsServiceName, "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Metrics service should exist")

			By("getting the service account token")
			token, err := serviceAccountToken()
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())

			By("ensuring the controller pod is ready")
			verifyControllerPodReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pod", controllerPodName, "-n", namespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"), "Controller pod not ready")
			}
			Eventually(verifyControllerPodReady, 3*time.Minute, time.Second).Should(Succeed())

			By("verifying that the controller manager is serving the metrics server")
			verifyMetricsServerStarted := func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("Serving metrics server"),
					"Metrics server not yet started")
			}
			Eventually(verifyMetricsServerStarted, 3*time.Minute, time.Second).Should(Succeed())

			// +kubebuilder:scaffold:e2e-metrics-webhooks-readiness

			By("creating the curl-metrics pod to access the metrics endpoint")
			cmd = exec.Command("kubectl", "run", "curl-metrics", "--restart=Never",
				"--namespace", namespace,
				"--image=curlimages/curl:latest",
				"--overrides",
				fmt.Sprintf(`{
					"spec": {
						"containers": [{
							"name": "curl",
							"image": "curlimages/curl:latest",
							"command": ["/bin/sh", "-c"],
							"args": [
								"for i in $(seq 1 30); do curl -v -k -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics && exit 0 || sleep 2; done; exit 1"
							],
							"securityContext": {
								"readOnlyRootFilesystem": true,
								"allowPrivilegeEscalation": false,
								"capabilities": {
									"drop": ["ALL"]
								},
								"runAsNonRoot": true,
								"runAsUser": 1000,
								"seccompProfile": {
									"type": "RuntimeDefault"
								}
							}
						}],
						"serviceAccountName": "%s"
					}
				}`, token, metricsServiceName, namespace, serviceAccountName))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create curl-metrics pod")

			By("waiting for the curl-metrics pod to complete.")
			verifyCurlUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "curl-metrics",
					"-o", "jsonpath={.status.phase}",
					"-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Succeeded"), "curl pod in wrong status")
			}
			Eventually(verifyCurlUp, 5*time.Minute).Should(Succeed())

			By("getting the metrics by checking curl-metrics logs")
			verifyMetricsAvailable := func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve logs from curl pod")
				g.Expect(metricsOutput).NotTo(BeEmpty())
				g.Expect(metricsOutput).To(ContainSubstring("< HTTP/1.1 200 OK"))
			}
			Eventually(verifyMetricsAvailable, 2*time.Minute).Should(Succeed())
		})

		// +kubebuilder:scaffold:e2e-webhooks-checks
	})

	Context("SonarQube resources", Ordered, func() {
		const (
			testNS        = "default"
			instanceName  = "e2e-sonarqube"
			adminPassword = "Admin@12345!"
		)

		BeforeAll(func() {
			By("deploying Postgres")
			Expect(kubectlApply(`
apiVersion: v1
kind: Pod
metadata:
  name: postgres
  namespace: default
  labels:
    app: postgres
spec:
  containers:
  - name: postgres
    image: postgres:15
    env:
    - name: POSTGRES_DB
      value: sonarqube
    - name: POSTGRES_USER
      value: sonar
    - name: POSTGRES_PASSWORD
      value: sonar
    ports:
    - containerPort: 5432
---
apiVersion: v1
kind: Service
metadata:
  name: postgres
  namespace: default
spec:
  selector:
    app: postgres
  ports:
  - port: 5432
    targetPort: 5432
`)).To(Succeed())

			By("waiting for Postgres to be Running")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pod", "postgres", "-n", testNS,
					"-o", "jsonpath={.status.phase}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("Running"))
			}, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("creating sonar-db-secret")
			Expect(kubectlCreateSecret(testNS, "sonar-db-secret",
				"username=sonar",
				"password=sonar",
			)).To(Succeed())

			By("creating sonar-admin secret")
			Expect(kubectlCreateSecret(testNS, "sonar-admin",
				"password="+adminPassword,
			)).To(Succeed())

			By("creating SonarQubeInstance")
			Expect(kubectlApply(fmt.Sprintf(`
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeInstance
metadata:
  name: %s
  namespace: %s
spec:
  edition: community
  version: "10.3"
  database:
    host: postgres
    port: 5432
    name: sonarqube
    secretRef: sonar-db-secret
  adminSecretRef: sonar-admin
  resources:
    requests:
      memory: "2Gi"
      cpu: "500m"
    limits:
      memory: "3Gi"
      cpu: "2"
  persistence:
    size: "2Gi"
    extensionsSize: "512Mi"
  jvmOptions: "-Xmx512m -Xms128m"
`, instanceName, testNS))).To(Succeed())
		})

		AfterAll(func() {
			By("deleting SonarQubeProject")
			cmd := exec.Command("kubectl", "delete", "sonarqubeproject", "e2e-project",
				"-n", testNS, "--ignore-not-found", "--timeout=60s")
			_, _ = utils.Run(cmd)

			By("deleting SonarQubeQualityGate")
			cmd = exec.Command("kubectl", "delete", "sonarqubequalitygate", "e2e-gate",
				"-n", testNS, "--ignore-not-found", "--timeout=60s")
			_, _ = utils.Run(cmd)

			By("deleting SonarQubeInstance")
			cmd = exec.Command("kubectl", "delete", "sonarqubeinstance", instanceName,
				"-n", testNS, "--ignore-not-found", "--timeout=60s")
			_, _ = utils.Run(cmd)

			By("deleting secrets")
			cmd = exec.Command("kubectl", "delete", "secret",
				"sonar-admin", "sonar-db-secret",
				"-n", testNS, "--ignore-not-found")
			_, _ = utils.Run(cmd)

			By("deleting Postgres")
			cmd = exec.Command("kubectl", "delete", "pod", "postgres",
				"-n", testNS, "--ignore-not-found", "--timeout=30s")
			_, _ = utils.Run(cmd)
			cmd = exec.Command("kubectl", "delete", "service", "postgres",
				"-n", testNS, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("SonarQubeInstance reaches Ready", func() {
			// SonarQube takes several minutes to start: ES bootstrap + DB migrations
			By("waiting for SonarQubeInstance to be Ready (up to 12 min)")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "sonarqubeinstance", instanceName,
					"-n", testNS, "-o", "jsonpath={.status.phase}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("Ready"))
			}, 12*time.Minute, 15*time.Second).Should(Succeed())

			By("checking status.url is set")
			cmd := exec.Command("kubectl", "get", "sonarqubeinstance", instanceName,
				"-n", testNS, "-o", "jsonpath={.status.url}")
			url, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(url).NotTo(BeEmpty(), "status.url should be set")

			By("checking status.version is set")
			cmd = exec.Command("kubectl", "get", "sonarqubeinstance", instanceName,
				"-n", testNS, "-o", "jsonpath={.status.version}")
			version, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(version).NotTo(BeEmpty(), "status.version should be set")

			By("checking Ready condition is True")
			cmd = exec.Command("kubectl", "get", "sonarqubeinstance", instanceName,
				"-n", testNS,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].status}`)
			condStatus, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(condStatus).To(Equal("True"))

			By("checking AdminInitialized condition is True")
			cmd = exec.Command("kubectl", "get", "sonarqubeinstance", instanceName,
				"-n", testNS,
				"-o", `jsonpath={.status.conditions[?(@.type=="AdminInitialized")].status}`)
			adminCond, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(adminCond).To(Equal("True"))

			By("verifying StatefulSet was created")
			cmd = exec.Command("kubectl", "get", "statefulset", instanceName, "-n", testNS)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "StatefulSet should exist")

			By("verifying Service was created")
			cmd = exec.Command("kubectl", "get", "service", instanceName, "-n", testNS)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Service should exist")
		})

		It("SonarQubeQualityGate reaches Ready", func() {
			By("creating SonarQubeQualityGate")
			Expect(kubectlApply(fmt.Sprintf(`
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeQualityGate
metadata:
  name: e2e-gate
  namespace: %s
spec:
  instanceRef:
    name: %s
  name: e2e-gate
  conditions:
  - metric: coverage
    operator: LT
    value: "80"
  - metric: new_reliability_rating
    operator: GT
    value: "1"
`, testNS, instanceName))).To(Succeed())

			By("waiting for SonarQubeQualityGate to be Ready")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "sonarqubequalitygate", "e2e-gate",
					"-n", testNS, "-o", "jsonpath={.status.phase}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("Ready"))
			}, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("checking Ready condition is True")
			cmd := exec.Command("kubectl", "get", "sonarqubequalitygate", "e2e-gate",
				"-n", testNS,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].status}`)
			condStatus, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(condStatus).To(Equal("True"))
		})

		It("SonarQubeProject reaches Ready", func() {
			By("creating SonarQubeProject")
			Expect(kubectlApply(fmt.Sprintf(`
apiVersion: sonarqube.sonarqube.io/v1alpha1
kind: SonarQubeProject
metadata:
  name: e2e-project
  namespace: %s
spec:
  instanceRef:
    name: %s
  key: e2e-project
  name: E2E Test Project
  visibility: private
  qualityGateRef: e2e-gate
`, testNS, instanceName))).To(Succeed())

			By("waiting for SonarQubeProject to be Ready")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "sonarqubeproject", "e2e-project",
					"-n", testNS, "-o", "jsonpath={.status.phase}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("Ready"))
			}, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("checking status.projectUrl is set")
			cmd := exec.Command("kubectl", "get", "sonarqubeproject", "e2e-project",
				"-n", testNS, "-o", "jsonpath={.status.projectUrl}")
			url, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(url).NotTo(BeEmpty(), "status.projectUrl should be set")

			By("checking Ready condition is True")
			cmd = exec.Command("kubectl", "get", "sonarqubeproject", "e2e-project",
				"-n", testNS,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].status}`)
			condStatus, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(condStatus).To(Equal("True"))
		})

		It("deletion removes resources from SonarQube via finalizers", func() {
			By("deleting SonarQubeProject and waiting for it to be gone")
			cmd := exec.Command("kubectl", "delete", "sonarqubeproject", "e2e-project",
				"-n", testNS, "--timeout=60s")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("deleting SonarQubeQualityGate and waiting for it to be gone")
			cmd = exec.Command("kubectl", "delete", "sonarqubequalitygate", "e2e-gate",
				"-n", testNS, "--timeout=60s")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

// kubectlApply runs kubectl apply -f - with the given YAML as stdin.
func kubectlApply(yaml string) error {
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(yaml)
	_, err := utils.Run(cmd)
	return err
}

// kubectlCreateSecret creates a secret idempotently using --dry-run=client | apply.
func kubectlCreateSecret(ns, name string, literals ...string) error {
	args := []string{"create", "secret", "generic", name, "-n", ns}
	for _, l := range literals {
		args = append(args, "--from-literal="+l)
	}
	args = append(args, "--dry-run=client", "-o", "yaml")
	out, err := utils.Run(exec.Command("kubectl", args...))
	if err != nil {
		return err
	}
	applyCmd := exec.Command("kubectl", "apply", "-f", "-")
	applyCmd.Stdin = strings.NewReader(out)
	_, err = utils.Run(applyCmd)
	return err
}

// serviceAccountToken returns a token for the specified service account in the given namespace.
// It uses the Kubernetes TokenRequest API to generate a token by directly sending a request
// and parsing the resulting token from the API response.
func serviceAccountToken() (string, error) {
	const tokenRequestRawString = `{
		"apiVersion": "authentication.k8s.io/v1",
		"kind": "TokenRequest"
	}`

	// Temporary file to store the token request
	secretName := fmt.Sprintf("%s-token-request", serviceAccountName)
	tokenRequestFile := filepath.Join("/tmp", secretName)
	err := os.WriteFile(tokenRequestFile, []byte(tokenRequestRawString), os.FileMode(0o644))
	if err != nil {
		return "", err
	}

	var out string
	verifyTokenCreation := func(g Gomega) {
		// Execute kubectl command to create the token
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			namespace,
			serviceAccountName,
		), "-f", tokenRequestFile)

		output, err := cmd.CombinedOutput()
		g.Expect(err).NotTo(HaveOccurred())

		// Parse the JSON output to extract the token
		var token tokenRequest
		err = json.Unmarshal(output, &token)
		g.Expect(err).NotTo(HaveOccurred())

		out = token.Status.Token
	}
	Eventually(verifyTokenCreation).Should(Succeed())

	return out, err
}

// getMetricsOutput retrieves and returns the logs from the curl pod used to access the metrics endpoint.
func getMetricsOutput() (string, error) {
	By("getting the curl-metrics logs")
	cmd := exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
	return utils.Run(cmd)
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response,
// containing only the token field that we need to extract.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}
