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

	// ReconcileTotal counts reconciliation iterations per controller.
	// Labels: controller.
	ReconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sonarqube_operator_reconcile_total",
			Help: "Total number of reconciliations per controller.",
		},
		[]string{"controller"},
	)

	// ReconcileErrors counts failed reconciliations per controller.
	// Labels: controller.
	ReconcileErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sonarqube_operator_reconcile_errors_total",
			Help: "Total number of failed reconciliations per controller.",
		},
		[]string{"controller"},
	)

	// ReconcileDuration tracks reconciliation duration per controller.
	// Labels: controller.
	ReconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "sonarqube_operator_reconcile_duration_seconds",
			Help:    "Duration of reconcile loops in seconds per controller.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"controller"},
	)
)

func init() {
	ctrlmetrics.Registry.MustRegister(
		InstanceReady,
		PluginsInstalled,
		ReconcileTotal,
		ReconcileErrors,
		ReconcileDuration,
	)
}
