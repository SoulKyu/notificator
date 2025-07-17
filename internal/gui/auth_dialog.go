// internal/gui/auth_dialog.go
package gui

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type AuthDialog struct {
	window       fyne.Window
	alertsWindow *AlertsWindow
	dialog       *dialog.CustomDialog
	loginTab     *container.TabItem
	registerTab  *container.TabItem

	// Login form fields
	loginUsernameEntry *widget.Entry
	loginPasswordEntry *widget.Entry
	loginButton        *widget.Button
	rememberMeCheck    *widget.Check

	// Register form fields
	registerUsernameEntry        *widget.Entry
	registerEmailEntry           *widget.Entry
	registerPasswordEntry        *widget.Entry
	registerConfirmPasswordEntry *widget.Entry
	registerButton               *widget.Button

	// Status and loading
	statusLabel  *widget.Label
	progressBar  *widget.ProgressBar
	isProcessing bool
}

func NewAuthDialog(alertsWindow *AlertsWindow) *AuthDialog {
	return &AuthDialog{
		window:       alertsWindow.window,
		alertsWindow: alertsWindow,
		statusLabel:  widget.NewLabel(""),
	}
}

func (aw *AlertsWindow) showAuthDialog() {
	// Check if backend is available
	if aw.backendClient == nil || !aw.backendClient.IsConnected() {
		dialog.ShowError(fmt.Errorf("backend server not available"), aw.window)
		return
	}

	// Create auth dialog
	authDialog := NewAuthDialog(aw)
	authDialog.Show()
}

func (ad *AuthDialog) Show() {
	// Create tabs container
	tabs := container.NewAppTabs()

	// Create login and register tabs
	ad.createLoginTab()
	ad.createRegisterTab()

	tabs.Append(ad.loginTab)
	tabs.Append(ad.registerTab)

	// Create main container with status
	content := container.NewVBox(
		tabs,
		widget.NewSeparator(),
		ad.statusLabel,
		ad.createProgressBar(),
	)

	// Create dialog
	ad.dialog = dialog.NewCustom("Authentication", "Cancel", content, ad.window)
	ad.dialog.Resize(fyne.NewSize(450, 380))

	// Set up dialog close handler
	ad.dialog.SetOnClosed(func() {
		ad.cleanup()
	})

	// Load and pre-fill stored credentials if they exist
	ad.loadAndFillCredentials()

	ad.dialog.Show()
}

func (ad *AuthDialog) createLoginTab() {
	ad.loginUsernameEntry = widget.NewEntry()
	ad.loginUsernameEntry.SetPlaceHolder("Enter username")
	ad.loginUsernameEntry.OnChanged = func(text string) {
		ad.updateLoginButtonState()
	}

	ad.loginPasswordEntry = widget.NewPasswordEntry()
	ad.loginPasswordEntry.SetPlaceHolder("Enter password")
	ad.loginPasswordEntry.OnChanged = func(text string) {
		ad.updateLoginButtonState()
	}

	// Enable login on Enter key
	ad.loginPasswordEntry.OnSubmitted = func(text string) {
		if ad.canLogin() {
			ad.performLogin()
		}
	}

	ad.loginButton = widget.NewButton("Login", func() {
		ad.performLogin()
	})
	ad.loginButton.Importance = widget.HighImportance
	ad.loginButton.Disable() // Initially disabled

	// Remember me checkbox
	ad.rememberMeCheck = widget.NewCheck("Remember me", nil)
	ad.rememberMeCheck.SetChecked(false)

	// Help text
	helpText := widget.NewRichTextFromMarkdown(`
**Login to access collaborative features:**
• Add comments to alerts
• Acknowledge alerts for your team
• View team activity and discussions
	`)
	helpText.Wrapping = fyne.TextWrapWord

	loginForm := container.NewVBox(
		helpText,
		widget.NewSeparator(),

		container.NewVBox(
			widget.NewLabel("Username:"),
			ad.loginUsernameEntry,
		),

		container.NewVBox(
			widget.NewLabel("Password:"),
			ad.loginPasswordEntry,
		),

		widget.NewSeparator(),
		ad.rememberMeCheck,
		ad.loginButton,
	)

	ad.loginTab = container.NewTabItem("Login", loginForm)
	ad.loginTab.Icon = theme.LoginIcon()
}

