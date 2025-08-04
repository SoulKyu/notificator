package gui

import (
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	authpb "notificator/internal/backend/proto/auth"
)

type MentionEntry struct {
	widget.Entry
	backendClient     *BackendClient
	mentionPopup      *widget.PopUp
	userList          *widget.List
	users             []*authpb.User
	selectedIndex     int
	currentQuery      string
	mentionStartPos   int
	debounceTimer     *time.Timer
	parentWindow      fyne.Window
	onMentionSelected func(user *authpb.User)
}

func NewMentionEntry(backendClient *BackendClient, parentWindow fyne.Window) *MentionEntry {
	entry := &MentionEntry{
		backendClient:   backendClient,
		parentWindow:    parentWindow,
		selectedIndex:   -1,
		mentionStartPos: -1,
	}

	entry.ExtendBaseWidget(entry)
	entry.MultiLine = true
	entry.Wrapping = fyne.TextWrapWord

	// Set up the user list
	entry.setupUserList()

	// Override the OnChanged callback
	entry.Entry.OnChanged = entry.handleTextChange

	// Handle key events
	entry.Entry.OnSubmitted = func(text string) {
		if entry.mentionPopup != nil && entry.mentionPopup.Visible() {
			entry.selectCurrentUser()
		}
	}

	return entry
}

func (m *MentionEntry) setupUserList() {
	m.userList = widget.NewList(
		func() int {
			return len(m.users)
		},
		func() fyne.CanvasObject {
			return container.NewHBox(
				widget.NewIcon(theme.AccountIcon()),
				widget.NewLabel("Username"),
			)
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			if id < len(m.users) {
				user := m.users[id]
				hbox := item.(*fyne.Container)
				label := hbox.Objects[1].(*widget.Label)
				label.SetText(user.Username)

				// Highlight selected item
				if id == m.selectedIndex {
					label.Importance = widget.HighImportance
				} else {
					label.Importance = widget.LowImportance
				}
			}
		},
	)

	// Handle list selection
	m.userList.OnSelected = func(id widget.ListItemID) {
		m.selectedIndex = id
		m.selectCurrentUser()
	}
}

func (m *MentionEntry) handleTextChange(text string) {
	// Find the cursor position (simplified - assumes cursor is at end)
	cursorPos := len(text)

	// Look for @ symbol before cursor
	mentionPos := -1
	for i := cursorPos - 1; i >= 0; i-- {
		if text[i] == '@' {
			// Check if this @ is at start of word
			if i == 0 || text[i-1] == ' ' || text[i-1] == '\n' {
				mentionPos = i
				break
			}
		} else if text[i] == ' ' || text[i] == '\n' {
			// Stop at whitespace
			break
		}
	}

	if mentionPos != -1 {
		// Extract the query after @
		query := text[mentionPos+1 : cursorPos]

		// Check if query contains spaces (end of mention)
		if strings.Contains(query, " ") || strings.Contains(query, "\n") {
			m.hidePopup()
			return
		}

		m.currentQuery = query
		m.mentionStartPos = mentionPos

		// Debounce search requests
		if m.debounceTimer != nil {
			m.debounceTimer.Stop()
		}

		m.debounceTimer = time.AfterFunc(300*time.Millisecond, func() {
			m.searchUsers(query)
		})
	} else {
		m.hidePopup()
	}
}

func (m *MentionEntry) searchUsers(query string) {
	if m.backendClient == nil || !m.backendClient.IsLoggedIn() {
		return
	}

	// Don't search for empty queries
	if query == "" {
		return
	}

	go func() {
		resp, err := m.backendClient.SearchUsers(query, 10)
		if err != nil {
			return
		}

		// Update UI on main thread
		fyne.Do(func() {
			m.users = resp.Users
			m.selectedIndex = 0

			if len(m.users) > 0 {
				m.showPopup()
			} else {
				m.hidePopup()
			}
		})
	}()
}

func (m *MentionEntry) showPopup() {
	if m.mentionPopup == nil {
		m.mentionPopup = widget.NewPopUp(m.userList, m.parentWindow.Canvas())
		m.mentionPopup.Resize(fyne.NewSize(200, 150))
	}

	m.userList.Refresh()

	// Position popup near the entry
	entryPos := fyne.CurrentApp().Driver().AbsolutePositionForObject(m)
	entrySize := m.Size()

	popupPos := fyne.NewPos(
		entryPos.X,
		entryPos.Y+entrySize.Height,
	)

	m.mentionPopup.Move(popupPos)
	m.mentionPopup.Show()
}

func (m *MentionEntry) hidePopup() {
	if m.mentionPopup != nil {
		m.mentionPopup.Hide()
	}
	m.selectedIndex = -1
	m.mentionStartPos = -1
	m.currentQuery = ""
}

func (m *MentionEntry) selectCurrentUser() {
	if m.selectedIndex >= 0 && m.selectedIndex < len(m.users) {
		user := m.users[m.selectedIndex]
		m.insertMention(user)
		m.hidePopup()
	}
}

func (m *MentionEntry) insertMention(user *authpb.User) {
	if m.mentionStartPos == -1 {
		return
	}

	currentText := m.Text

	// Replace @query with @username
	beforeMention := currentText[:m.mentionStartPos]
	afterMention := currentText[m.mentionStartPos+len(m.currentQuery)+1:] // +1 for @

	newText := beforeMention + "@" + user.Username + " " + afterMention
	m.SetText(newText)

	// Call callback if set
	if m.onMentionSelected != nil {
		m.onMentionSelected(user)
	}
}

func (m *MentionEntry) SetOnMentionSelected(callback func(user *authpb.User)) {
	m.onMentionSelected = callback
}

func (m *MentionEntry) TypedKey(key *fyne.KeyEvent) {
	if m.mentionPopup != nil && m.mentionPopup.Visible() {
		switch key.Name {
		case fyne.KeyDown:
			if m.selectedIndex < len(m.users)-1 {
				m.selectedIndex++
				m.userList.Refresh()
			}
			return
		case fyne.KeyUp:
			if m.selectedIndex > 0 {
				m.selectedIndex--
				m.userList.Refresh()
			}
			return
		case fyne.KeyReturn, fyne.KeyEnter:
			m.selectCurrentUser()
			return
		case fyne.KeyEscape:
			m.hidePopup()
			return
		}
	}

	// Pass through to base widget
	m.Entry.TypedKey(key)
}
