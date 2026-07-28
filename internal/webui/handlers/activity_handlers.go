package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"notificator/internal/backend/proto/alert"
	"notificator/internal/webui/middleware"
	webuimodels "notificator/internal/webui/models"
	"notificator/internal/webui/templates/pages"

	"github.com/a-h/templ"
)

// alertResolver returns an alert's display fields from the cache.
type alertResolver func(alertKey string) (name, source, severity, team string, ok bool)

// hasAlertLevelFilter reports whether any label-level filter is active. When none is,
// events whose alert is no longer cached pass through (full history); when one is,
// they are hidden because they cannot be evaluated.
func hasAlertLevelFilter(f webuimodels.DashboardFilters) bool {
	return f.Search != "" || len(f.Alertmanagers) > 0 || len(f.Severities) > 0 ||
		len(f.Teams) > 0 || len(f.AlertNames) > 0
}

// buildActivityFeed resolves each backend event's alert name from the cache and applies
// the shared alert-level filter predicate, honouring the uncached pass-through/hide rule.
func buildActivityFeed(events []*alert.ActivityEvent, filters webuimodels.DashboardFilters, resolve alertResolver) []webuimodels.ActivityEvent {
	filterActive := hasAlertLevelFilter(filters)
	out := make([]webuimodels.ActivityEvent, 0, len(events))
	for _, e := range events {
		name, source, severity, team, ok := resolve(e.AlertKey)
		if !ok {
			if filterActive {
				continue // cannot evaluate an uncached alert against an active filter
			}
			out = append(out, webuimodels.ActivityEvent{
				ID: e.Id, AlertKey: e.AlertKey, AlertName: e.AlertKey, Kind: e.Kind,
				Username: e.Username, Content: e.Content, Uncached: true,
				CreatedAt: e.CreatedAt.AsTime(),
			})
			continue
		}
		probe := &webuimodels.DashboardAlert{AlertName: name, Source: source, Severity: severity, Team: team}
		if filterActive && !alertPassesAlertLevelFilters(probe, filters) {
			continue
		}
		out = append(out, webuimodels.ActivityEvent{
			ID: e.Id, AlertKey: e.AlertKey, AlertName: name, Source: source, Kind: e.Kind,
			Username: e.Username, Content: e.Content, CreatedAt: e.CreatedAt.AsTime(),
		})
	}
	return out
}

// matchesActivitySearch reports whether an activity event's content, username, or
// alert name contains term (case-insensitive). An empty term always matches, so
// callers can apply this unconditionally.
func matchesActivitySearch(ev webuimodels.ActivityEvent, term string) bool {
	if term == "" {
		return true
	}
	term = strings.ToLower(term)
	return strings.Contains(strings.ToLower(ev.Content), term) ||
		strings.Contains(strings.ToLower(ev.Username), term) ||
		strings.Contains(strings.ToLower(ev.AlertName), term)
}

// GetActivity serves the activity feed JSON.
func GetActivity(c *gin.Context) {
	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Backend not available"))
		return
	}
	sessionID := middleware.GetSessionIDFromContext(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("Not authenticated"))
		return
	}

	// window: minutes back from now, default 60. Only guarded for n > 0; there is no
	// upper bound here — result size is instead bounded by the backend query's limit=200.
	minutes := 60
	if v := c.Query("windowMinutes"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			minutes = n
		}
	}
	since := time.Now().Add(-time.Duration(minutes) * time.Minute)

	events, err := backendClient.GetRecentActivity(sessionID, since, 200)
	if err != nil {
		c.JSON(http.StatusBadGateway, webuimodels.ErrorResponse("Failed to load activity: "+err.Error()))
		return
	}

	filters := parseDashboardFilters(c)
	// Search targets comment content/username/alertName (applied below, post-resolution),
	// not the alert-level fields alertPassesAlertLevelFilters checks. Clear it here so it
	// neither gets applied at the alert level nor counts toward hasAlertLevelFilter -
	// otherwise a search term would incorrectly hide every uncached event.
	searchTerm := filters.Search
	filters.Search = ""
	resolve := func(key string) (string, string, string, string, bool) {
		if alertCache == nil {
			return "", "", "", "", false
		}
		a, ok := alertCache.GetAlert(key)
		if !ok {
			return "", "", "", "", false
		}
		return a.AlertName, a.Source, a.Severity, a.Team, true
	}

	feed := buildActivityFeed(events, filters, resolve)

	// Scope (Mine/Everyone), search, and action-type (kinds) are applied here, post-resolution.
	if searchTerm != "" {
		kept := feed[:0]
		for _, ev := range feed {
			if matchesActivitySearch(ev, searchTerm) {
				kept = append(kept, ev)
			}
		}
		feed = kept
	}
	if c.Query("scope") == "mine" {
		if u := middleware.GetEffectiveUser(c); u != nil {
			mineName := u.Username
			kept := feed[:0]
			for _, ev := range feed {
				if ev.Username == mineName {
					kept = append(kept, ev)
				}
			}
			feed = kept
		}
	}
	if kinds := parseStringArray(c.Query("kinds")); len(kinds) > 0 {
		kept := feed[:0]
		for _, ev := range feed {
			if contains(kinds, ev.Kind) {
				kept = append(kept, ev)
			}
		}
		feed = kept
	}

	// Offer the same filter option lists the dashboard does, computed from the same
	// alert cache, so the two filter bars stay in lockstep.
	available := webuimodels.DashboardAvailableFilters{}
	if alertCache != nil {
		available = computeAvailableFilters(alertCache.GetAllAlerts())
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"events":           feed,
		"availableFilters": available,
	}))
}

// ActivityPage serves the activity page shell.
func ActivityPage(c *gin.Context) {
	user := middleware.GetCurrentUserFromContext(c)
	if user == nil {
		c.Redirect(http.StatusFound, "/login")
		return
	}
	templ.Handler(pages.Activity(pages.ActivityData{
		User: pages.ProfileUser{ID: user.ID, Username: user.Username, Email: user.Email},
	})).ServeHTTP(c.Writer, c.Request)
}