func (ad *AuthDialog) createRegisterTab() {
	ad.registerUsernameEntry = widget.NewEntry()
	ad.registerUsernameEntry.SetPlaceHolder("Choose a username")
	ad.registerUsernameEntry.OnChanged = func(text string) {
		ad.updateRegisterButtonState()
	}

	ad.registerEmailEntry = widget.NewEntry()
	ad.registerEmailEntry.SetPlaceHolder("your@email.com (optional)")
	ad.registerEmailEntry.OnChanged = func(text string) {
		ad.updateRegisterButtonState()
	}

	ad.registerPasswordEntry = widget.NewPasswordEntry()
	ad.registerPasswordEntry.SetPlaceHolder("Choose a password")
	ad.registerPasswordEntry.OnChanged = func(text string) {
		ad.updateRegisterButtonState()
	}

	ad.registerConfirmPasswordEntry = widget.NewPasswordEntry()
	ad.registerConfirmPasswordEntry.SetPlaceHolder("Confirm password")
	ad.registerConfirmPasswordEntry.OnChanged = func(text string) {
		ad.updateRegisterButtonState()
	}

	// Enable register on Enter key from confirm password field
	ad.registerConfirmPasswordEntry.OnSubmitted = func(text string) {
		if ad.canRegister() {
			ad.performRegister()
		}
	}

	ad.registerButton = widget.NewButton("Create Account", func() {
		ad.performRegister()
	})
	ad.registerButton.Importance = widget.HighImportance
	ad.registerButton.Disable() // Initially disabled

	// Password requirements help
	passwordHelp := widget.NewRichTextFromMarkdown(`
**Password Requirements:**
• Minimum 4 characters
• Passwords must match
	`)
	passwordHelp.Wrapping = fyne.TextWrapWord

	registerForm := container.NewVBox(
		container.NewVBox(
			widget.NewLabel("Username:"),
			ad.registerUsernameEntry,
		),

		container.NewVBox(
			widget.NewLabel("Email (optional):"),
			ad.registerEmailEntry,
		),

		container.NewVBox(
			widget.NewLabel("Password:"),
			ad.registerPasswordEntry,
		),

		container.NewVBox(
			widget.NewLabel("Confirm Password:"),
			ad.registerConfirmPasswordEntry,
		),

		passwordHelp,
		widget.NewSeparator(),
		ad.registerButton,
	)

	ad.registerTab = container.NewTabItem("Register", registerForm)
	ad.registerTab.Icon = theme.ContentAddIcon()
}

func (ad *AuthDialog) createProgressBar() *widget.ProgressBar {
	ad.progressBar = widget.NewProgressBar()
	ad.progressBar.Hide()
	return ad.progressBar
}

func (ad *AuthDialog) updateLoginButtonState() {
	if ad.loginButton != nil {
		if ad.canLogin() {
			ad.loginButton.Enable()
		} else {
			ad.loginButton.Disable()
		}
	}
}

func (ad *AuthDialog) updateRegisterButtonState() {
	if ad.registerButton != nil {
		if ad.canRegister() {
			ad.registerButton.Enable()
		} else {
			ad.registerButton.Disable()
		}
	}
}

func (ad *AuthDialog) canLogin() bool {
	return !ad.isProcessing &&
		strings.TrimSpace(ad.loginUsernameEntry.Text) != "" &&
		strings.TrimSpace(ad.loginPasswordEntry.Text) != ""
}

