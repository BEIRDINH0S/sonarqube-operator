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

package sonarqube

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Client définit les opérations que l'opérateur effectue sur l'API SonarQube.
// C'est une interface — ça permet d'injecter un mock dans les tests.
type Client interface {
	GetStatus(ctx context.Context) (string, error)
	ChangeAdminPassword(ctx context.Context, currentPassword, newPassword string) error
}

type httpClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient crée un client HTTP pour l'API SonarQube.
func NewClient(baseURL string) Client {
	return &httpClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type statusResponse struct {
	Status string `json:"status"`
}

// GetStatus appelle /api/system/status (sans auth) et retourne l'état de SonarQube.
// Valeurs possibles : "STARTING", "UP", "RESTARTING", "DB_MIGRATION_NEEDED", "DB_MIGRATION_RUNNING", "DOWN".
func (c *httpClient) GetStatus(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/system/status", nil)
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result statusResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Status, nil
}

// ChangeAdminPassword change le mot de passe du compte admin via /api/users/change_password.
// Utilise la Basic Auth avec le mot de passe courant.
func (c *httpClient) ChangeAdminPassword(ctx context.Context, currentPassword, newPassword string) error {
	body := strings.NewReader(fmt.Sprintf(
		"login=admin&previousPassword=%s&password=%s",
		currentPassword, newPassword,
	))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/users/change_password", body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("admin", currentPassword)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("change password failed with status %d", resp.StatusCode)
	}
	return nil
}
