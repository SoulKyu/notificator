package gui

import (
	"encoding/json"
	"image/color"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// CustomTheme wraps the default theme and allows us to control the variant
type CustomTheme struct {
	variant fyne.ThemeVariant
}

// NewCustomTheme creates a new custom theme with the specified variant
func NewCustomTheme(variant fyne.ThemeVariant) *CustomTheme {
	return &CustomTheme{variant: variant}
}

// Color returns theme colors based on the variant
func (t *CustomTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	return theme.DefaultTheme().Color(name, t.variant)
}

// Font returns theme fonts
func (t *CustomTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(style)
}

// Icon returns theme icons
func (t *CustomTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

// Size returns theme sizes
func (t *CustomTheme) Size(name fyne.ThemeSizeName) float32 {
	return theme.DefaultTheme().Size(name)
}

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
		aw.app.Settings().SetTheme(NewCustomTheme(theme.VariantDark))
		aw.themeVariant = "dark"
		if aw.themeBtn != nil {
			aw.themeBtn.SetText("Light")
		}
	} else {
		aw.app.Settings().SetTheme(NewCustomTheme(theme.VariantLight))
		aw.themeVariant = "light"
		if aw.themeBtn != nil {
			aw.themeBtn.SetText("Dark")
		}
	}

	// Save theme preference
	aw.saveThemePreference()

	// Refresh the table to update enhanced severity badges with new theme
	aw.refreshStyling()
}

func (aw *AlertsWindow) refreshStyling() {
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
				aw.app.Settings().SetTheme(NewCustomTheme(theme.VariantDark))
			} else {
				aw.app.Settings().SetTheme(NewCustomTheme(theme.VariantLight))
			}
		}
	}
}
