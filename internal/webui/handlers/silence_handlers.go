package handlers

import (
	"errors"
	"log"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"time"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"notificator/internal/alertmanager"
	"notificator/internal/models"
	"notificator/internal/webui/middleware"
	webuimodels "notificator/internal/webui/models"
	"notificator/internal/webui/templates/pages"
)

// maxSilenceExtension caps how far a single extend request can push the end time.
const maxSilenceExtension = 30 * 24 * time.Hour

// silenceStateExpired is the Alertmanager state of a silence whose end time has passed.
const silenceStateExpired = "expired"

// compiledMatcher is a silence matcher whose regex has been compiled once, so matching a
// silence against a whole alert cache does not recompile the pattern for every alert.
type compiledMatcher struct {
	name    string
	value   string
	isEqual bool
	re      *regexp.Regexp // nil for plain equality matchers
}

// matches reports whether the matcher holds for a label value. An absent label is matched
// as the empty string, which is how Alertmanager evaluates it.
func (cm compiledMatcher) matches(value string) bool {
	matched := false
	if cm.re != nil {
		matched = cm.re.MatchString(value)
	} else {
		matched = cm.value == value
	}

	if cm.isEqual {
		return matched
	}
	return !matched
}

// compileMatchers compiles every matcher of a silence up front. It reports false when a
// matcher carries an invalid regex, in which case the silence matches nothing. A silence
// with no matchers also matches nothing (Alertmanager rejects those anyway).
func compileMatchers(silence models.Silence) ([]compiledMatcher, bool) {
	if len(silence.Matchers) == 0 {
		return nil, false
	}

	compiled := make([]compiledMatcher, 0, len(silence.Matchers))
	for _, matcher := range silence.Matchers {
		cm := compiledMatcher{name: matcher.Name, value: matcher.Value, isEqual: matcher.IsEqual}
		if matcher.IsRegex {
			// Alertmanager anchors silence regexes on both ends.
			re, err := regexp.Compile("^(?:" + matcher.Value + ")$")
			if err != nil {
				return nil, false
			}
			cm.re = re
		}
		compiled = append(compiled, cm)
	}
	return compiled, true
}

// matchLabels reports whether every compiled matcher holds for the given labels.
func matchLabels(matchers []compiledMatcher, labels map[string]string) bool {
	for _, matcher := range matchers {
		if !matcher.matches(labels[matcher.name]) {
			return false
		}
	}
	return true
}

// silenceMatchesAlert reports whether every matcher of the silence holds for the alert labels.
func silenceMatchesAlert(silence models.Silence, labels map[string]string) bool {
	matchers, ok := compileMatchers(silence)
	if !ok {
		return false
	}
	return matchLabels(matchers, labels)
}

// countMatchedAlerts counts cached alerts from the same Alertmanager that the silence
// matches. Matchers are compiled once for the whole scan rather than once per alert.
func countMatchedAlerts(silence models.Silence, source string, alerts []*webuimodels.DashboardAlert) int {
	matchers, ok := compileMatchers(silence)
	if !ok {
		return 0
	}

	count := 0
	for _, alert := range alerts {
		if alert.Source != source {
			continue
		}
		if matchLabels(matchers, alert.Labels) {
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

// sortSilences puts live silences first, soonest expiry first, and pushes expired ones to
// the back most-recent-first. Ordering by EndsAt alone would bury every actionable silence
// under the 120h of expired ones Alertmanager retains by default.
func sortSilences(silences []webuimodels.Silence) {
	sort.SliceStable(silences, func(i, j int) bool {
		iExpired := silences[i].Status.State == silenceStateExpired
		jExpired := silences[j].Status.State == silenceStateExpired
		if iExpired != jExpired {
			return !iExpired
		}
		if iExpired {
			return silences[i].EndsAt.After(silences[j].EndsAt)
		}
		return silences[i].EndsAt.Before(silences[j].EndsAt)
	})
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
		// Expired silences never show a matched count, so don't pay for the scan.
		// Alertmanager retains them for --data.retention (120h by default).
		matched := 0
		if item.Silence.Status.State != silenceStateExpired {
			matched = countMatchedAlerts(item.Silence, item.Source, alerts)
		}
		silences = append(silences, toWebuiSilence(item.Silence, item.Source, matched))
	}

	sortSilences(silences)

	failed := make(map[string]string, len(failedSources))
	for name, err := range failedSources {
		failed[name] = err.Error()
	}

	// GetClientNames ranges over a map, so sort to keep the source dropdown stable.
	sources := alertmanagerClient.GetClientNames()
	sort.Strings(sources)

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"silences":      silences,
		"sources":       sources,
		"failedSources": failed,
	}))
}

