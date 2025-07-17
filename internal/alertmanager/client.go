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

// customHeaderRoundTripper adds custom headers to every request
type customHeaderRoundTripper struct {
	headers map[string]string
	rt      http.RoundTripper
}

// RoundTrip implements the RoundTripper interface
func (c *customHeaderRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request to avoid modifying the original
	req = req.Clone(req.Context())

	// Add custom headers
	for key, value := range c.headers {
		req.Header.Set(key, value)
	}

	return c.rt.RoundTrip(req)
}

// Client handles communication with Alertmanager
type Client struct {
	// Name is the friendly name of this Alertmanager instance
	Name string

	// BaseURL is the base URL of the Alertmanager instance
	BaseURL string

	// HTTPClient is the HTTP client used for requests
	HTTPClient *http.Client

	// Timeout for HTTP requests
	Timeout time.Duration

	// Authentication
	Username string
	Password string
	Token    string

	// Custom headers (for OAuth bypass, etc.)
	Headers map[string]string

	// Proxy auth manager for OAuth proxy authentication
	ProxyAuthManager *auth.ProxyAuthManager
}

// MultiClient manages multiple Alertmanager clients
type MultiClient struct {
	clients map[string]*Client
	mutex   sync.RWMutex
}

// NewClient creates a new Alertmanager client
func NewClient(baseURL string) *Client {
	return &Client{
		Name:    "default",
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		Timeout: 10 * time.Second,
		Headers: make(map[string]string),
	}
}

// DebugRequest performs a debug request to see redirects and headers
func (c *Client) DebugRequest(endpoint string) {
	url := fmt.Sprintf("%s%s", c.BaseURL, endpoint)
	fmt.Printf("Debug: Making request to %s\n", url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Printf("  ✗ Failed to create request: %v\n", err)
		return
	}

	// Add all headers
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Notificator/1.0")
	c.addAuth(req)

	// Show request headers
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

	// Make request without following redirects
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // Don't follow redirects
		},
	}

	// Copy headers for this debug client
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

	// Show response headers
	fmt.Println("Response headers:")
	for key, values := range resp.Header {
		for _, value := range values {
			fmt.Printf("  %s: %s\n", key, value)
		}
	}

	// Show redirect location if present
	if location := resp.Header.Get("Location"); location != "" {
		fmt.Printf("Redirect to: %s\n", location)
	}

	// Read response body
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

