package models

import "time"

// ActivityEvent is one row of the activity feed as delivered to the browser.
type ActivityEvent struct {
	ID        string    `json:"id"`
	AlertKey  string    `json:"alertKey"`
	AlertName string    `json:"alertName"` // resolved from cache; falls back to AlertKey
	Source    string    `json:"source"`    // alertmanager, "" when uncached
	Kind      string    `json:"kind"`      // comment|ack|unack|silence|resolve
	Username  string    `json:"username"`
	Content   string    `json:"content"`
	Uncached  bool      `json:"uncached"` // true when the alert is no longer in the cache
	CreatedAt time.Time `json:"createdAt"`
}
