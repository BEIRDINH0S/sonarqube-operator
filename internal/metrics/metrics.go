package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// InstanceReady is 1 when the SonarQubeInstance is in Ready phase, 0 otherwise.
	// Labels: namespace, name.
	InstanceReady = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "sonarqube_instance_ready",
			Help: "1 if the SonarQubeInstance is in Ready phase, 0 otherwise.",
		},
		[]string{"namespace", "name"},
	)

	// PluginsInstalled tracks how many plugins are installed on each instance.
	// Labels: namespace, instance.
	PluginsInstalled = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "sonarqube_plugins_installed",
			Help: "Number of plugins currently installed on a SonarQubeInstance.",
		},
		[]string{"namespace", "instance"},
	)
)

func init() {
	ctrlmetrics.Registry.MustRegister(InstanceReady, PluginsInstalled)
}