// TestOAuthBypass specifically tests the OAuth bypass functionality
func (c *Client) TestOAuthBypass() error {
	fmt.Println("=== Testing OAuth Bypass ===")

	// Check if we have OAuth bypass token
	bypassToken, hasToken := c.Headers["X-Oauth-Bypass-Token"]
	if !hasToken || bypassToken == "" {
		return fmt.Errorf("no X-Oauth-Bypass-Token found in headers")
	}

	fmt.Printf("Found OAuth bypass token (first 10 chars): %s...\n", bypassToken[:min(10, len(bypassToken))])

	// Test the bypass
	url := fmt.Sprintf("%s/api/v2/alerts", c.BaseURL)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Only add the bypass token (no other auth)
	req.Header.Set("X-Oauth-Bypass-Token", bypassToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Notificator/1.0")

	// Create a client that doesn't follow redirects
	client := &http.Client{
		Timeout: 10 * time.Second,
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

// NewClientWithAuth creates a new Alertmanager client with authentication
func NewClientWithAuth(baseURL, username, password, token string) *Client {
	return &Client{
		Name:    "default",
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		Timeout:  10 * time.Second,
		Username: username,
		Password: password,
		Token:    token,
		Headers:  make(map[string]string),
	}
}

// NewClientWithProxyAuth creates a new Alertmanager client with OAuth proxy support
func NewClientWithProxyAuth(baseURL string) *Client {
	proxyAuth := auth.NewProxyAuthManager(baseURL)

	return &Client{
		Name:             "default",
		BaseURL:          baseURL,
		HTTPClient:       proxyAuth.GetAuthenticatedClient(),
		Timeout:          10 * time.Second,
		Headers:          make(map[string]string),
		ProxyAuthManager: proxyAuth,
	}
}

// NewClientWithConfig creates a new Alertmanager client from config
func NewClientWithConfig(baseURL, username, password, token string, headers map[string]string, name string) *Client {
	// Create base HTTP client with redirect handling
	var httpClient *http.Client

	// Custom redirect policy to preserve headers
	checkRedirect := func(req *http.Request, via []*http.Request) error {
		// Preserve custom headers on redirect
		if len(via) > 0 {
			for key, value := range headers {
				req.Header.Set(key, value)
			}
			// Also preserve authentication
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			} else if username != "" && password != "" {
				req.SetBasicAuth(username, password)
			}
		}

		// Allow up to 10 redirects
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	}

	// If we have custom headers, wrap the transport
	if len(headers) > 0 {
		transport := &customHeaderRoundTripper{
			headers: headers,
			rt:      http.DefaultTransport,
		}

		httpClient = &http.Client{
			Timeout:       10 * time.Second,
			Transport:     transport,
			CheckRedirect: checkRedirect,
		}
	} else {
		httpClient = &http.Client{
			Timeout:       10 * time.Second,
			CheckRedirect: checkRedirect,
		}
	}

	return &Client{
		Name:       name,
		BaseURL:    baseURL,
		HTTPClient: httpClient,
		Timeout:    10 * time.Second,
		Username:   username,
		Password:   password,
		Token:      token,
		Headers:    headers,
	}
}

// NewMultiClient creates a new MultiClient from configuration
func NewMultiClient(cfg *config.Config) *MultiClient {
	mc := &MultiClient{
		clients: make(map[string]*Client),
	}

	// Create clients for each Alertmanager configuration
	for _, amConfig := range cfg.Alertmanagers {
		client := NewClientFromConfig(amConfig)
		mc.clients[amConfig.Name] = client
	}

	return mc
}

// NewClientFromConfig creates a new client from AlertmanagerConfig
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

// GetClient returns a client by name
func (mc *MultiClient) GetClient(name string) (*Client, bool) {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	client, exists := mc.clients[name]
	return client, exists
}

// GetAllClients returns all clients
func (mc *MultiClient) GetAllClients() map[string]*Client {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	// Create a copy to avoid race conditions
	clients := make(map[string]*Client)
	for name, client := range mc.clients {
		clients[name] = client
	}
	return clients
}

// AddClient adds a new client
func (mc *MultiClient) AddClient(amConfig config.AlertmanagerConfig) {
	mc.mutex.Lock()
	defer mc.mutex.Unlock()

	client := NewClientFromConfig(amConfig)
	mc.clients[amConfig.Name] = client
}

// RemoveClient removes a client by name
func (mc *MultiClient) RemoveClient(name string) {
	mc.mutex.Lock()
	defer mc.mutex.Unlock()

	delete(mc.clients, name)
}

// UpdateClient updates an existing client
func (mc *MultiClient) UpdateClient(amConfig config.AlertmanagerConfig) {
	mc.mutex.Lock()
	defer mc.mutex.Unlock()

	client := NewClientFromConfig(amConfig)
	mc.clients[amConfig.Name] = client
}

// addAuth adds authentication to the request
func (c *Client) addAuth(req *http.Request) {
	// Add authentication methods
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	} else if c.Username != "" && c.Password != "" {
		req.SetBasicAuth(c.Username, c.Password)
	}
}

// FetchAlerts retrieves all alerts from Alertmanager using v2 API
func (c *Client) FetchAlerts() ([]models.Alert, error) {
	// Use v2 API endpoint
	url := fmt.Sprintf("%s/api/v2/alerts", c.BaseURL)

	// Create HTTP request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Notificator/1.0")

	// Add authentication if configured
	c.addAuth(req)

	// Execute the request
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// Check if request was successful
	if resp.StatusCode != http.StatusOK {
		// Read response body for debugging
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("alertmanager returned status %d, body: %s", resp.StatusCode, string(body))
	}

	// Read the entire response body first for debugging
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check if the response looks like JSON
	if len(body) > 0 && body[0] == '<' {
		return nil, fmt.Errorf("received HTML response instead of JSON. Response: %s", string(body[:min(500, len(body))]))
	}

	// v2 API returns alerts directly as an array
	var alerts []models.Alert
	if err := json.Unmarshal(body, &alerts); err != nil {
		return nil, fmt.Errorf("failed to decode v2 response: %w. Response was: %s", err, string(body[:min(200, len(body))]))
	}

	return alerts, nil
}

// FetchActiveAlerts retrieves only active (firing) alerts
func (c *Client) FetchActiveAlerts() ([]models.Alert, error) {
	allAlerts, err := c.FetchAlerts()
	if err != nil {
		return nil, err
	}

	// Filter for active alerts only
	var activeAlerts []models.Alert
	for _, alert := range allAlerts {
		if alert.IsActive() {
			activeAlerts = append(activeAlerts, alert)
		}
	}

	return activeAlerts, nil
}

// FetchSilence retrieves a specific silence by ID from Alertmanager
func (c *Client) FetchSilence(silenceID string) (*models.Silence, error) {
	// Use v2 API endpoint for specific silence
	url := fmt.Sprintf("%s/api/v2/silence/%s", c.BaseURL, silenceID)

	// Create HTTP request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Notificator/1.0")

	// Add authentication if configured
	c.addAuth(req)

	// Execute the request
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// Check if request was successful
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("silence with ID %s not found", silenceID)
	}
	if resp.StatusCode != http.StatusOK {
		// Read response body for debugging
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("alertmanager returned status %d, body: %s", resp.StatusCode, string(body))
	}

	// Read the entire response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check if the response looks like JSON
	if len(body) > 0 && body[0] == '<' {
		return nil, fmt.Errorf("received HTML response instead of JSON. Response: %s", string(body[:min(500, len(body))]))
	}

	// Parse the silence
	var silence models.Silence
	if err := json.Unmarshal(body, &silence); err != nil {
		return nil, fmt.Errorf("failed to decode silence response: %w. Response was: %s", err, string(body[:min(200, len(body))]))
	}

	return &silence, nil
}

