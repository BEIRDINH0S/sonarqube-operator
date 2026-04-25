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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// errIsAPIError retourne true si l'erreur vient d'une réponse JSON SonarQube (4xx avec body).
// Permet de distinguer "ressource absente" d'une vraie erreur réseau.
func errIsAPIError(err error) bool {
	var e *apiError
	return errors.As(err, &e)
}

// ErrNotFound est retourné quand une ressource n'existe pas dans SonarQube.
// Utiliser errors.Is(err, ErrNotFound) pour distinguer "absent" d'une vraie erreur réseau.
var ErrNotFound = errors.New("not found")

// --- Types métier ---

// Plugin représente un plugin SonarQube installé.
type Plugin struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Project représente un projet SonarQube.
type Project struct {
	Key        string `json:"key"`
	Name       string `json:"name"`
	Visibility string `json:"visibility"`
}

// QualityGate représente un quality gate SonarQube.
type QualityGate struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	IsDefault  bool        `json:"isDefault"`
	Conditions []Condition `json:"conditions"`
}

// Condition représente une condition d'un quality gate.
// L'ID est une chaîne (UUID) depuis SonarQube 10.x.
type Condition struct {
	ID     string `json:"id"`
	Metric string `json:"metric"`
	Op     string `json:"op"`
	Error  string `json:"error"`
}

// Token représente un token d'accès SonarQube.
type Token struct {
	Name  string `json:"name"`
	Token string `json:"token"`
}

// --- Interface ---

// Client définit toutes les opérations que l'opérateur effectue sur l'API SonarQube.
// C'est une interface pour pouvoir injecter un mock dans les tests.
type Client interface {
	// Système
	// GetStatus retourne le statut SonarQube ("UP", "STARTING"…) et la version réelle du serveur.
	GetStatus(ctx context.Context) (status, version string, err error)
	Restart(ctx context.Context) error
	ChangeAdminPassword(ctx context.Context, currentPassword, newPassword string) error
	ValidateAuth(ctx context.Context) error

	// Plugins
	ListInstalledPlugins(ctx context.Context) ([]Plugin, error)
	InstallPlugin(ctx context.Context, key, version string) error
	UninstallPlugin(ctx context.Context, key string) error

	// Projets
	CreateProject(ctx context.Context, key, name, visibility string) error
	GetProject(ctx context.Context, key string) (*Project, error)
	DeleteProject(ctx context.Context, key string) error
	UpdateProjectVisibility(ctx context.Context, key, visibility string) error

	// Quality Gates
	ListQualityGates(ctx context.Context) ([]QualityGate, error)
	GetQualityGate(ctx context.Context, name string) (*QualityGate, error)
	CreateQualityGate(ctx context.Context, name string) (*QualityGate, error)
	// DeleteQualityGate supprime le quality gate via l'API REST v2 de SonarQube 10.x.
	// id est l'UUID retourné par CreateQualityGate / GetQualityGate.
	DeleteQualityGate(ctx context.Context, id string) error
	// AddCondition ajoute une condition à un quality gate identifié par son nom.
	// Le paramètre gateId est déprécié depuis SonarQube 9.8 ; gateName est requis en 10.x.
	AddCondition(ctx context.Context, gateName string, metric, op, value string) (*Condition, error)
	RemoveCondition(ctx context.Context, conditionID string) error
	SetAsDefault(ctx context.Context, name string) error
	AssignQualityGate(ctx context.Context, projectKey, gateName string) error

	// Tokens
	GenerateToken(ctx context.Context, name, tokenType, projectKey string) (*Token, error)
	RevokeToken(ctx context.Context, name string) error
}

// --- Implémentation HTTP ---

// defaultRetryDelays defines the wait between successive retry attempts.
// Index 0 is the delay before the first retry (after the initial attempt fails).
var defaultRetryDelays = []time.Duration{500 * time.Millisecond, 1 * time.Second}

type httpClient struct {
	baseURL     string
	token       string
	username    string
	password    string
	httpClient  *http.Client
	retryDelays []time.Duration // nil → use defaultRetryDelays
}

