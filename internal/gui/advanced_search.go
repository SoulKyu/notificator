package gui

import (
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// AutocompleteEntry is an enhanced entry widget with autocomplete functionality
type AutocompleteEntry struct {
	widget.Entry
	suggestions []string
	popup       *widget.PopUp
	list        *widget.List
	window      fyne.Window
}

// NewAutocompleteEntry creates a new autocomplete entry widget
func NewAutocompleteEntry(window fyne.Window) *AutocompleteEntry {
	entry := &AutocompleteEntry{
		window: window,
	}
	entry.ExtendBaseWidget(entry)
	return entry
}

// Tapped handles mouse clicks to ensure focus
func (a *AutocompleteEntry) Tapped(pe *fyne.PointEvent) {
	a.Entry.Tapped(pe)
	// Ensure the entry gets focus when clicked
	a.window.Canvas().Focus(a)
}

// FocusGained handles when the entry gains focus
func (a *AutocompleteEntry) FocusGained() {
	a.Entry.FocusGained()
}

// SetSuggestions updates the list of available suggestions
func (a *AutocompleteEntry) SetSuggestions(suggestions []string) {
	a.suggestions = suggestions
}

// TypedRune handles character input and shows suggestions
func (a *AutocompleteEntry) TypedRune(r rune) {
	a.Entry.TypedRune(r)
	a.showSuggestions()
}

// TypedKey handles key input
func (a *AutocompleteEntry) TypedKey(key *fyne.KeyEvent) {
	switch key.Name {
	case fyne.KeyEscape:
		a.hideSuggestions()
		return
	case fyne.KeyDown:
		if a.popup != nil && a.popup.Visible() {
			// Move selection down in list
			return
		}
	case fyne.KeyUp:
		if a.popup != nil && a.popup.Visible() {
			// Move selection up in list
			return
		}
	case fyne.KeyReturn, fyne.KeyEnter:
		if a.popup != nil && a.popup.Visible() {
			a.hideSuggestions()
			return
		}
	}
	a.Entry.TypedKey(key)
	a.showSuggestions()
}

// showSuggestions displays the autocomplete popup with enhanced filtering
func (a *AutocompleteEntry) showSuggestions() {
	text := strings.ToLower(a.Text)
	if len(text) < 1 { // Show suggestions even with 1 character
		a.hideSuggestions()
		return
	}

	// More lenient filtering with fuzzy matching
	var filtered []string
	var exactMatches []string
	var startsWithMatches []string
	var containsMatches []string
	var fuzzyMatches []string

	for _, suggestion := range a.suggestions {
		suggestionLower := strings.ToLower(suggestion)

		if suggestionLower == text {
			exactMatches = append(exactMatches, suggestion)
		} else if strings.HasPrefix(suggestionLower, text) {
			startsWithMatches = append(startsWithMatches, suggestion)
		} else if strings.Contains(suggestionLower, text) {
			containsMatches = append(containsMatches, suggestion)
		} else if a.fuzzyMatch(text, suggestionLower) {
			// Add fuzzy matching for more lenient suggestions
			fuzzyMatches = append(fuzzyMatches, suggestion)
		}
	}

	// Combine results in order of relevance
	filtered = append(filtered, exactMatches...)
	filtered = append(filtered, startsWithMatches...)
	filtered = append(filtered, containsMatches...)
	filtered = append(filtered, fuzzyMatches...)

	if len(filtered) == 0 {
		a.hideSuggestions()
		return
	}

	// Sort each category by length (shorter first, more specific)
	sort.Slice(startsWithMatches, func(i, j int) bool {
		return len(startsWithMatches[i]) < len(startsWithMatches[j])
	})
	sort.Slice(containsMatches, func(i, j int) bool {
		return len(containsMatches[i]) < len(containsMatches[j])
	})
	sort.Slice(fuzzyMatches, func(i, j int) bool {
		return len(fuzzyMatches[i]) < len(fuzzyMatches[j])
	})

	// Rebuild filtered list with sorted categories
	filtered = filtered[:0]
	filtered = append(filtered, exactMatches...)
	filtered = append(filtered, startsWithMatches...)
	filtered = append(filtered, containsMatches...)
	filtered = append(filtered, fuzzyMatches...)

	// Limit to top 20 suggestions (increased from 15)
	if len(filtered) > 20 {
		filtered = filtered[:20]
	}

	a.createPopup(filtered)
}

// fuzzyMatch performs simple fuzzy matching for more lenient autocomplete
func (a *AutocompleteEntry) fuzzyMatch(input, target string) bool {
	if len(input) == 0 {
		return false
	}

	// Simple fuzzy matching: check if all characters of input appear in order in target
	inputIndex := 0
	for i := 0; i < len(target) && inputIndex < len(input); i++ {
		if target[i] == input[inputIndex] {
			inputIndex++
		}
	}

	// Match if we found all input characters in order
	return inputIndex == len(input)
}

// createPopup creates and shows the suggestion popup
func (a *AutocompleteEntry) createPopup(suggestions []string) {
	if a.popup != nil {
		a.popup.Hide()
	}

	a.list = widget.NewList(
		func() int { return len(suggestions) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			obj.(*widget.Label).SetText(suggestions[id])
		},
	)

	a.list.OnSelected = func(id widget.ListItemID) {
		if id < len(suggestions) {
			a.SetText(suggestions[id])
			a.hideSuggestions()
			if a.OnChanged != nil {
				a.OnChanged(suggestions[id])
			}
		}
	}

	// Calculate popup size and position
	entrySize := a.Size()
	entryPos := fyne.CurrentApp().Driver().AbsolutePositionForObject(a)

	// Make popup wider and taller for better visibility
	popupWidth := entrySize.Width
	if popupWidth < 700 {
		popupWidth = 700 // Ensure minimum width
	}

	popupSize := fyne.NewSize(popupWidth, float32(len(suggestions)*35))
	if popupSize.Height > 250 {
		popupSize.Height = 250 // Increased max height
	}

	content := container.NewBorder(nil, nil, nil, nil, a.list)
	content.Resize(popupSize)

	a.popup = widget.NewPopUp(content, a.window.Canvas())
	a.popup.ShowAtPosition(fyne.NewPos(entryPos.X, entryPos.Y+entrySize.Height))
}

// hideSuggestions hides the suggestion popup
func (a *AutocompleteEntry) hideSuggestions() {
	if a.popup != nil {
		a.popup.Hide()
		a.popup = nil
	}
}

// FocusLost handles when the entry loses focus
func (a *AutocompleteEntry) FocusLost() {
	a.Entry.FocusLost()
	// Delay hiding to allow for selection
	go func() {
		// Small delay to allow popup selection
		// time.Sleep(100 * time.Millisecond)
		a.hideSuggestions()
	}()
}
