package gui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// showColumnSettings displays the column width configuration dialog
func (aw *AlertsWindow) showColumnSettings() {
	// Create sliders for each column
	var sliders []*widget.Slider
	var labels []*widget.Label
	var resetBtns []*widget.Button

	content := container.NewVBox()

	// Header
	title := widget.NewLabelWithStyle("Column Width Settings", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	content.Add(title)
	content.Add(widget.NewSeparator())

	// Create controls for each column
	for i, col := range aw.columns {
		colIndex := i // Capture for closure

		// Column name label
		nameLabel := widget.NewLabelWithStyle(col.Name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})

		// Current width display
		widthLabel := widget.NewLabel(fmt.Sprintf("%.0f px", col.Width))
		labels = append(labels, widthLabel)

		// Width slider
		slider := widget.NewSlider(float64(col.MinWidth), float64(col.MaxWidth))
		slider.SetValue(float64(col.Width))
		slider.Step = 10
		slider.OnChanged = func(value float64) {
			aw.columns[colIndex].Width = float32(value)
			labels[colIndex].SetText(fmt.Sprintf("%.0f px", value))
			aw.applyColumnWidths()
			if aw.table != nil {
				aw.table.Refresh()
			}
			// Auto-save column configuration
			aw.saveColumnConfig()
		}
		sliders = append(sliders, slider)

		// Reset button for this column
		resetBtn := widget.NewButtonWithIcon("Reset", theme.ViewRefreshIcon(), func() {
			defaultCols := getDefaultColumns()
			aw.columns[colIndex].Width = defaultCols[colIndex].Width
			slider.SetValue(float64(defaultCols[colIndex].Width))
			labels[colIndex].SetText(fmt.Sprintf("%.0f px", defaultCols[colIndex].Width))
			aw.applyColumnWidths()
			if aw.table != nil {
				aw.table.Refresh()
			}
		})
		resetBtns = append(resetBtns, resetBtn)

		// Layout for this column
		colContainer := container.NewBorder(
			nameLabel,  // Top
			nil,        // Bottom
			widthLabel, // Left
			resetBtn,   // Right
			slider,     // Center
		)

		content.Add(colContainer)
		content.Add(widget.NewSeparator())
	}

	// Preset buttons
	presetContainer := container.NewHBox(
		widget.NewLabel("Presets:"),
		widget.NewButtonWithIcon("Default", theme.HomeIcon(), func() {
			aw.applyPreset("default", sliders, labels)
		}),
		widget.NewButtonWithIcon("Compact", theme.ZoomFitIcon(), func() {
			aw.applyPreset("compact", sliders, labels)
		}),
		widget.NewButtonWithIcon("Wide", theme.ZoomInIcon(), func() {
			aw.applyPreset("wide", sliders, labels)
		}),
		widget.NewButtonWithIcon("Summary Focus", theme.DocumentIcon(), func() {
			aw.applyPreset("summary", sliders, labels)
		}),
	)
	content.Add(presetContainer)

	// Action buttons
	actionContainer := container.NewHBox(
		widget.NewButtonWithIcon("Reset All", theme.ViewRefreshIcon(), func() {
			aw.applyPreset("default", sliders, labels)
		}),
		widget.NewButton("Close", func() {
			if aw.columnDialog != nil {
				aw.columnDialog.Hide()
			}
		}),
	)
	content.Add(actionContainer)

	// Create scrollable container for the content
	scroll := container.NewScroll(content)
	scroll.SetMinSize(fyne.NewSize(500, 600))

	// Create and show dialog
	aw.columnDialog = dialog.NewCustom("Column Settings", "Close", scroll, aw.window)
	aw.columnDialog.Resize(fyne.NewSize(550, 650))
	aw.columnDialog.Show()
}

// applyPreset applies a predefined column width preset
func (aw *AlertsWindow) applyPreset(preset string, sliders []*widget.Slider, labels []*widget.Label) {
	var presetColumns []ColumnConfig

	switch preset {
	case "compact":
		presetColumns = []ColumnConfig{
			{Name: "Alert", Width: 150, MinWidth: 100, MaxWidth: 400},
			{Name: "Severity", Width: 100, MinWidth: 100, MaxWidth: 150},
			{Name: "Status", Width: 100, MinWidth: 100, MaxWidth: 150},
			{Name: "Team", Width: 100, MinWidth: 80, MaxWidth: 200},
			{Name: "Summary", Width: 300, MinWidth: 200, MaxWidth: 800},
			{Name: "Duration", Width: 100, MinWidth: 80, MaxWidth: 200},
			{Name: "Instance", Width: 150, MinWidth: 100, MaxWidth: 400},
		}
	case "wide":
		presetColumns = []ColumnConfig{
			{Name: "Alert", Width: 250, MinWidth: 100, MaxWidth: 400},
			{Name: "Severity", Width: 140, MinWidth: 100, MaxWidth: 150},
			{Name: "Status", Width: 140, MinWidth: 100, MaxWidth: 150},
			{Name: "Team", Width: 150, MinWidth: 80, MaxWidth: 200},
			{Name: "Summary", Width: 500, MinWidth: 200, MaxWidth: 800},
			{Name: "Duration", Width: 140, MinWidth: 80, MaxWidth: 200},
			{Name: "Instance", Width: 250, MinWidth: 100, MaxWidth: 400},
		}
	case "summary":
		presetColumns = []ColumnConfig{
			{Name: "Alert", Width: 150, MinWidth: 100, MaxWidth: 400},
			{Name: "Severity", Width: 110, MinWidth: 100, MaxWidth: 150},
			{Name: "Status", Width: 110, MinWidth: 100, MaxWidth: 150},
			{Name: "Team", Width: 110, MinWidth: 80, MaxWidth: 200},
			{Name: "Summary", Width: 600, MinWidth: 200, MaxWidth: 800},
			{Name: "Duration", Width: 100, MinWidth: 80, MaxWidth: 200},
			{Name: "Instance", Width: 140, MinWidth: 100, MaxWidth: 400},
		}
	default: // "default"
		presetColumns = getDefaultColumns()
	}

	// Apply the preset
	for i, col := range presetColumns {
		aw.columns[i] = col
		if i < len(sliders) {
			sliders[i].SetValue(float64(col.Width))
			labels[i].SetText(fmt.Sprintf("%.0f px", col.Width))
		}
	}

	aw.applyColumnWidths()
	if aw.table != nil {
		aw.table.Refresh()
	}
	// Auto-save when applying presets
	aw.saveColumnConfig()
}