// NewClient crée un client HTTP pour l'API SonarQube authentifié par Bearer token.
func NewClient(baseURL, token string) Client {
	return &httpClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewClientWithPassword crée un client authentifié en Basic Auth (username:password).
// Réservé au bootstrap admin : une fois le token généré, utiliser NewClient.
func NewClientWithPassword(baseURL, username, password string) Client {
	return &httpClient{
		baseURL:  strings.TrimRight(baseURL, "/"),
		username: username,
		password: password,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// apiError représente la structure d'erreur retournée par l'API SonarQube.
// SonarQube retourne toujours les erreurs dans le body JSON, pas seulement via le status HTTP.
type apiError struct {
	Errors []struct {
		Msg string `json:"msg"`
	} `json:"errors"`
}

func (e *apiError) Error() string {
	msgs := make([]string, len(e.Errors))
	for i, err := range e.Errors {
		msgs[i] = err.Msg
	}
	return strings.Join(msgs, "; ")
}

// do executes an HTTP request with retries for transient network errors.
// SonarQube API errors (4xx/5xx responses) are not retried — the reconcile
// loop's own exponential backoff handles those.
func (c *httpClient) do(ctx context.Context, method, path string, params url.Values) ([]byte, error) {
	retryDelays := c.retryDelays
	if retryDelays == nil {
		retryDelays = defaultRetryDelays
	}

	body, isNetworkErr, err := c.doOnce(ctx, method, path, params)
	if err == nil || !isNetworkErr {
		return body, err
	}

	for _, delay := range retryDelays {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
		body, isNetworkErr, err = c.doOnce(ctx, method, path, params)
		if err == nil || !isNetworkErr {
			return body, err
		}
	}
	return nil, err
}

// doOnce executes one HTTP attempt. Returns (body, isNetworkError, error).
func (c *httpClient) doOnce(ctx context.Context, method, path string, params url.Values) ([]byte, bool, error) {
	var bodyReader io.Reader
	fullURL := c.baseURL + path

	if method == http.MethodPost && params != nil {
		bodyReader = strings.NewReader(params.Encode())
	} else if method == http.MethodGet && params != nil {
		fullURL += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, false, err
	}

	if method == http.MethodPost && params != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	} else if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, true, err // network error — retryable
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}

	if resp.StatusCode >= 400 {
		var apiErr apiError
		if jsonErr := json.Unmarshal(body, &apiErr); jsonErr == nil && len(apiErr.Errors) > 0 {
			return nil, false, &apiErr
		}
		return nil, false, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return body, false, nil
}

// --- Système ---

type statusResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

func (c *httpClient) GetStatus(ctx context.Context) (status, version string, err error) {
	body, reqErr := c.do(ctx, http.MethodGet, "/api/system/status", nil)
	if reqErr != nil {
		return "", "", reqErr
	}
	var result statusResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", err
	}
	return result.Status, result.Version, nil
}

func (c *httpClient) Restart(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodPost, "/api/system/restart", nil)
	return err
}

func (c *httpClient) ChangeAdminPassword(ctx context.Context, currentPassword, newPassword string) error {
	params := url.Values{
		"login":            {"admin"},
		"previousPassword": {currentPassword},
		"password":         {newPassword},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/users/change_password",
		strings.NewReader(params.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("admin", currentPassword)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("change password failed with status %d", resp.StatusCode)
	}
	return nil
}

type authValidateResponse struct {
	Valid bool `json:"valid"`
}

// ValidateAuth vérifie que les credentials du client sont valides via /api/authentication/validate.
// Retourne une erreur si l'auth échoue (401 ou valid=false).
func (c *httpClient) ValidateAuth(ctx context.Context) error {
	body, err := c.do(ctx, http.MethodGet, "/api/authentication/validate", nil)
	if err != nil {
		return err
	}
	var result authValidateResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return err
	}
	if !result.Valid {
		return fmt.Errorf("authentication invalid")
	}
	return nil
}

// --- Plugins ---

type pluginsResponse struct {
	Plugins []Plugin `json:"plugins"`
}

func (c *httpClient) ListInstalledPlugins(ctx context.Context) ([]Plugin, error) {
	body, err := c.do(ctx, http.MethodGet, "/api/plugins/installed", nil)
	if err != nil {
		return nil, err
	}
	var result pluginsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result.Plugins, nil
}

func (c *httpClient) InstallPlugin(ctx context.Context, key, version string) error {
	params := url.Values{"key": {key}}
	if version != "" {
		params.Set("version", version)
	}
	_, err := c.do(ctx, http.MethodPost, "/api/plugins/install", params)
	return err
}

func (c *httpClient) UninstallPlugin(ctx context.Context, key string) error {
	_, err := c.do(ctx, http.MethodPost, "/api/plugins/uninstall", url.Values{"key": {key}})
	return err
}

// --- Projets ---

type projectSearchResponse struct {
	Components []Project `json:"components"`
}

func (c *httpClient) CreateProject(ctx context.Context, key, name, visibility string) error {
	_, err := c.do(ctx, http.MethodPost, "/api/projects/create", url.Values{
		"project":    {key},
		"name":       {name},
		"visibility": {visibility},
	})
	return err
}