func (ad *AuthDialog) canRegister() bool {
	username := strings.TrimSpace(ad.registerUsernameEntry.Text)
	password := ad.registerPasswordEntry.Text
	confirmPassword := ad.registerConfirmPasswordEntry.Text

	return !ad.isProcessing &&
		username != "" &&
		len(password) >= 4 &&
		password == confirmPassword
}

func (ad *AuthDialog) performLogin() {
	if !ad.canLogin() {
		return
	}

	// Validate that we have a backend client
	if ad.alertsWindow.backendClient == nil {
		ad.showError("Backend client not available")
		return
	}

	username := strings.TrimSpace(ad.loginUsernameEntry.Text)
	password := ad.loginPasswordEntry.Text

	ad.setProcessing(true, "Logging in...")

	// Perform login in goroutine using BackendClient
	go func() {
		resp, err := ad.alertsWindow.backendClient.Login(username, password)

		// Update UI on main thread
		ad.alertsWindow.scheduleUpdate(func() {
			ad.setProcessing(false, "")

			if err != nil {
				ad.showError(fmt.Sprintf("Login failed: %v", err))
				return
			}

			if !resp.Success {
				ad.showError(fmt.Sprintf("Login failed: %s", resp.Message))
				return
			}

			// Success - user is now authenticated through BackendClient
			ad.showSuccess(fmt.Sprintf("Welcome back, %s!", resp.User.Username))

			// Save credentials if remember me is checked
			if ad.rememberMeCheck.Checked {
				if err := ad.saveCredentials(username, password, true); err != nil {
					log.Printf("Failed to save credentials: %v", err)
				}
			} else {
				// Clear any existing credentials if remember me is unchecked
				if err := ad.deleteCredentials(); err != nil {
					log.Printf("Failed to clear credentials: %v", err)
				}
			}

			// Update main window UI to reflect authentication state
			ad.alertsWindow.updateUserInterface()

			// Close dialog after a brief delay
			time.AfterFunc(1*time.Second, func() {
				ad.alertsWindow.scheduleUpdate(func() {
					ad.dialog.Hide()
				})
			})
		})
	}()
}

func (ad *AuthDialog) performRegister() {
	if !ad.canRegister() {
		return
	}

	// Validate that we have a backend client
	if ad.alertsWindow.backendClient == nil {
		ad.showError("Backend client not available")
		return
	}

	username := strings.TrimSpace(ad.registerUsernameEntry.Text)
	email := strings.TrimSpace(ad.registerEmailEntry.Text)
	password := ad.registerPasswordEntry.Text

	ad.setProcessing(true, "Creating account...")

	// Perform registration in goroutine using BackendClient
	go func() {
		resp, err := ad.alertsWindow.backendClient.Register(username, password, email)

		// Update UI on main thread
		ad.alertsWindow.scheduleUpdate(func() {
			ad.setProcessing(false, "")

			if err != nil {
				ad.showError(fmt.Sprintf("Registration failed: %v", err))
				return
			}

			if !resp.Success {
				ad.showError(fmt.Sprintf("Registration failed: %s", resp.Message))
				return
			}

			// Success
			ad.showSuccess("Account created successfully! You can now log in.")

			// Clear register form and switch to login tab
			ad.clearRegisterForm()

			// Note: We don't auto-login, user should manually login
		})
	}()
}

func (ad *AuthDialog) setProcessing(processing bool, message string) {
	ad.isProcessing = processing

	if processing {
		ad.statusLabel.SetText(message)
		ad.progressBar.Show()

		// Disable all buttons
		ad.loginButton.Disable()
		ad.registerButton.Disable()
	} else {
		ad.progressBar.Hide()

		// Re-enable buttons based on form state
		ad.updateLoginButtonState()
		ad.updateRegisterButtonState()
	}
}

