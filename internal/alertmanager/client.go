package alertmanager

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"notificator/config"
	"notificator/internal/auth"
	"notificator/internal/models"
)

type customHeaderRoundTripper struct {
	headers map[string]string
	rt      http.RoundTripper
}

func (c *customHeaderRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())

	for key, value := range c.headers {
		req.Header.Set(key, value)
	}

	return c.rt.RoundTrip(req)
}

type Client struct {
	Name       string
	BaseURL    string
	HTTPClient *http.Client
	Timeout    time.Duration

	Username string
	Password string
	Token    string

	Headers map[string]string // For OAuth bypass, etc.

	ProxyAuthManager *auth.ProxyAuthManager
}

type MultiClient struct {
	clients map[string]*Client
	mutex   sync.RWMutex
}

func NewClient(baseURL string) *Client {
	return &Client{
		Name:    "default",
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second, // 10 seconds
		},
		Timeout: 10 * time.Second, // 10 seconds
		Headers: make(map[string]string),
	}
}

func (c *Client) DebugRequest(endpoint string) {
	url := fmt.Sprintf("%s%s", c.BaseURL, endpoint)
	fmt.Printf("DEBUG: Making request to %s\n", url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Printf("  ✗ Failed to create request: %v\n", err)
		return
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Notificator/1.0")
	c.addAuth(req)

	fmt.Println("Request headers:")
	for key, values := range req.Header {
		for _, value := range values {
			if key == "X-Oauth-Bypass-Token" {
				fmt.Printf("  %s: %s...\n", key, value[:min(10, len(value))])
			} else {
				fmt.Printf("  %s: %s\n", key, value)
			}
		}
	}

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // Don't follow redirects
		},
	}

	if len(c.Headers) > 0 {
		transport := &customHeaderRoundTripper{
			headers: c.Headers,
			rt:      http.DefaultTransport,
		}
		client.Transport = transport
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("  ✗ Request failed: %v\n", err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("Response status: %d %s\n", resp.StatusCode, resp.Status)

	fmt.Println("Response headers:")
	for key, values := range resp.Header {
		for _, value := range values {
			fmt.Printf("  %s: %s\n", key, value)
		}
	}

	if location := resp.Header.Get("Location"); location != "" {
		fmt.Printf("Redirect to: %s\n", location)
	}

	body, _ := io.ReadAll(resp.Body)
	if len(body) > 0 {
		if body[0] == '{' || body[0] == '[' {
			fmt.Printf("JSON Response (first 200 chars): %s\n", string(body[:min(200, len(body))]))
		} else {
			fmt.Printf("Non-JSON Response (first 200 chars): %s\n", string(body[:min(200, len(body))]))
		}
	}

	fmt.Println()
}

func (c *Client) TestOAuthBypass() error {
	fmt.Println("=== Testing OAuth Bypass ===")

	bypassToken, hasToken := c.Headers["X-Oauth-Bypass-Token"]
	if !hasToken || bypassToken == "" {
		return fmt.Errorf("no X-Oauth-Bypass-Token found in headers")
	}

	fmt.Printf("Found OAuth bypass token (first 10 chars): %s...\n", bypassToken[:min(10, len(bypassToken))])

	url := fmt.Sprintf("%s/api/v2/alerts", c.BaseURL)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-Oauth-Bypass-Token", bypassToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Notificator/1.0")

	client := &http.Client{
		Timeout: 10 * time.Second, // 10 seconds
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // Don't follow redirects
		},
	}

	fmt.Printf("Making request to: %s\n", url)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	fmt.Printf("Response status: %d %s\n", resp.StatusCode, resp.Status)

	if resp.StatusCode == 200 {
		body, _ := io.ReadAll(resp.Body)
		if len(body) > 0 && (body[0] == '{' || body[0] == '[') {
			fmt.Println("✅ SUCCESS: OAuth bypass working! Got JSON response")
			fmt.Printf("Response preview: %s\n", string(body[:min(200, len(body))]))
			return nil
		} else {
			fmt.Println("⚠️  Got 200 but response doesn't look like JSON")
			fmt.Printf("Response: %s\n", string(body[:min(200, len(body))]))
		}
	} else if resp.StatusCode == 302 {
		location := resp.Header.Get("Location")
		if strings.Contains(location, "oauth") {
			fmt.Println("❌ OAuth bypass NOT working - still getting redirected to OAuth")
			fmt.Printf("Redirect location: %s\n", location)
			return fmt.Errorf("OAuth bypass failed - got redirect to: %s", location)
		}
	} else {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("❌ Unexpected status code: %d\n", resp.StatusCode)
		fmt.Printf("Response: %s\n", string(body[:min(200, len(body))]))
		return fmt.Errorf("unexpected response: %d %s", resp.StatusCode, resp.Status)
	}

	return fmt.Errorf("OAuth bypass test inconclusive")
}

