package handlers

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"notificator/internal/backend/proto/alert"
	"notificator/internal/webui/middleware"
	webuimodels "notificator/internal/webui/models"
	"notificator/internal/webui/templates/pages"

	"github.com/a-h/templ"
)

// MentionWindowMinutes is how far back a mentions feed reaches. It is the default
// for any mentionsOnly request, so the unseen-mentions badge and the mentions view
// cover exactly the same set of comments — a mention the badge counts is always a
// mention the view can show.
const MentionWindowMinutes = 7 * 24 * 60

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

// mentionHandleRE matches a whole "@handle" run. The handle character class is
// Unicode-aware - letters and digits from any script, plus "_" and "-" - so an
// accented handle like "@bobé" is one token rather than "@bob" followed by
// something that a byte-wise scan mistakes for a word boundary.
var mentionHandleRE = regexp.MustCompile(`@[\p{L}\p{N}_-]+`)

// isMentionHandleRune reports whether r can be part of an "@handle".
func isMentionHandleRune(r rune) bool {
	return r == '_' || r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// mentionsUsername reports whether content @-mentions username, case-insensitively.
//
// Every "@handle" run in the text is extracted whole and compared to username as a
// unit, so a longer handle can never be read as a mention of a shorter one: not for
// ASCII ("@bobby" is not "bob") and not across a multi-byte rune ("@bobé" is not
// "bob"). A run preceded by another handle rune is an address rather than a mention,
// which keeps "ops-team@bob" and "db01@bob.internal" out.
func mentionsUsername(content, username string) bool {
	if username == "" {
		return false
	}
	for _, m := range mentionHandleRE.FindAllStringIndex(content, -1) {
		if prev, _ := utf8.DecodeLastRuneInString(content[:m[0]]); isMentionHandleRune(prev) {
			continue
		}
		if strings.EqualFold(content[m[0]+len("@"):m[1]], username) {
			return true
		}
	}
	return false
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

	// mentionsOnly narrows the feed to comments that @-mention the current user. Resolve
	// the requesting user up front so the mention can be pushed into the backend query
	// itself (below) instead of being applied only after the 200-row cap has already
	// truncated the feed.
	mentionsOnly := c.Query("mentionsOnly") == "true"
	mentionUsername := ""
	if mentionsOnly {
		u := middleware.GetEffectiveUser(c)
		if u == nil {
			c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("Not authenticated"))
			return
		}
		mentionUsername = u.Username
	}

	// window: minutes back from now. The general feed is a live tail, so it defaults to
	// the last hour; a mentions feed defaults to MentionWindowMinutes because a mention
	// is addressed to one person and has to keep until they read it. Both the badge and
	// the mentions view derive their window from this single default, so they cannot
	// disagree about which mentions exist. Only guarded for n > 0; there is no upper
	// bound here — result size is instead bounded by the backend query's limit=200.
	minutes := 60
	if mentionsOnly {
		minutes = MentionWindowMinutes
	}
	if v := c.Query("windowMinutes"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			minutes = n
		}
	}
	since := time.Now().Add(-time.Duration(minutes) * time.Minute)

	events, err := backendClient.GetRecentActivity(sessionID, since, 200, mentionUsername)
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
	// The backend query above already narrowed to comments containing "@mentionUsername"
	// as a coarse substring prefilter; apply the exact word-boundary check here to rule
	// out e.g. "@bob" matching inside "@bobby" or "ops-team@bob", plus the kind and
	// self-mention checks. The author's own comment never counts, even if it mentions
	// themself.
	if mentionsOnly {
		kept := feed[:0]
		for _, ev := range feed {
			if ev.Kind == "comment" && ev.Username != mentionUsername && mentionsUsername(ev.Content, mentionUsername) {
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
		User:                 pages.ProfileUser{ID: user.ID, Username: user.Username, Email: user.Email},
		MentionWindowMinutes: MentionWindowMinutes,
	})).ServeHTTP(c.Writer, c.Request)
}