// silenceErrorStatus maps a silence lookup failure to an HTTP status. A stale silence ID is
// the caller's problem (404); 502 stays reserved for a genuine upstream fault, so watching
// webui 5xx rates does not alarm on user input.
func silenceErrorStatus(err error) int {
	if errors.Is(err, alertmanager.ErrSilenceNotFound) {
		return http.StatusNotFound
	}
	return http.StatusBadGateway
}

// ExtendSilence pushes the end time of an existing silence further out. Alertmanager
// upserts on the silence ID for active and pending silences, so re-posting one keeps its
// identity. An expired silence cannot be revived that way — Alertmanager's canUpdate
// refuses it and mints a brand-new silence instead — so extending one is rejected.
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

	if !slices.Contains(alertmanagerClient.GetClientNames(), request.Source) {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Unknown alertmanager source: "+request.Source))
		return
	}

	silence, err := alertmanagerClient.FetchSilenceFromAlertmanager(request.Source, silenceID)
	if err != nil {
		c.JSON(silenceErrorStatus(err), webuimodels.ErrorResponse("Failed to fetch silence: "+err.Error()))
		return
	}

	// A lapsed silence cannot be moved: Alertmanager would create a different silence
	// under a new ID and leave this one expired. Say so instead of silently duplicating.
	if silence.Status.State == silenceStateExpired {
		c.JSON(http.StatusConflict, webuimodels.ErrorResponse("Silence has already expired and cannot be extended; recreate it instead"))
		return
	}

	silence.EndsAt = silence.EndsAt.Add(duration)

	updated, err := alertmanagerClient.CreateSilenceOnAlertmanager(request.Source, *silence)
	if err != nil {
		c.JSON(http.StatusBadGateway, webuimodels.ErrorResponse("Failed to extend silence: "+err.Error()))
		return
	}

	// Alertmanager only reuses the ID when it accepted the update in place. A different ID
	// means the silence lapsed between our read and its write, so this is a new silence
	// rather than an extension — the state check above cannot see that window.
	if updated.ID != silenceID {
		c.JSON(http.StatusConflict, webuimodels.ErrorResponse(
			"Silence lapsed while being extended; Alertmanager created a new silence "+updated.ID+" instead and the original stayed expired"))
		return
	}

	// CreateSilence echoes back the request struct, so re-read to answer with Alertmanager's
	// own status and updatedAt instead of the pre-POST snapshot. Fall back on a read failure.
	if fresh, err := alertmanagerClient.FetchSilenceFromAlertmanager(request.Source, silenceID); err == nil {
		updated = fresh
	} else {
		log.Printf("⚠️  Extended silence %s on %s but could not re-read it: %v", silenceID, request.Source, err)
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

	if !slices.Contains(alertmanagerClient.GetClientNames(), source) {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Unknown alertmanager source: "+source))
		return
	}

	if err := alertmanagerClient.DeleteSilenceFromAlertmanager(source, silenceID); err != nil {
		c.JSON(silenceErrorStatus(err), webuimodels.ErrorResponse("Failed to expire silence: "+err.Error()))
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