func (ad *AuthDialog) showError(message string) {
	ad.statusLabel.SetText("❌ " + message)
	ad.statusLabel.Importance = widget.DangerImportance

	// Clear error after 5 seconds
	time.AfterFunc(5*time.Second, func() {
		ad.alertsWindow.scheduleUpdate(func() {
			ad.statusLabel.SetText("")
			ad.statusLabel.Importance = widget.MediumImportance
		})
	})
}

func (ad *AuthDialog) showSuccess(message string) {
	ad.statusLabel.SetText("✅ " + message)
	ad.statusLabel.Importance = widget.SuccessImportance
}

func (ad *AuthDialog) clearRegisterForm() {
	ad.registerUsernameEntry.SetText("")
	ad.registerEmailEntry.SetText("")
	ad.registerPasswordEntry.SetText("")
	ad.registerConfirmPasswordEntry.SetText("")
}

func (ad *AuthDialog) cleanup() {
	if ad.progressBar != nil {
		ad.progressBar.Hide()
	}

	log.Println("Authentication dialog closed")
}

// Additional helper methods for the AlertsWindow

// updateUserInterface updates the UI to reflect authentication state
func (aw *AlertsWindow) updateUserInterface() {
	// This method updates the UI based on whether user is logged in
	if aw.backendClient != nil && aw.backendClient.IsLoggedIn() {
		log.Println("User authenticated - updating UI to show collaborative features")

		// Here you would typically:
		// - Show comment/acknowledgment buttons in alert details
		// - Update menu items to show logout option
		// - Enable collaborative features

		// For now, just show a status message
		aw.setStatus("✅ Connected to backend - collaborative features enabled")
	} else {
		log.Println("User not authenticated - hiding collaborative features")

		// Hide collaborative features
		aw.setStatus("Backend available - login to enable collaborative features")
	}
}

// Helper method to check if user is authenticated
func (aw *AlertsWindow) isUserAuthenticated() bool {
	return aw.backendClient != nil && aw.backendClient.IsLoggedIn()
}

// Helper method to get current user info from BackendClient
func (aw *AlertsWindow) getCurrentUser() *User {
	if !aw.isUserAuthenticated() {
		return nil
	}

	// Get user from BackendClient
	backendUser := aw.backendClient.GetCurrentUser()
	if backendUser == nil {
		return nil
	}

	// Convert from backend user type to local User type
	return &User{
		ID:       backendUser.Id,
		Username: backendUser.Username,
		Email:    backendUser.Email,
	}
}

// User represents a simplified user info structure for UI purposes
type User struct {
	ID       string
	Username string
	Email    string
}

// showLogoutDialog displays a logout confirmation dialog
func (aw *AlertsWindow) showLogoutDialog() {
	if !aw.isUserAuthenticated() {
		return
	}

	user := aw.getCurrentUser()
	if user == nil {
		return
	}

	confirmDialog := dialog.NewConfirm("Logout", fmt.Sprintf("Are you sure you want to logout %s?", user.Username), func(confirmed bool) {
		if confirmed {
			aw.performLogout()
		}
	}, aw.window)

	confirmDialog.Show()
}

// performLogout handles the logout process
func (aw *AlertsWindow) performLogout() {
	if aw.backendClient == nil {
		return
	}

	go func() {
		// Call logout on BackendClient
		err := aw.backendClient.Logout()

		fyne.Do(func() {
			if err != nil {
				log.Printf("Logout error: %v", err)
				// Even if logout fails on server, clear local state
			}

			// Clear any stored credentials on logout
			authDialog := NewAuthDialog(aw)
			if err := authDialog.deleteCredentials(); err != nil {
				log.Printf("Failed to clear stored credentials: %v", err)
			}

			// Update UI to reflect logged out state
			aw.updateUserInterface()
			aw.setStatus("Logged out successfully")
		})
	}()
}

// StoredCredentials represents stored user credentials
type StoredCredentials struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	RememberMe  bool   `json:"remember_me"`
	LastLogin   int64  `json:"last_login"`
}

