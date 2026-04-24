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

package sonarqube_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BEIRDINH0S/sonarqube-operator/internal/sonarqube"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestServer crée un faux serveur HTTP SonarQube pour les tests.
// handlers est une map path → fonction de réponse.
func newTestServer(t *testing.T, handlers map[string]http.HandlerFunc) (*httptest.Server, sonarqube.Client) {
	t.Helper()
	mux := http.NewServeMux()
	for path, handler := range handlers {
		mux.HandleFunc(path, handler)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, sonarqube.NewClient(srv.URL, "test-token")
}

func TestGetStatus(t *testing.T) {
	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"/api/system/status": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"UP","version":"10.3"}`))
		},
	})

	status, err := client.GetStatus(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "UP", status)
}

func TestGetStatus_SonarQubeError(t *testing.T) {
	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"/api/system/status": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"errors":[{"msg":"Service unavailable"}]}`))
		},
	})

	_, err := client.GetStatus(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Service unavailable")
}

func TestListInstalledPlugins(t *testing.T) {
	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"/api/plugins/installed": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"plugins":[
				{"key":"sonar-java","name":"SonarJava","version":"7.30.1"},
				{"key":"sonar-go","name":"SonarGo","version":"1.12.0"}
			]}`))
		},
	})

	plugins, err := client.ListInstalledPlugins(context.Background())
	require.NoError(t, err)
	assert.Len(t, plugins, 2)
	assert.Equal(t, "sonar-java", plugins[0].Key)
	assert.Equal(t, "7.30.1", plugins[0].Version)
}

func TestInstallPlugin(t *testing.T) {
	var receivedKey, receivedVersion string
	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"/api/plugins/install": func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, r.ParseForm())
			receivedKey = r.FormValue("key")
			receivedVersion = r.FormValue("version")
			w.WriteHeader(http.StatusNoContent)
		},
	})

	err := client.InstallPlugin(context.Background(), "sonar-java", "7.30.1")
	require.NoError(t, err)
	assert.Equal(t, "sonar-java", receivedKey)
	assert.Equal(t, "7.30.1", receivedVersion)
}

func TestUninstallPlugin(t *testing.T) {
	var receivedKey string
	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"/api/plugins/uninstall": func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, r.ParseForm())
			receivedKey = r.FormValue("key")
			w.WriteHeader(http.StatusNoContent)
		},
	})

	err := client.UninstallPlugin(context.Background(), "sonar-java")
	require.NoError(t, err)
	assert.Equal(t, "sonar-java", receivedKey)
}

func TestCreateProject(t *testing.T) {
	var receivedKey, receivedName, receivedVisibility string
	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"/api/projects/create": func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, r.ParseForm())
			receivedKey = r.FormValue("project")
			receivedName = r.FormValue("name")
			receivedVisibility = r.FormValue("visibility")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		},
	})

	err := client.CreateProject(context.Background(), "my-project", "My Project", "private")
	require.NoError(t, err)
	assert.Equal(t, "my-project", receivedKey)
	assert.Equal(t, "My Project", receivedName)
	assert.Equal(t, "private", receivedVisibility)
}

func TestGetProject(t *testing.T) {
	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"/api/projects/search": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"components":[{"key":"my-project","name":"My Project","visibility":"private"}]}`))
		},
	})

	project, err := client.GetProject(context.Background(), "my-project")
	require.NoError(t, err)
	assert.Equal(t, "my-project", project.Key)
	assert.Equal(t, "private", project.Visibility)
}

func TestGetProject_NotFound(t *testing.T) {
	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"/api/projects/search": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"components":[]}`))
		},
	})

	_, err := client.GetProject(context.Background(), "unknown")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCreateQualityGate(t *testing.T) {
	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"/api/qualitygates/create": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":42,"name":"strict-gate"}`))
		},
	})

	gate, err := client.CreateQualityGate(context.Background(), "strict-gate")
	require.NoError(t, err)
	assert.Equal(t, int64(42), gate.ID)
	assert.Equal(t, "strict-gate", gate.Name)
}

func TestAddCondition(t *testing.T) {
	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"/api/qualitygates/create_condition": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":10,"metric":"coverage","op":"LT","error":"80"}`))
		},
	})

	cond, err := client.AddCondition(context.Background(), 42, "coverage", "LT", "80")
	require.NoError(t, err)
	assert.Equal(t, int64(10), cond.ID)
	assert.Equal(t, "coverage", cond.Metric)
}

func TestGetQualityGate(t *testing.T) {
	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"/api/qualitygates/show": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":7,"name":"strict-gate","conditions":[
				{"id":101,"metric":"coverage","op":"LT","error":"80"}
			]}`))
		},
	})

	gate, err := client.GetQualityGate(context.Background(), "strict-gate")
	require.NoError(t, err)
	assert.Equal(t, int64(7), gate.ID)
	assert.Equal(t, "strict-gate", gate.Name)
	assert.Len(t, gate.Conditions, 1)
	assert.Equal(t, "coverage", gate.Conditions[0].Metric)
}

func TestGetQualityGate_NotFound(t *testing.T) {
	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"/api/qualitygates/show": func(w http.ResponseWriter, _ *http.Request) {
			// SonarQube retourne 400 avec un body d'erreur quand le gate est absent
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errors":[{"msg":"No quality gate has been found for name 'unknown'"}]}`))
		},
	})

	_, err := client.GetQualityGate(context.Background(), "unknown")
	require.Error(t, err)
	assert.ErrorIs(t, err, sonarqube.ErrNotFound)
}

func TestGetQualityGate_NetworkError_NotTreatedAsNotFound(t *testing.T) {
	// Client pointant vers un serveur inexistant — erreur réseau pure
	client := sonarqube.NewClient("http://127.0.0.1:19999", "token")

	_, err := client.GetQualityGate(context.Background(), "my-gate")
	require.Error(t, err)
	// Une erreur réseau NE doit PAS être ErrNotFound
	assert.False(t, errors.Is(err, sonarqube.ErrNotFound),
		"network error should not be treated as ErrNotFound")
}

func TestBearerTokenSent(t *testing.T) {
	var receivedAuth string
	_, client := newTestServer(t, map[string]http.HandlerFunc{
		"/api/plugins/installed": func(w http.ResponseWriter, r *http.Request) {
			receivedAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"plugins":[]}`))
		},
	})

	_, err := client.ListInstalledPlugins(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "Bearer test-token", receivedAuth)
}
