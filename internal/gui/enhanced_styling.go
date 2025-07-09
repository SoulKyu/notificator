// internal/gui/enhanced_styling.go
package gui

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"notificator/internal/models"
)

// SeverityColors defines the color scheme for different alert severities
type SeverityColors struct {
	BorderColor     color.Color
	BackgroundStart color.Color
	BackgroundEnd   color.Color
	TextColor       color.Color
}

// getSeverityColors returns the color scheme for a given severity
func getSeverityColors(severity string, isDarkTheme bool) SeverityColors {
	// Normalize severity to lowercase for consistent matching
	normalizedSeverity := strings.ToLower(severity)

	switch normalizedSeverity {
	case "critical":
		if isDarkTheme {
			return SeverityColors{
				BorderColor:     color.RGBA{220, 38, 38, 255},   // Red-600
				BackgroundStart: color.RGBA{127, 29, 29, 80},    // Red-900 with transparency
				BackgroundEnd:   color.RGBA{185, 28, 28, 40},    // Red-700 with transparency
				TextColor:       color.RGBA{254, 226, 226, 255}, // Red-100
			}
		} else {
			return SeverityColors{
				BorderColor:     color.RGBA{185, 28, 28, 255},   // Red-700
				BackgroundStart: color.RGBA{254, 226, 226, 120}, // Red-100 with transparency
				BackgroundEnd:   color.RGBA{252, 165, 165, 80},  // Red-300 with transparency
				TextColor:       color.RGBA{127, 29, 29, 255},   // Red-900
			}
		}
	case "warning":
		if isDarkTheme {
			return SeverityColors{
				BorderColor:     color.RGBA{245, 158, 11, 255},  // Amber-500
				BackgroundStart: color.RGBA{146, 64, 14, 80},    // Amber-900 with transparency
				BackgroundEnd:   color.RGBA{217, 119, 6, 40},    // Amber-700 with transparency
				TextColor:       color.RGBA{254, 243, 199, 255}, // Amber-100
			}
		} else {
			return SeverityColors{
				BorderColor:     color.RGBA{217, 119, 6, 255},   // Amber-700
				BackgroundStart: color.RGBA{254, 243, 199, 120}, // Amber-100 with transparency
				BackgroundEnd:   color.RGBA{252, 211, 77, 80},   // Amber-300 with transparency
				TextColor:       color.RGBA{146, 64, 14, 255},   // Amber-900
			}
		}
	case "info", "information":
		if isDarkTheme {
			return SeverityColors{
				BorderColor:     color.RGBA{59, 130, 246, 255},  // Blue-500
				BackgroundStart: color.RGBA{30, 58, 138, 80},    // Blue-900 with transparency
				BackgroundEnd:   color.RGBA{29, 78, 216, 40},    // Blue-700 with transparency
				TextColor:       color.RGBA{219, 234, 254, 255}, // Blue-100
			}
		} else {
			return SeverityColors{
				BorderColor:     color.RGBA{29, 78, 216, 255},   // Blue-700
				BackgroundStart: color.RGBA{219, 234, 254, 120}, // Blue-100 with transparency
				BackgroundEnd:   color.RGBA{147, 197, 253, 80},  // Blue-300 with transparency
				TextColor:       color.RGBA{30, 58, 138, 255},   // Blue-900
			}
		}
	default:
		if isDarkTheme {
			return SeverityColors{
				BorderColor:     color.RGBA{156, 163, 175, 255}, // Gray-400
				BackgroundStart: color.RGBA{55, 65, 81, 80},     // Gray-700 with transparency
				BackgroundEnd:   color.RGBA{75, 85, 99, 40},     // Gray-600 with transparency
				TextColor:       color.RGBA{243, 244, 246, 255}, // Gray-100
			}
		} else {
			return SeverityColors{
				BorderColor:     color.RGBA{107, 114, 128, 255}, // Gray-500
				BackgroundStart: color.RGBA{249, 250, 251, 120}, // Gray-50 with transparency
				BackgroundEnd:   color.RGBA{209, 213, 219, 80},  // Gray-300 with transparency
				TextColor:       color.RGBA{55, 65, 81, 255},    // Gray-700
			}
		}
	}
}

// EnhancedSeverityBadge creates a custom severity badge with gradient background and border
type EnhancedSeverityBadge struct {
	widget.BaseWidget
	text        string
	severity    string
	isDarkTheme bool
	colors      SeverityColors
	background  *canvas.LinearGradient
	border      *canvas.Rectangle
	textObj     *canvas.Text
	container   *fyne.Container
}

// NewEnhancedSeverityBadge creates a new enhanced severity badge
func NewEnhancedSeverityBadge(alert models.Alert, isDarkTheme bool) *EnhancedSeverityBadge {
	severity := alert.GetSeverity()
	var text string

	// Normalize severity for consistent text display
	normalizedSeverity := strings.ToLower(severity)

	switch normalizedSeverity {
	case "critical":
		text = "🔴 CRITICAL"
	case "warning":
		text = "🟡 WARNING"
	case "info", "information":
		text = "🔵 INFORMATION"
	case "unknown":
		text = "⚪ UNKNOWN"
	case "":
		text = "⚪ DEFAULT"
	default:
		text = "⚪ " + strings.ToUpper(severity)
	}

	badge := &EnhancedSeverityBadge{
		text:        text,
		severity:    severity,
		isDarkTheme: isDarkTheme,
		colors:      getSeverityColors(severity, isDarkTheme),
	}

	badge.ExtendBaseWidget(badge)
	badge.createObjects()
	return badge
}