// getCredentialsPath returns the path to the stored credentials file
func (ad *AuthDialog) getCredentialsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".notificator_credentials")
	}
	return filepath.Join(home, ".config", "notificator", "credentials.enc")
}

// generateKey generates a key from the machine-specific information
func (ad *AuthDialog) generateKey() []byte {
	// Use hostname as part of key generation for machine-specific encryption
	hostname, _ := os.Hostname()
	hash := sha256.Sum256([]byte("notificator-" + hostname + "-credentials"))
	return hash[:]
}

// saveCredentials saves user credentials securely
func (ad *AuthDialog) saveCredentials(username, password string, rememberMe bool) error {
	if !rememberMe {
		// If not remembering, delete any existing credentials
		return ad.deleteCredentials()
	}

	credentials := StoredCredentials{
		Username:   username,
		Password:   password,
		RememberMe: rememberMe,
		LastLogin:  time.Now().Unix(),
	}

	// Serialize credentials
	jsonData, err := json.Marshal(credentials)
	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}

	// Encrypt the data
	encryptedData, err := ad.encryptData(jsonData)
	if err != nil {
		return fmt.Errorf("failed to encrypt credentials: %w", err)
	}

	// Ensure directory exists
	credPath := ad.getCredentialsPath()
	if err := os.MkdirAll(filepath.Dir(credPath), 0700); err != nil {
		return fmt.Errorf("failed to create credentials directory: %w", err)
	}

	// Write encrypted data to file
	if err := os.WriteFile(credPath, encryptedData, 0600); err != nil {
		return fmt.Errorf("failed to write credentials file: %w", err)
	}

	log.Printf("Credentials saved successfully for user: %s", username)
	return nil
}

// loadCredentials loads stored credentials
func (ad *AuthDialog) loadCredentials() (*StoredCredentials, error) {
	credPath := ad.getCredentialsPath()
	
	// Check if file exists
	if _, err := os.Stat(credPath); os.IsNotExist(err) {
		return nil, nil // No credentials stored
	}

	// Read encrypted file
	encryptedData, err := os.ReadFile(credPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read credentials file: %w", err)
	}

	// Decrypt the data
	decryptedData, err := ad.decryptData(encryptedData)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt credentials: %w", err)
	}

	// Deserialize credentials
	var credentials StoredCredentials
	if err := json.Unmarshal(decryptedData, &credentials); err != nil {
		return nil, fmt.Errorf("failed to unmarshal credentials: %w", err)
	}

	return &credentials, nil
}

// deleteCredentials removes stored credentials
func (ad *AuthDialog) deleteCredentials() error {
	credPath := ad.getCredentialsPath()
	if err := os.Remove(credPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete credentials: %w", err)
	}
	return nil
}

// encryptData encrypts data using AES-GCM
func (ad *AuthDialog) encryptData(data []byte) ([]byte, error) {
	key := ad.generateKey()
	
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nonce, nonce, data, nil)
	return []byte(base64.StdEncoding.EncodeToString(ciphertext)), nil
}

// decryptData decrypts data using AES-GCM
func (ad *AuthDialog) decryptData(encryptedData []byte) ([]byte, error) {
	key := ad.generateKey()
	
	// Decode from base64
	ciphertext, err := base64.StdEncoding.DecodeString(string(encryptedData))
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}

// loadAndFillCredentials loads stored credentials and pre-fills the login form
func (ad *AuthDialog) loadAndFillCredentials() {
	credentials, err := ad.loadCredentials()
	if err != nil {
		log.Printf("Failed to load credentials: %v", err)
		return
	}

	if credentials != nil && credentials.RememberMe {
		// Pre-fill the login form
		ad.loginUsernameEntry.SetText(credentials.Username)
		ad.loginPasswordEntry.SetText(credentials.Password)
		ad.rememberMeCheck.SetChecked(true)
		
		// Update button state
		ad.updateLoginButtonState()
		
		log.Printf("Loaded credentials for user: %s", credentials.Username)
	}
}