func (c *httpClient) GetProject(ctx context.Context, key string) (*Project, error) {
	body, err := c.do(ctx, http.MethodGet, "/api/projects/search", url.Values{"projects": {key}})
	if err != nil {
		return nil, err
	}
	var result projectSearchResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	// L'API /projects/search fait un startsWith — on filtre côté client pour un match exact.
	for i := range result.Components {
		if result.Components[i].Key == key {
			return &result.Components[i], nil
		}
	}
	return nil, fmt.Errorf("project %q: %w", key, ErrNotFound)
}

func (c *httpClient) DeleteProject(ctx context.Context, key string) error {
	_, err := c.do(ctx, http.MethodPost, "/api/projects/delete", url.Values{"project": {key}})
	return err
}

func (c *httpClient) UpdateProjectVisibility(ctx context.Context, key, visibility string) error {
	_, err := c.do(ctx, http.MethodPost, "/api/projects/update_visibility", url.Values{
		"project":    {key},
		"visibility": {visibility},
	})
	return err
}

// --- Quality Gates ---

type qualityGatesResponse struct {
	Qualitygates []QualityGate `json:"qualitygates"`
}

type qualityGateResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type conditionResponse struct {
	ID     string `json:"id"`
	Metric string `json:"metric"`
	Op     string `json:"op"`
	Error  string `json:"error"`
}

func (c *httpClient) ListQualityGates(ctx context.Context) ([]QualityGate, error) {
	body, err := c.do(ctx, http.MethodGet, "/api/qualitygates/list", nil)
	if err != nil {
		return nil, err
	}
	var result qualityGatesResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result.Qualitygates, nil
}

// GetQualityGate retourne le quality gate avec ses conditions via /api/qualitygates/show.
// L'appel /api/qualitygates/list ne renvoie pas les conditions — seul /show le fait.
// Retourne ErrNotFound si SonarQube répond avec une erreur API (gate absent).
// Propage les erreurs réseau sans les transformer en ErrNotFound.
func (c *httpClient) GetQualityGate(ctx context.Context, name string) (*QualityGate, error) {
	body, err := c.do(ctx, http.MethodGet, "/api/qualitygates/show", url.Values{"name": {name}})
	if err != nil {
		if errIsAPIError(err) {
			return nil, fmt.Errorf("quality gate %q: %w", name, ErrNotFound)
		}
		return nil, err
	}
	var result QualityGate
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *httpClient) CreateQualityGate(ctx context.Context, name string) (*QualityGate, error) {
	body, err := c.do(ctx, http.MethodPost, "/api/qualitygates/create", url.Values{"name": {name}})
	if err != nil {
		return nil, err
	}
	var result qualityGateResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &QualityGate{ID: result.ID, Name: result.Name}, nil
}

func (c *httpClient) DeleteQualityGate(ctx context.Context, id string) error {
	_, err := c.do(ctx, http.MethodDelete, "/api/v2/quality-gates/"+id, nil)
	return err
}

func (c *httpClient) AddCondition(ctx context.Context, gateName string, metric, op, value string) (*Condition, error) {
	body, err := c.do(ctx, http.MethodPost, "/api/qualitygates/create_condition", url.Values{
		"gateName": {gateName},
		"metric":   {metric},
		"op":       {op},
		"error":    {value},
	})
	if err != nil {
		return nil, err
	}
	var result conditionResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &Condition{ID: result.ID, Metric: result.Metric, Op: result.Op, Error: result.Error}, nil
}

func (c *httpClient) RemoveCondition(ctx context.Context, conditionID string) error {
	_, err := c.do(ctx, http.MethodPost, "/api/qualitygates/delete_condition",
		url.Values{"id": {conditionID}})
	return err
}

func (c *httpClient) SetAsDefault(ctx context.Context, name string) error {
	_, err := c.do(ctx, http.MethodPost, "/api/qualitygates/set_as_default", url.Values{"name": {name}})
	return err
}

func (c *httpClient) AssignQualityGate(ctx context.Context, projectKey, gateName string) error {
	_, err := c.do(ctx, http.MethodPost, "/api/qualitygates/select", url.Values{
		"projectKey": {projectKey},
		"gateName":   {gateName},
	})
	return err
}

// --- Tokens ---

func (c *httpClient) GenerateToken(ctx context.Context, name, tokenType, projectKey string) (*Token, error) {
	params := url.Values{
		"name": {name},
		"type": {tokenType},
	}
	if projectKey != "" {
		params.Set("projectKey", projectKey)
	}
	body, err := c.do(ctx, http.MethodPost, "/api/user_tokens/generate", params)
	if err != nil {
		return nil, err
	}
	var result Token
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *httpClient) RevokeToken(ctx context.Context, name string) error {
	_, err := c.do(ctx, http.MethodPost, "/api/user_tokens/revoke", url.Values{"name": {name}})
	return err
}
