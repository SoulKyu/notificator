package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// SSEStream handles Server-Sent Events connections for real-time alert updates.
// Clients connect to this endpoint and receive push notifications when alerts change,
// eliminating the need for polling.
//
// SSE Protocol:
// - Content-Type: text/event-stream
// - Cache-Control: no-cache
// - Connection: keep-alive
//
// Events sent:
// - "update": Contains DashboardIncrementalUpdate JSON with new/updated/removed alerts
// - "ping": Heartbeat to keep connection alive (every 30 seconds)
func SSEStream(c *gin.Context) {
	// Verify alert cache is available
	if alertCache == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Alert cache not initialized"})
		return
	}

	// Set SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no") // Disable nginx buffering

	// Subscribe to alert updates
	updates := alertCache.Subscribe()
	defer alertCache.Unsubscribe(updates)

	// Create a heartbeat ticker to keep the connection alive
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	log.Printf("SSE client connected, total subscribers: %d", alertCache.GetSubscriberCount())

	// Use gin's streaming functionality
	c.Stream(func(w io.Writer) bool {
		select {
		case update, ok := <-updates:
			if !ok {
				// Channel was closed, end the stream
				log.Printf("SSE update channel closed")
				return false
			}

			// Marshal the update to JSON
			data, err := json.Marshal(update)
			if err != nil {
				log.Printf("SSE: Failed to marshal update: %v", err)
				return true // Continue streaming
			}

			// Send the SSE event
			c.SSEvent("update", string(data))
			return true

		case <-heartbeat.C:
			// Send heartbeat to keep connection alive
			c.SSEvent("ping", `{"type":"heartbeat"}`)
			return true

		case <-c.Request.Context().Done():
			// Client disconnected
			log.Printf("SSE client disconnected")
			return false
		}
	})
}

// SSEStatus returns the current status of the SSE system including subscriber count.
// This is useful for monitoring and debugging.
func SSEStatus(c *gin.Context) {
	if alertCache == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Alert cache not initialized"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"subscribers": alertCache.GetSubscriberCount(),
		"status":      "active",
	})
}
