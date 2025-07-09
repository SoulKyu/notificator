// internal/gui/background_mode.go
package gui

import (
	"log"
	"notificator/internal/models"
	"time"
)

// initializeBackgroundMode sets up background mode functionality
func (aw *AlertsWindow) initializeBackgroundMode() {
	// Initialize background mode state
	aw.isBackgroundMode = false
	if aw.originalConfig != nil {
		aw.isBackgroundMode = aw.originalConfig.GUI.BackgroundMode
	}

	// Create tray manager
	aw.trayManager = NewTrayManager(aw.app, aw.window, aw)

	// Create background notifier with callback to show window
	aw.backgroundNotifier = NewBackgroundNotifier(aw.notificationConfig, aw.app, func() {
		if aw.trayManager != nil {
			aw.trayManager.ShowWindow()
		}
	})

	// Handle window close event
	aw.window.SetCloseIntercept(func() {
		if aw.originalConfig != nil && aw.originalConfig.GUI.MinimizeToTray {
			aw.trayManager.HideToBackground()
		} else {
			aw.app.Quit()
		}
	})

	log.Println("Background mode initialized")
}

// startBackgroundModeIfConfigured starts in background mode if configured
func (aw *AlertsWindow) startBackgroundModeIfConfigured() {
	if aw.originalConfig != nil && aw.originalConfig.GUI.StartMinimized {
		go func() {
			time.Sleep(2 * time.Second) // Give UI time to initialize
			aw.scheduleUpdate(func() {
				if aw.trayManager != nil {
					aw.trayManager.HideToBackground()
				}
			})
		}()
	}
}

// processNotificationsBasedOnMode processes notifications based on current mode
func (aw *AlertsWindow) processNotificationsBasedOnMode(newAlerts, previousAlerts []models.Alert) {
	if aw.IsBackgroundMode() && aw.backgroundNotifier != nil {
		aw.backgroundNotifier.ProcessAlerts(newAlerts, previousAlerts)
	} else if aw.notifier != nil {
		aw.notifier.ProcessAlerts(newAlerts, previousAlerts)
	}
}