func NewClientWithAuth(baseURL, username, password, token string) *Client {
	return &Client{
		Name:    "default",
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second, // 10 seconds
		},
		Timeout:  10 * time.Second, // 10 seconds
		Username: username,
		Password: password,
		Token:    token,
		Headers:  make(map[string]string),
	}
}

func NewClientWithProxyAuth(baseURL string) *Client {
	proxyAuth := auth.NewProxyAuthManager(baseURL)

	return &Client{
		Name:             "default",
		BaseURL:          baseURL,
		HTTPClient:       proxyAuth.GetAuthenticatedClient(),
		Timeout:          10 * time.Second, // 10 seconds
		Headers:          make(map[string]string),
		ProxyAuthManager: proxyAuth,
	}
}

func NewClientWithConfig(baseURL, username, password, token string, headers map[string]string, name string) *Client {
	var httpClient *http.Client

	checkRedirect := func(req *http.Request, via []*http.Request) error {
		if len(via) > 0 {
			for key, value := range headers {
				req.Header.Set(key, value)
			}
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			} else if username != "" && password != "" {
				req.SetBasicAuth(username, password)
			}
		}

		if len(via) >= 10 { // Support up to 10 redirects
			return fmt.Errorf("too many redirects")
		}
		return nil
	}

	if len(headers) > 0 {
		transport := &customHeaderRoundTripper{
			headers: headers,
			rt:      http.DefaultTransport,
		}

		httpClient = &http.Client{
			Timeout:       10 * time.Second, // 10 seconds
			Transport:     transport,
			CheckRedirect: checkRedirect,
		}
	} else {
		httpClient = &http.Client{
			Timeout:       10 * time.Second, // 10 seconds
			CheckRedirect: checkRedirect,
		}
	}

	return &Client{
		Name:       name,
		BaseURL:    baseURL,
		HTTPClient: httpClient,
		Timeout:    10 * time.Second, // 10 seconds
		Username:   username,
		Password:   password,
		Token:      token,
		Headers:    headers,
	}
}

func NewMultiClient(cfg *config.Config) *MultiClient {
	mc := &MultiClient{
		clients: make(map[string]*Client),
	}

	for _, amConfig := range cfg.Alertmanagers {
		client := NewClientFromConfig(amConfig)
		mc.clients[amConfig.Name] = client
	}

	return mc
}

func NewClientFromConfig(amConfig config.AlertmanagerConfig) *Client {
	return NewClientWithConfig(
		amConfig.URL,
		amConfig.Username,
		amConfig.Password,
		amConfig.Token,
		amConfig.Headers,
		amConfig.Name,
	)
}

func (mc *MultiClient) GetClient(name string) (*Client, bool) {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	client, exists := mc.clients[name]
	return client, exists
}

func (mc *MultiClient) GetAllClients() map[string]*Client {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	clients := make(map[string]*Client) // Copy to avoid race conditions
	for name, client := range mc.clients {
		clients[name] = client
	}
	return clients
}

