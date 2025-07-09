package gui

import (
	"encoding/json"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// createThemeToggle creates the theme toggle button
func (aw *AlertsWindow) createThemeToggle() *widget.Button {
	// Create theme button with appropriate icon and text
	var icon fyne.Resource
	var text string
	if aw.themeVariant == "light" {
		icon = theme.VisibilityIcon() // Moon-like icon for switching to dark
		text = "Dark"
	} else {
		icon = theme.VisibilityOffIcon() // Sun-like icon for switching to light
		text = "Light"
	}

	themeBtn := widget.NewButtonWithIcon(text, icon, func() {
		aw.toggleTheme()
	})

	return themeBtn
}

// toggleTheme switches between light and dark themes
func (aw *AlertsWindow) toggleTheme() {
	if aw.themeVariant == "light" {
		aw.app.Settings().SetTheme(theme.DarkTheme())
		aw.themeVariant = "dark"
		aw.themeBtn.SetText("Light")
	} else {
		aw.app.Settings().SetTheme(theme.LightTheme())
		aw.themeVariant = "light"
		aw.themeBtn.SetText("Dark")
	}

	// Save theme preference
	aw.saveThemePreference()

	// Refresh the table to update enhanced severity badges with new theme
	aw.refreshEnhancedStyling()
}

// refreshEnhancedStyling refreshes the table to update enhanced styling elements
func (aw *AlertsWindow) refreshEnhancedStyling() {
	if aw.table != nil {
		aw.table.Refresh()
	}
}

// saveThemePreference saves the current theme to config
func (aw *AlertsWindow) saveThemePreference() {
	if aw.configPath == "" {
		return
	}

	type ThemeConfig struct {
		Theme string `json:"theme"`
	}

	config := ThemeConfig{Theme: aw.themeVariant}
	if data, err := json.MarshalIndent(config, "", "  "); err == nil {
		os.WriteFile(aw.configPath+".theme", data, 0644)
	}
}

// loadThemePreference loads the saved theme preference
func (aw *AlertsWindow) loadThemePreference() {
	if aw.configPath == "" {
		return
	}

	type ThemeConfig struct {
		Theme string `json:"theme"`
	}

	if data, err := os.ReadFile(aw.configPath + ".theme"); err == nil {
		var config ThemeConfig
		if json.Unmarshal(data, &config) == nil && config.Theme != "" {
			aw.themeVariant = config.Theme
			if aw.themeVariant == "dark" {
				aw.app.Settings().SetTheme(theme.DarkTheme())
			} else {
				aw.app.Settings().SetTheme(theme.LightTheme())
			}
		}
	}
}
