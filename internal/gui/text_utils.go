// internal/gui/text_utils.go
package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// DynamicLabel is a label that automatically truncates text based on available width
type DynamicLabel struct {
	widget.Label
	originalText string
	maxWidth     float32
}

// NewDynamicLabel creates a new dynamic label that truncates text automatically
func NewDynamicLabel(text string, maxWidth float32) *DynamicLabel {
	dl := &DynamicLabel{
		Label:        *widget.NewLabel(text),
		originalText: text,
		maxWidth:     maxWidth,
	}
	dl.ExtendBaseWidget(dl)
	dl.updateText()
	return dl
}

// SetMaxWidth updates the maximum width and re-truncates text
func (dl *DynamicLabel) SetMaxWidth(width float32) {
	dl.maxWidth = width
	dl.updateText()
}

// SetOriginalText updates the original text and re-truncates
func (dl *DynamicLabel) SetOriginalText(text string) {
	dl.originalText = text
	dl.updateText()
}

// updateText truncates the text to fit within the max width using actual text measurement
func (dl *DynamicLabel) updateText() {
	if dl.originalText == "" {
		dl.SetText("")
		return
	}

	// Add some padding to account for margins
	availableWidth := dl.maxWidth - 8.0 // More generous padding
	if availableWidth <= 20 {
		// Column too narrow, just show ellipsis
		dl.SetText("...")
		return
	}

	// Get text size using theme's text size
	textSize := theme.TextSize()
	textStyle := fyne.TextStyle{}

	// Check if the original text fits
	originalSize := fyne.MeasureText(dl.originalText, textSize, textStyle)
	if originalSize.Width <= availableWidth {
		dl.SetText(dl.originalText)
		return
	}

	// Text is too long, need to truncate
	ellipsis := "..."
	ellipsisSize := fyne.MeasureText(ellipsis, textSize, textStyle)
	availableForText := availableWidth - ellipsisSize.Width

	if availableForText <= 0 {
		dl.SetText(ellipsis)
		return
	}

	// Use a more efficient approach: estimate initial position and then adjust
	// Start with a reasonable estimate (6 pixels per character)
	estimatedChars := int(availableForText / 6.0)
	if estimatedChars >= len(dl.originalText) {
		estimatedChars = len(dl.originalText) - 1
	}
	if estimatedChars < 1 {
		estimatedChars = 1
	}

	// Test the estimated position
	for i := estimatedChars; i >= 1; i-- {
		subtext := dl.originalText[:i]
		subtextSize := fyne.MeasureText(subtext, textSize, textStyle)

		if subtextSize.Width <= availableForText {
			dl.SetText(subtext + ellipsis)
			return
		}
	}

	// Fallback to just ellipsis if nothing fits
	dl.SetText(ellipsis)
}

// Resize overrides the resize method to update text truncation
func (dl *DynamicLabel) Resize(size fyne.Size) {
	dl.Label.Resize(size)
	if size.Width != dl.maxWidth && size.Width > 0 {
		dl.SetMaxWidth(size.Width)
		// Force immediate refresh
		dl.Refresh()
	}
}

// Refresh overrides the refresh method to update text truncation
func (dl *DynamicLabel) Refresh() {
	dl.updateText()
	dl.Label.Refresh()
}

// truncateTextForColumn truncates text to fit within a column width (legacy function)
func truncateTextForColumn(text string, columnWidth float32) string {
	if text == "" {
		return text
	}

	// Estimate characters per pixel (rough approximation)
	const avgCharWidth = 8.0
	maxChars := int(columnWidth / avgCharWidth)

	// Minimum characters to show
	if maxChars < 5 {
		maxChars = 5
	}

	// Maximum characters to prevent extremely long text
	if maxChars > 100 {
		maxChars = 100
	}

	if len(text) <= maxChars {
		return text
	}

	// Truncate and add ellipsis
	return text[:maxChars-3] + "..."
}

// createTruncatedLabel creates a label with text truncated to fit the column width (legacy function)
func createTruncatedLabel(text string, columnWidth float32) *widget.Label {
	truncatedText := truncateTextForColumn(text, columnWidth)
	label := widget.NewLabel(truncatedText)
	label.Wrapping = fyne.TextWrapOff
	return label
}

// createDynamicLabel creates a dynamic label that updates when resized
func createDynamicLabel(text string, columnWidth float32) *DynamicLabel {
	dl := NewDynamicLabel(text, columnWidth)
	dl.Wrapping = fyne.TextWrapOff
	// Override the label's resize method to update immediately
	dl.ExtendBaseWidget(dl)
	return dl
}
