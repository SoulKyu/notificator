package handlers

import (
	"log"
	"net/http"
	"regexp"
	"sort"
	"time"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"notificator/internal/models"
	"notificator/internal/webui/middleware"
	webuimodels "notificator/internal/webui/models"
	"notificator/internal/webui/templates/pages"
)

// maxSilenceExtension caps how far a single extend request can push the end time.
const maxSilenceExtension = 30 * 24 * time.Hour

// silenceMatcherMatches reports whether a single matcher holds for a label value.
// An absent label is matched as the empty string, which is how Alertmanager evaluates it.
func silenceMatcherMatches(matcher models.SilenceMatcher, value string) bool {
	matched := false
	if matcher.IsRegex {
		// Alertmanager anchors silence regexes on both ends.
		re, err := regexp.Compile("^(?:" + matcher.Value + ")$")
		if err != nil {
			return false
		}
		matched = re.MatchString(value)
	} else {
		matched = matcher.Value == value
	}

	if matcher.IsEqual {
		return matched
	}
	return !matched
}

// silenceMatchesAlert reports whether every matcher of the silence holds for the alert
// labels. A silence with no matchers matches nothing (Alertmanager rejects those anyway).
func silenceMatchesAlert(silence models.Silence, labels map[string]string) bool {
	if len(silence.Matchers) == 0 {
		return false
	}

	for _, matcher := range silence.Matchers {
		if !silenceMatcherMatches(matcher, labels[matcher.Name]) {
			return false
		}
	}
	return true
}

// countMatchedAlerts counts cached alerts from the same Alertmanager that the silence matches.
func countMatchedAlerts(silence models.Silence, source string, alerts []*webuimodels.DashboardAlert) int {
	count := 0
	for _, alert := range alerts {
		if alert.Source != source {
			continue
		}
		if silenceMatchesAlert(silence, alert.Labels) {
			count++
		}
	}
	return count
}

// toWebuiSilence converts an Alertmanager silence into the wire type used by the UI.
func toWebuiSilence(silence models.Silence, source string, matchedAlerts int) webuimodels.Silence {
	matchers := make([]webuimodels.SilenceMatcher, len(silence.Matchers))
	for i, matcher := range silence.Matchers {
		matchers[i] = webuimodels.SilenceMatcher{
			Name:    matcher.Name,
			Value:   matcher.Value,
			IsRegex: matcher.IsRegex,
			IsEqual: matcher.IsEqual,
		}
	}

	return webuimodels.Silence{
		ID:            silence.ID,
		Source:        source,
		CreatedBy:     silence.CreatedBy,
		Comment:       silence.Comment,
		StartsAt:      silence.StartsAt,
		EndsAt:        silence.EndsAt,
		UpdatedAt:     silence.UpdatedAt,
		Matchers:      matchers,
		Status:        webuimodels.SilenceStatus{State: silence.Status.State},
		MatchedAlerts: matchedAlerts,
	}
}

// fetchAlertSilences resolves the silence IDs of a silenced alert into full silences.
// It only queries the Alertmanager the alert came from, so an opened modal costs at most
// one request per silence ID. Unresolvable IDs are skipped rather than failing the modal.
func fetchAlertSilences(alert *webuimodels.DashboardAlert) []webuimodels.Silence {
	silences := []webuimodels.Silence{}
	if alertmanagerClient == nil || alert == nil || len(alert.Status.SilencedBy) == 0 {
		return silences
	}

	for _, silenceID := range alert.Status.SilencedBy {
		silence, err := alertmanagerClient.FetchSilenceFromAlertmanager(alert.Source, silenceID)
		if err != nil {
			log.Printf("⚠️  Failed to fetch silence %s from %s: %v", silenceID, alert.Source, err)
			continue
		}
		silences = append(silences, toWebuiSilence(*silence, alert.Source, 0))
	}

	return silences
}

