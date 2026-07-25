package alertmanager

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"sync"
	"time"

	"notificator/config"
	"notificator/internal/auth"
	"notificator/internal/models"
)

// ErrSilenceNotFound is returned when Alertmanager answers 404 for a silence ID, so callers
// can tell a stale ID apart from an upstream fault instead of matching on the error text.
var ErrSilenceNotFound = errors.New("silence not found")

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
		return nil, fmt.Errorf("silence with ID %s: %w", silenceID, ErrSilenceNotFound)
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
		return fmt.Errorf("silence with ID %s: %w", silenceID, ErrSilenceNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("alertmanager returned status %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

// healthCheckFilter keeps the health probe cheap: the alerts endpoint is the
// only one guaranteed to exist across Alertmanager, Cortex and Mimir, but a
// filter that cannot match anything makes it answer with an empty list instead
// of the full payload.
const healthCheckFilter = `alertname="__notificator_health_check__"`

func (c *Client) TestConnection() error {
	url := fmt.Sprintf("%s/api/v2/alerts?filter=%s", c.BaseURL, neturl.QueryEscape(healthCheckFilter))

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

	// Drain the body so the connection goes back to the keep-alive pool.
	_, _ = io.Copy(io.Discard, resp.Body)

	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (c *Client) DebugURL() {

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

type namedClient struct {
	name   string
	client *Client
}

type fanOutResult[T any] struct {
	name  string
	value T
	err   error
}

// snapshotClients copies the client map under RLock so fan-outs can do their
// HTTP work without holding mc.mutex, which would otherwise block
// UpdateFromConfig for the whole N x timeout window.
func (mc *MultiClient) snapshotClients() []namedClient {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	snapshot := make([]namedClient, 0, len(mc.clients))
	for name, client := range mc.clients {
		snapshot = append(snapshot, namedClient{name: name, client: client})
	}

	return snapshot
}

// fanOut calls fn against every client concurrently and returns the results in
// snapshot order, so the wall-clock cost is max(latency) rather than
// sum(latency). Concurrency is unbounded on purpose: the fleet size is the
// number of configured Alertmanagers, which is small by construction.
func fanOut[T any](clients []namedClient, fn func(*Client) (T, error)) []fanOutResult[T] {
	results := make([]fanOutResult[T], len(clients))

	var wg sync.WaitGroup
	for i, nc := range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := fn(nc.client)
			results[i] = fanOutResult[T]{name: nc.name, value: value, err: err}
		}()
	}
	wg.Wait()

	return results
}

func testConnections(clients []namedClient) []fanOutResult[struct{}] {
	return fanOut(clients, func(c *Client) (struct{}, error) {
		return struct{}{}, c.TestConnection()
	})
}

// FetchAllAlertsDetailed fetches alerts from every configured Alertmanager and
// reports per-source failures instead of collapsing them into a single error,
// so callers can tell a partial fetch from a genuinely empty one.
func (mc *MultiClient) FetchAllAlertsDetailed() ([]AlertWithSource, map[string]error) {
	var allAlerts []AlertWithSource
	failedSources := make(map[string]error)

	for _, result := range fanOut(mc.snapshotClients(), (*Client).FetchAlerts) {
		if result.err != nil {
			failedSources[result.name] = result.err
			continue
		}

		for _, alert := range result.value {
			allAlerts = append(allAlerts, AlertWithSource{
				Alert:  alert,
				Source: result.name,
			})
		}
	}

	return allAlerts, failedSources
}

func (mc *MultiClient) FetchAllAlerts() ([]AlertWithSource, error) {
	allAlerts, failedSources := mc.FetchAllAlertsDetailed()

	if len(failedSources) > 0 && len(allAlerts) == 0 { // If all clients failed, return the first error
		for name, err := range failedSources {
			return nil, fmt.Errorf("failed to fetch alerts from %s: %w", name, err)
		}
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

// FetchAllSilencesDetailed fetches silences from every configured Alertmanager and
// reports per-source failures instead of collapsing them into a single error, so an
// unreachable Alertmanager degrades that source instead of blanking the inventory.
func (mc *MultiClient) FetchAllSilencesDetailed() ([]SilenceWithSource, map[string]error) {
	var allSilences []SilenceWithSource
	failedSources := make(map[string]error)

	for _, result := range fanOut(mc.snapshotClients(), (*Client).FetchSilences) {
		if result.err != nil {
			failedSources[result.name] = result.err
			continue
		}

		for _, silence := range result.value {
			allSilences = append(allSilences, SilenceWithSource{
				Silence: silence,
				Source:  result.name,
			})
		}
	}

	return allSilences, failedSources
}

func (mc *MultiClient) TestAllConnections() map[string]error {
	results := make(map[string]error)

	for _, result := range testConnections(mc.snapshotClients()) {
		results[result.name] = result.err
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
	status := make(map[string]bool)
	for _, result := range testConnections(mc.snapshotClients()) {
		status[result.name] = result.err == nil
	}

	return status
}

func (mc *MultiClient) GetHealthyClients() map[string]*Client {
	clients := mc.snapshotClients()

	healthy := make(map[string]*Client)
	for i, result := range testConnections(clients) {
		if result.err == nil {
			healthy[result.name] = clients[i].client
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