// createObjects creates the visual components of the badge
func (b *EnhancedSeverityBadge) createObjects() {
	// Create gradient background
	b.background = canvas.NewLinearGradient(b.colors.BackgroundStart, b.colors.BackgroundEnd, 45)
	b.background.Resize(fyne.NewSize(140, 32))

	// Create border
	b.border = canvas.NewRectangle(color.Transparent)
	b.border.StrokeColor = b.colors.BorderColor
	b.border.StrokeWidth = 2
	b.border.Resize(fyne.NewSize(140, 32))

	// Create text
	b.textObj = canvas.NewText(b.text, b.colors.TextColor)
	b.textObj.Alignment = fyne.TextAlignCenter
	b.textObj.TextStyle = fyne.TextStyle{Bold: true}
	b.textObj.TextSize = 11
	b.textObj.Move(fyne.NewPos(8, 8))
	b.textObj.Resize(fyne.NewSize(124, 16))

	// Create container with layered objects
	b.container = container.NewWithoutLayout(
		b.background,
		b.border,
		b.textObj,
	)
	b.container.Resize(fyne.NewSize(140, 32))
}

// CreateRenderer creates the renderer for the enhanced severity badge
func (b *EnhancedSeverityBadge) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(b.container)
}

// Resize resizes the badge and all its components
func (b *EnhancedSeverityBadge) Resize(size fyne.Size) {
	b.BaseWidget.Resize(size)
	if b.background != nil {
		b.background.Resize(size)
	}
	if b.border != nil {
		b.border.Resize(size)
	}
	if b.textObj != nil {
		textSize := fyne.NewSize(size.Width-16, size.Height-16)
		b.textObj.Move(fyne.NewPos(8, (size.Height-textSize.Height)/2))
		b.textObj.Resize(textSize)
	}
	if b.container != nil {
		b.container.Resize(size)
	}
}

// Move moves the badge to a new position
func (b *EnhancedSeverityBadge) Move(pos fyne.Position) {
	b.BaseWidget.Move(pos)
	if b.container != nil {
		b.container.Move(pos)
	}
}

// MinSize returns the minimum size for the badge
func (b *EnhancedSeverityBadge) MinSize() fyne.Size {
	return fyne.NewSize(120, 28)
}

// UpdateTheme updates the badge colors when theme changes
func (b *EnhancedSeverityBadge) UpdateTheme(isDarkTheme bool) {
	b.isDarkTheme = isDarkTheme
	b.colors = getSeverityColors(b.severity, isDarkTheme)

	if b.background != nil {
		b.background.StartColor = b.colors.BackgroundStart
		b.background.EndColor = b.colors.BackgroundEnd
		b.background.Refresh()
	}

	if b.border != nil {
		b.border.StrokeColor = b.colors.BorderColor
		b.border.Refresh()
	}

	if b.textObj != nil {
		b.textObj.Color = b.colors.TextColor
		b.textObj.Refresh()
	}
}

// EnhancedAlertRow creates an enhanced alert row with severity-based styling
type EnhancedAlertRow struct {
	widget.BaseWidget
	alert       models.Alert
	isDarkTheme bool
	background  *canvas.Rectangle
	container   *fyne.Container
}

// NewEnhancedAlertRow creates a new enhanced alert row
func NewEnhancedAlertRow(alert models.Alert, isDarkTheme bool) *EnhancedAlertRow {
	row := &EnhancedAlertRow{
		alert:       alert,
		isDarkTheme: isDarkTheme,
	}

	row.ExtendBaseWidget(row)
	row.createBackground()
	return row
}

// createBackground creates a subtle background color based on alert severity
func (r *EnhancedAlertRow) createBackground() {
	severity := r.alert.GetSeverity()

	// Create a very subtle background color for the entire row
	var bgColor color.Color
	switch severity {
	case "critical":
		if r.isDarkTheme {
			bgColor = color.RGBA{127, 29, 29, 15} // Very subtle red
		} else {
			bgColor = color.RGBA{254, 226, 226, 30} // Very subtle red
		}
	case "warning":
		if r.isDarkTheme {
			bgColor = color.RGBA{146, 64, 14, 15} // Very subtle amber
		} else {
			bgColor = color.RGBA{254, 243, 199, 30} // Very subtle amber
		}
	default:
		bgColor = color.Transparent
	}

	r.background = canvas.NewRectangle(bgColor)
}

// CreateRenderer creates the renderer for the enhanced alert row
func (r *EnhancedAlertRow) CreateRenderer() fyne.WidgetRenderer {
	if r.container == nil {
		r.container = container.NewWithoutLayout(r.background)
	}
	return widget.NewSimpleRenderer(r.container)
}

// Resize resizes the row background
func (r *EnhancedAlertRow) Resize(size fyne.Size) {
	r.BaseWidget.Resize(size)
	if r.background != nil {
		r.background.Resize(size)
	}
	if r.container != nil {
		r.container.Resize(size)
	}
}

// Move moves the row to a new position
func (r *EnhancedAlertRow) Move(pos fyne.Position) {
	r.BaseWidget.Move(pos)
	if r.background != nil {
		r.background.Move(pos)
	}
	if r.container != nil {
		r.container.Move(pos)
	}
}

// UpdateTheme updates the row colors when theme changes
func (r *EnhancedAlertRow) UpdateTheme(isDarkTheme bool) {
	r.isDarkTheme = isDarkTheme
	r.createBackground()
	if r.container != nil {
		r.container.Objects[0] = r.background
		r.container.Refresh()
	}
}

// Helper function to determine if current theme is dark
func (aw *AlertsWindow) isDarkTheme() bool {
	return aw.themeVariant == "dark"
}

// createEnhancedSeverityBadgeNew creates the new enhanced severity badge
func (aw *AlertsWindow) createEnhancedSeverityBadgeNew(alert models.Alert) fyne.CanvasObject {
	return NewEnhancedSeverityBadge(alert, aw.isDarkTheme())
}