// FetchSilences retrieves all silences from Alertmanager
func (c *Client) FetchSilences() ([]models.Silence, error) {
	// Use v2 API endpoint for silences
	url := fmt.Sprintf("%s/api/v2/silences", c.BaseURL)

	// Create HTTP request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Notificator/1.0")

	// Add authentication if configured
	c.addAuth(req)

	// Execute the request
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// Check if request was successful
	if resp.StatusCode != http.StatusOK {
		// Read response body for debugging
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("alertmanager returned status %d, body: %s", resp.StatusCode, string(body))
	}

	// Read the entire response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check if the response looks like JSON
	if len(body) > 0 && body[0] == '<' {
		return nil, fmt.Errorf("received HTML response instead of JSON. Response: %s", string(body[:min(500, len(body))]))
	}

	// Parse the silences
	var silences []models.Silence
	if err := json.Unmarshal(body, &silences); err != nil {
		return nil, fmt.Errorf("failed to decode silences response: %w. Response was: %s", err, string(body[:min(200, len(body))]))
	}

	return silences, nil
}

// CreateSilence creates a new silence in Alertmanager
func (c *Client) CreateSilence(silence models.Silence) (*models.Silence, error) {
	// Use v2 API endpoint for creating silences
	url := fmt.Sprintf("%s/api/v2/silences", c.BaseURL)

	// Marshal the silence to JSON
	jsonData, err := json.Marshal(silence)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal silence: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Notificator/1.0")

	// Add authentication if configured
	c.addAuth(req)

	// Execute the request
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// Read the entire response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check if request was successful
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("alertmanager returned status %d, body: %s", resp.StatusCode, string(body))
	}

	// Check if the response looks like JSON
	if len(body) > 0 && body[0] == '<' {
		return nil, fmt.Errorf("received HTML response instead of JSON. Response: %s", string(body[:min(500, len(body))]))
	}

	// According to the API spec, the response should contain silenceID
	var response struct {
		SilenceID string `json:"silenceID"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode silence response: %w. Response was: %s", err, string(body[:min(200, len(body))]))
	}

	// Return the silence with the assigned ID
	createdSilence := silence
	createdSilence.ID = response.SilenceID

	return &createdSilence, nil
}

// TestConnection tests if we can connect to Alertmanager using v2 API
func (c *Client) TestConnection() error {
	// v2 API doesn't have a dedicated status endpoint, so we'll test the alerts endpoint
	url := fmt.Sprintf("%s/api/v2/alerts", c.BaseURL)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication if configured
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

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// DebugURL prints the full URL being used (for debugging)
func (c *Client) DebugURL() {
	fmt.Printf("Debug: Using Alertmanager URL: %s/api/v2/alerts\n", c.BaseURL)
}

// TestAPIEndpoints tests different API endpoints to find the correct one
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

// AlertWithSource represents an alert with its source Alertmanager
type AlertWithSource struct {
	Alert  models.Alert
	Source string // Name of the Alertmanager instance
}

// SilenceWithSource represents a silence with its source Alertmanager
type SilenceWithSource struct {
	Silence models.Silence
	Source  string // Name of the Alertmanager instance
}

// FetchAllAlerts retrieves alerts from all Alertmanagers
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

	// If all clients failed, return the first error
	if len(errors) > 0 && len(allAlerts) == 0 {
		return nil, errors[0]
	}

	return allAlerts, nil
}

// FetchAllActiveAlerts retrieves only active alerts from all Alertmanagers
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

// FetchAllSilences retrieves silences from all Alertmanagers
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

	// If all clients failed, return the first error
	if len(errors) > 0 && len(allSilences) == 0 {
		return nil, errors[0]
	}

	return allSilences, nil
}

// TestAllConnections tests connectivity to all Alertmanagers
func (mc *MultiClient) TestAllConnections() map[string]error {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	results := make(map[string]error)

	for name, client := range mc.clients {
		results[name] = client.TestConnection()
	}

	return results
}

// CreateSilenceOnAlertmanager creates a silence on a specific Alertmanager
func (mc *MultiClient) CreateSilenceOnAlertmanager(alertmanagerName string, silence models.Silence) (*models.Silence, error) {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	client, exists := mc.clients[alertmanagerName]
	if !exists {
		return nil, fmt.Errorf("alertmanager '%s' not found", alertmanagerName)
	}

	return client.CreateSilence(silence)
}

// FetchSilenceFromAlertmanager retrieves a specific silence from a specific Alertmanager
func (mc *MultiClient) FetchSilenceFromAlertmanager(alertmanagerName, silenceID string) (*models.Silence, error) {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	client, exists := mc.clients[alertmanagerName]
	if !exists {
		return nil, fmt.Errorf("alertmanager '%s' not found", alertmanagerName)
	}

	return client.FetchSilence(silenceID)
}

// GetClientNames returns the names of all configured Alertmanagers
func (mc *MultiClient) GetClientNames() []string {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	names := make([]string, 0, len(mc.clients))
	for name := range mc.clients {
		names = append(names, name)
	}

	return names
}

// GetName returns the name of the Alertmanager client
func (c *Client) GetName() string {
	return c.Name
}

// SetName sets the name of the Alertmanager client
func (c *Client) SetName(name string) {
	c.Name = name
}

// String returns a string representation of the client
func (c *Client) String() string {
	return fmt.Sprintf("Alertmanager{Name: %s, URL: %s}", c.Name, c.BaseURL)
}

// IsHealthy checks if the Alertmanager is healthy
func (c *Client) IsHealthy() bool {
	return c.TestConnection() == nil
}

// GetHealthStatus returns the health status of all Alertmanagers
func (mc *MultiClient) GetHealthStatus() map[string]bool {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	status := make(map[string]bool)
	for name, client := range mc.clients {
		status[name] = client.IsHealthy()
	}

	return status
}

// GetHealthyClients returns only the healthy clients
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

// Count returns the number of configured Alertmanagers
func (mc *MultiClient) Count() int {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	return len(mc.clients)
}

// MigrateFromSingleClient helps migrate from single client usage to MultiClient
// This function can be used when upgrading from single Alertmanager to multiple
func MigrateFromSingleClient(oldClient *Client) *MultiClient {
	mc := &MultiClient{
		clients: make(map[string]*Client),
	}

	// Use the old client's name or default
	name := oldClient.Name
	if name == "" {
		name = "Default"
	}

	mc.clients[name] = oldClient
	return mc
}

// UpdateFromConfig updates the MultiClient configuration
func (mc *MultiClient) UpdateFromConfig(cfg *config.Config) {
	mc.mutex.Lock()
	defer mc.mutex.Unlock()

	// Clear existing clients
	mc.clients = make(map[string]*Client)

	// Create new clients from configuration
	for _, amConfig := range cfg.Alertmanagers {
		client := NewClientFromConfig(amConfig)
		mc.clients[amConfig.Name] = client
	}
}

// GetClientByURL returns a client by its URL (for backward compatibility)
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