// GetSilences returns every silence of every configured Alertmanager, sorted by soonest
// expiry, together with the sources that could not be reached.
func GetSilences(c *gin.Context) {
	if alertmanagerClient == nil {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Alertmanager client not available"))
		return
	}

	silencesWithSource, failedSources := alertmanagerClient.FetchAllSilencesDetailed()

	var alerts []*webuimodels.DashboardAlert
	if alertCache != nil {
		alerts = alertCache.GetAllAlerts()
	}

	silences := make([]webuimodels.Silence, 0, len(silencesWithSource))
	for _, item := range silencesWithSource {
		matched := countMatchedAlerts(item.Silence, item.Source, alerts)
		silences = append(silences, toWebuiSilence(item.Silence, item.Source, matched))
	}

	sort.SliceStable(silences, func(i, j int) bool {
		return silences[i].EndsAt.Before(silences[j].EndsAt)
	})

	failed := make(map[string]string, len(failedSources))
	for name, err := range failedSources {
		failed[name] = err.Error()
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"silences":      silences,
		"sources":       alertmanagerClient.GetClientNames(),
		"failedSources": failed,
	}))
}

// ExtendSilence pushes the end time of an existing silence further out. Alertmanager
// upserts on the silence ID, so re-posting the same silence keeps its identity.
func ExtendSilence(c *gin.Context) {
	if alertmanagerClient == nil {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Alertmanager client not available"))
		return
	}

	silenceID := c.Param("id")
	if silenceID == "" {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Silence ID is required"))
		return
	}

	var request struct {
		Source   string `json:"source" binding:"required"`
		Duration string `json:"duration" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid request format: "+err.Error()))
		return
	}

	duration, err := time.ParseDuration(request.Duration)
	if err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid duration: "+err.Error()))
		return
	}
	if duration <= 0 || duration > maxSilenceExtension {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Duration must be between 0 and 30 days"))
		return
	}

	silence, err := alertmanagerClient.FetchSilenceFromAlertmanager(request.Source, silenceID)
	if err != nil {
		c.JSON(http.StatusBadGateway, webuimodels.ErrorResponse("Failed to fetch silence: "+err.Error()))
		return
	}

	// Extend from the current end time, or from now if the silence already lapsed.
	base := silence.EndsAt
	if now := time.Now(); base.Before(now) {
		base = now
	}
	silence.EndsAt = base.Add(duration)

	updated, err := alertmanagerClient.CreateSilenceOnAlertmanager(request.Source, *silence)
	if err != nil {
		c.JSON(http.StatusBadGateway, webuimodels.ErrorResponse("Failed to extend silence: "+err.Error()))
		return
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(toWebuiSilence(*updated, request.Source, 0)))
}

// ExpireSilence deletes a silence from the Alertmanager that holds it.
func ExpireSilence(c *gin.Context) {
	if alertmanagerClient == nil {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Alertmanager client not available"))
		return
	}

	silenceID := c.Param("id")
	if silenceID == "" {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Silence ID is required"))
		return
	}

	source := c.Query("source")
	if source == "" {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Source alertmanager is required"))
		return
	}

	if err := alertmanagerClient.DeleteSilenceFromAlertmanager(source, silenceID); err != nil {
		c.JSON(http.StatusBadGateway, webuimodels.ErrorResponse("Failed to expire silence: "+err.Error()))
		return
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{"id": silenceID, "source": source}))
}

// SilencesPage serves the silence inventory page
func SilencesPage(c *gin.Context) {
	user := middleware.GetCurrentUserFromContext(c)
	if user == nil {
		c.Redirect(http.StatusFound, "/login")
		return
	}

	pageData := pages.SilencesData{
		User: pages.ProfileUser{
			ID:       user.ID,
			Username: user.Username,
			Email:    user.Email,
		},
	}

	templ.Handler(pages.Silences(pageData)).ServeHTTP(c.Writer, c.Request)
}
