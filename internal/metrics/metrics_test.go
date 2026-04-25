package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestInstanceReady_SetAndRead(t *testing.T) {
	InstanceReady.WithLabelValues("default", "my-sonarqube").Set(1)
	value := testutil.ToFloat64(InstanceReady.WithLabelValues("default", "my-sonarqube"))
	assert.Equal(t, float64(1), value)

	InstanceReady.WithLabelValues("default", "my-sonarqube").Set(0)
	value = testutil.ToFloat64(InstanceReady.WithLabelValues("default", "my-sonarqube"))
	assert.Equal(t, float64(0), value)
}

func TestPluginsInstalled_SetAndRead(t *testing.T) {
	PluginsInstalled.WithLabelValues("default", "my-sonarqube").Set(5)
	value := testutil.ToFloat64(PluginsInstalled.WithLabelValues("default", "my-sonarqube"))
	assert.Equal(t, float64(5), value)
}

func TestInstanceReady_IndependentLabels(t *testing.T) {
	InstanceReady.WithLabelValues("ns-a", "instance-1").Set(1)
	InstanceReady.WithLabelValues("ns-b", "instance-2").Set(0)

	v1 := testutil.ToFloat64(InstanceReady.WithLabelValues("ns-a", "instance-1"))
	v2 := testutil.ToFloat64(InstanceReady.WithLabelValues("ns-b", "instance-2"))
	assert.Equal(t, float64(1), v1)
	assert.Equal(t, float64(0), v2)
}