func (mc *MultiClient) AddClient(amConfig config.AlertmanagerConfig) {
	mc.mutex.Lock()
	defer mc.mutex.Unlock()

	client := NewClientFromConfig(amConfig)
	mc.clients[amConfig.Name] = client
}

func (mc *MultiClient) RemoveClient(name string) {
	mc.mutex.Lock()
	defer mc.mutex.Unlock()

	delete(mc.clients, name)
}

func (mc *MultiClient) UpdateClient(amConfig config.AlertmanagerConfig) {
	mc.mutex.Lock()
	defer mc.mutex.Unlock()

	client := NewClientFromConfig(amConfig)
	mc.clients[amConfig.Name] = client
}

func (c *Client) addAuth(req *http.Request) {
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	} else if c.Username != "" && c.Password != "" {
		req.SetBasicAuth(c.Username, c.Password)
	}
}

func (c *Client) FetchAlerts() ([]models.Alert, error) {
	url := fmt.Sprintf("%s/api/v2/alerts", c.BaseURL)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Notificator/1.0")

	c.addAuth(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("alertmanager returned status %d, body: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if len(body) > 0 && body[0] == '<' {
		return nil, fmt.Errorf("received HTML response instead of JSON. Response: %s", string(body[:min(500, len(body))]))
	}

	var alerts []models.Alert
	if err := json.Unmarshal(body, &alerts); err != nil {
		return nil, fmt.Errorf("failed to decode v2 response: %w. Response was: %s", err, string(body[:min(200, len(body))]))
	}

	return alerts, nil
}

func (c *Client) FetchActiveAlerts() ([]models.Alert, error) {
	allAlerts, err := c.FetchAlerts()
	if err != nil {
		return nil, err
	}

	var activeAlerts []models.Alert
	for _, alert := range allAlerts {
		if alert.IsActive() {
			activeAlerts = append(activeAlerts, alert)
		}
	}

	return activeAlerts, nil
}

func (c *Client) FetchSilence(silenceID string) (*models.Silence, error) {
	url := fmt.Sprintf("%s/api/v2/silence/%s", c.BaseURL, silenceID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Notificator/1.0")

	c.addAuth(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("silence with ID %s not found", silenceID)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("alertmanager returned status %d, body: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if len(body) > 0 && body[0] == '<' {
		return nil, fmt.Errorf("received HTML response instead of JSON. Response: %s", string(body[:min(500, len(body))]))
	}

	var silence models.Silence
	if err := json.Unmarshal(body, &silence); err != nil {
		return nil, fmt.Errorf("failed to decode silence response: %w. Response was: %s", err, string(body[:min(200, len(body))]))
	}

	return &silence, nil
}

func (c *Client) FetchSilences() ([]models.Silence, error) {
	url := fmt.Sprintf("%s/api/v2/silences", c.BaseURL)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Notificator/1.0")

	c.addAuth(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("alertmanager returned status %d, body: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if len(body) > 0 && body[0] == '<' {
		return nil, fmt.Errorf("received HTML response instead of JSON. Response: %s", string(body[:min(500, len(body))]))
	}

	var silences []models.Silence
	if err := json.Unmarshal(body, &silences); err != nil {
		return nil, fmt.Errorf("failed to decode silences response: %w. Response was: %s", err, string(body[:min(200, len(body))]))
	}

	return silences, nil
}

func (c *Client) CreateSilence(silence models.Silence) (*models.Silence, error) {
	url := fmt.Sprintf("%s/api/v2/silences", c.BaseURL)

	jsonData, err := json.Marshal(silence)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal silence: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Notificator/1.0")

	c.addAuth(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("alertmanager returned status %d, body: %s", resp.StatusCode, string(body))
	}

	if len(body) > 0 && body[0] == '<' {
		return nil, fmt.Errorf("received HTML response instead of JSON. Response: %s", string(body[:min(500, len(body))]))
	}

	var response struct {
		SilenceID string `json:"silenceID"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode silence response: %w. Response was: %s", err, string(body[:min(200, len(body))]))
	}

	createdSilence := silence
	createdSilence.ID = response.SilenceID

	return &createdSilence, nil
}

func (c *Client) DeleteSilence(silenceID string) error {
	url := fmt.Sprintf("%s/api/v2/silence/%s", c.BaseURL, silenceID)

	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Notificator/1.0")

	c.addAuth(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("silence with ID %s not found", silenceID)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("alertmanager returned status %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (c *Client) TestConnection() error {
	url := fmt.Sprintf("%s/api/v2/alerts", c.BaseURL) // v2 API doesn't have dedicated status endpoint

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to alertmanager: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("alertmanager returned status %d: %s", resp.StatusCode, string(body[:min(200, len(body))]))
	}

	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (c *Client) DebugURL() {
	fmt.Printf("DEBUG: Using Alertmanager URL: %s/api/v2/alerts\n", c.BaseURL)
}

func (c *Client) TestAPIEndpoints() {
	endpoints := []string{
		"/api/v2/alerts", // Current standard
		"/api/v1/alerts", // Deprecated/removed
		"/alerts",
		"/api/alerts",
	}

	fmt.Println("Testing different API endpoints...")

	for _, endpoint := range endpoints {
		url := fmt.Sprintf("%s%s", c.BaseURL, endpoint)
		fmt.Printf("Testing: %s\n", url)

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			fmt.Printf("  ✗ Failed to create request: %v\n", err)
			continue
		}

		req.Header.Set("Accept", "application/json")
		c.addAuth(req)

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			fmt.Printf("  ✗ Request failed: %v\n", err)
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		fmt.Printf("  Status: %d\n", resp.StatusCode)
		fmt.Printf("  Content-Type: %s\n", resp.Header.Get("Content-Type"))

		if resp.StatusCode == 200 && len(body) > 0 {
			if body[0] == '{' || body[0] == '[' {
				fmt.Printf("  ✓ Returns JSON (first 100 chars): %s\n", string(body[:min(100, len(body))]))
			} else {
				fmt.Printf("  ✗ Returns non-JSON (first 100 chars): %s\n", string(body[:min(100, len(body))]))
			}
		} else if strings.Contains(string(body), "deprecated") {
			fmt.Printf("  ⚠️  API deprecated: %s\n", string(body[:min(100, len(body))]))
		} else if len(body) > 0 {
			fmt.Printf("  ✗ Error response (first 100 chars): %s\n", string(body[:min(100, len(body))]))
		}
		fmt.Println()
	}
}

type AlertWithSource struct {
	Alert  models.Alert
	Source string // Name of the Alertmanager instance
}

type SilenceWithSource struct {
	Silence models.Silence
	Source  string // Name of the Alertmanager instance
}

func (mc *MultiClient) FetchAllAlerts() ([]AlertWithSource, error) {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	var allAlerts []AlertWithSource
	var errors []error

	for name, client := range mc.clients {
		alerts, err := client.FetchAlerts()
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to fetch alerts from %s: %w", name, err))
			continue
		}

		for _, alert := range alerts {
			allAlerts = append(allAlerts, AlertWithSource{
				Alert:  alert,
				Source: name,
			})
		}
	}

	if len(errors) > 0 && len(allAlerts) == 0 { // If all clients failed, return the first error
		return nil, errors[0]
	}

	return allAlerts, nil
}

func (mc *MultiClient) FetchAllActiveAlerts() ([]AlertWithSource, error) {
	allAlerts, err := mc.FetchAllAlerts()
	if err != nil {
		return nil, err
	}

	var activeAlerts []AlertWithSource
	for _, alertWithSource := range allAlerts {
		if alertWithSource.Alert.IsActive() {
			activeAlerts = append(activeAlerts, alertWithSource)
		}
	}

	return activeAlerts, nil
}

func (mc *MultiClient) FetchAllSilences() ([]SilenceWithSource, error) {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	var allSilences []SilenceWithSource
	var errors []error

	for name, client := range mc.clients {
		silences, err := client.FetchSilences()
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to fetch silences from %s: %w", name, err))
			continue
		}

		for _, silence := range silences {
			allSilences = append(allSilences, SilenceWithSource{
				Silence: silence,
				Source:  name,
			})
		}
	}

	if len(errors) > 0 && len(allSilences) == 0 { // If all clients failed, return the first error
		return nil, errors[0]
	}

	return allSilences, nil
}

func (mc *MultiClient) TestAllConnections() map[string]error {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	results := make(map[string]error)

	for name, client := range mc.clients {
		results[name] = client.TestConnection()
	}

	return results
}

func (mc *MultiClient) CreateSilenceOnAlertmanager(alertmanagerName string, silence models.Silence) (*models.Silence, error) {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	client, exists := mc.clients[alertmanagerName]
	if !exists {
		return nil, fmt.Errorf("alertmanager '%s' not found", alertmanagerName)
	}

	return client.CreateSilence(silence)
}

func (mc *MultiClient) FetchSilenceFromAlertmanager(alertmanagerName, silenceID string) (*models.Silence, error) {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	client, exists := mc.clients[alertmanagerName]
	if !exists {
		return nil, fmt.Errorf("alertmanager '%s' not found", alertmanagerName)
	}

	return client.FetchSilence(silenceID)
}

func (mc *MultiClient) DeleteSilenceFromAlertmanager(alertmanagerName, silenceID string) error {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	client, exists := mc.clients[alertmanagerName]
	if !exists {
		return fmt.Errorf("alertmanager '%s' not found", alertmanagerName)
	}

	return client.DeleteSilence(silenceID)
}

func (mc *MultiClient) GetClientNames() []string {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	names := make([]string, 0, len(mc.clients))
	for name := range mc.clients {
		names = append(names, name)
	}

	return names
}

func (c *Client) GetName() string {
	return c.Name
}

func (c *Client) SetName(name string) {
	c.Name = name
}

func (c *Client) String() string {
	return fmt.Sprintf("Alertmanager{Name: %s, URL: %s}", c.Name, c.BaseURL)
}

func (c *Client) IsHealthy() bool {
	return c.TestConnection() == nil
}

func (mc *MultiClient) GetHealthStatus() map[string]bool {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	status := make(map[string]bool)
	for name, client := range mc.clients {
		status[name] = client.IsHealthy()
	}

	return status
}

func (mc *MultiClient) GetHealthyClients() map[string]*Client {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	healthy := make(map[string]*Client)
	for name, client := range mc.clients {
		if client.IsHealthy() {
			healthy[name] = client
		}
	}

	return healthy
}

func (mc *MultiClient) Count() int {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	return len(mc.clients)
}

// MigrateFromSingleClient helps migrate from single client usage to MultiClient
func MigrateFromSingleClient(oldClient *Client) *MultiClient {
	mc := &MultiClient{
		clients: make(map[string]*Client),
	}

	name := oldClient.Name
	if name == "" {
		name = "Default"
	}

	mc.clients[name] = oldClient
	return mc
}

func (mc *MultiClient) UpdateFromConfig(cfg *config.Config) {
	mc.mutex.Lock()
	defer mc.mutex.Unlock()

	mc.clients = make(map[string]*Client) // Clear existing clients

	for _, amConfig := range cfg.Alertmanagers {
		client := NewClientFromConfig(amConfig)
		mc.clients[amConfig.Name] = client
	}
}

func (mc *MultiClient) GetClientByURL(url string) (*Client, bool) {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	for _, client := range mc.clients {
		if client.BaseURL == url {
			return client, true
		}
	}
	return nil, false
}
