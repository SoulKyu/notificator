package auth

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/cookiejar"
	"os/exec"
	"runtime"
	"time"
)

// ProxyAuthManager handles authentication through an OAuth proxy
type ProxyAuthManager struct {
	alertmanagerURL string
	client          *http.Client
	cookieJar       *cookiejar.Jar
}

// NewProxyAuthManager creates a new proxy authentication manager
func NewProxyAuthManager(alertmanagerURL string) *ProxyAuthManager {
	jar, _ := cookiejar.New(nil)

	client := &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Allow redirects but limit to 10
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	return &ProxyAuthManager{
		alertmanagerURL: alertmanagerURL,
		client:          client,
		cookieJar:       jar,
	}
}

// Authenticate performs authentication through the OAuth proxy
func (pam *ProxyAuthManager) Authenticate(ctx context.Context) error {
	log.Println("Starting OAuth proxy authentication...")

	// First, try to access the Alertmanager API directly
	// If it redirects to OAuth, we'll handle that
	apiURL := fmt.Sprintf("%s/api/v2/alerts", pam.alertmanagerURL)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Notificator/1.0")

	resp, err := pam.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	// If we get a 200, we're already authenticated
	if resp.StatusCode == 200 {
		log.Println("Already authenticated with OAuth proxy")
		return nil
	}

	// If we get redirected to OAuth, we need to authenticate
	if resp.StatusCode == 302 || resp.StatusCode == 307 {
		location := resp.Header.Get("Location")
		if location != "" {
			log.Printf("Redirected to OAuth: %s", location)
			return pam.performBrowserAuth(location)
		}
	}

	// If we get 401/403, we need to authenticate
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		log.Println("Authentication required, opening browser...")
		return pam.performBrowserAuth(pam.alertmanagerURL)
	}

	return fmt.Errorf("unexpected response: %d %s", resp.StatusCode, resp.Status)
}

// performBrowserAuth opens a browser for OAuth authentication
func (pam *ProxyAuthManager) performBrowserAuth(authURL string) error {
	fmt.Printf("Opening browser for OAuth authentication...\n")
	fmt.Printf("Please authenticate in your browser and then return to this application.\n")
	fmt.Printf("URL: %s\n", authURL)

	// Open browser
	if err := pam.openBrowser(authURL); err != nil {
		log.Printf("Failed to open browser automatically: %v", err)
		fmt.Printf("Please manually open the URL above in your browser.\n")
	}

	// Wait for user to complete authentication
	fmt.Printf("\nPress Enter after you have completed authentication in your browser...")
	fmt.Scanln()

	// Test if authentication was successful
	return pam.testAuthentication()
}

// testAuthentication tests if we can now access the API
func (pam *ProxyAuthManager) testAuthentication() error {
	apiURL := fmt.Sprintf("%s/api/v2/alerts", pam.alertmanagerURL)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create test request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Notificator/1.0")

	resp, err := pam.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to test authentication: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		log.Println("✓ OAuth proxy authentication successful!")
		return nil
	}

	return fmt.Errorf("authentication failed: got status %d", resp.StatusCode)
}

// GetAuthenticatedClient returns an HTTP client with authentication cookies
func (pam *ProxyAuthManager) GetAuthenticatedClient() *http.Client {
	return pam.client
}

// openBrowser opens the default browser with the given URL
func (pam *ProxyAuthManager) openBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start"}
	case "darwin":
		cmd = "open"
	default: // "linux", "freebsd", "openbsd", "netbsd"
		cmd = "xdg-open"
	}
	args = append(args, url)
	return exec.Command(cmd, args...).Start()
}

// IsAuthenticated checks if we're currently authenticated
func (pam *ProxyAuthManager) IsAuthenticated() bool {
	apiURL := fmt.Sprintf("%s/api/v2/alerts", pam.alertmanagerURL)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return false
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Notificator/1.0")

	resp, err := pam.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == 200
}
