package handlers

import (
	"errors"
	"log"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
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
	annotateSilenceOrigins(c, silences)

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

// annotateSilenceOrigins resolves each silence's raw createdBy against the
// notificator user base: a match becomes the username with origin
// "notificator" (old silences stored opaque user IDs — this also fixes their
// display), anything else is tagged "external" (amtool, karma, the Alertmanager
// UI…). Resolution unavailable → origins stay "" and the UI shows no badge.
func annotateSilenceOrigins(c *gin.Context, silences []webuimodels.Silence) {
	if backendClient == nil || !backendClient.IsConnected() {
		return
	}
	sessionID := middleware.GetSessionIDFromContext(c)
	if sessionID == "" {
		return
	}

	seen := make(map[string]bool)
	creators := make([]string, 0, 8)
	for i := range silences {
		if cb := silences[i].CreatedBy; cb != "" && !seen[cb] {
			seen[cb] = true
			creators = append(creators, cb)
		}
	}
	if len(creators) == 0 {
		return
	}

	resolved, err := backendClient.ResolveSilenceCreators(sessionID, creators)
	if err != nil {
		log.Printf("⚠️  Failed to resolve silence creators: %v", err)
		return
	}

	for i := range silences {
		cb := silences[i].CreatedBy
		if cb == "" {
			continue
		}
		if username, ok := resolved[cb]; ok {
			silences[i].CreatedBy = username
			silences[i].Origin = "notificator"
		} else {
			silences[i].Origin = "external"
		}
	}
}

// silenceDraftRequest is the wire format of the create and preview endpoints. EndsAt
// (absolute RFC3339) and Duration (free-form, parsed by parseExtendedDuration so "2w"
// works) are two ways to bound the silence; Duration is relative to StartsAt.
type silenceDraftRequest struct {
	Sources  []string                     `json:"sources"`
	Matchers []webuimodels.SilenceMatcher `json:"matchers"`
	StartsAt string                       `json:"startsAt"`
	EndsAt   string                       `json:"endsAt"`
	Duration string                       `json:"duration"`
	Comment  string                       `json:"comment"`
}

// matcherError points a validation failure at the matcher row that caused it, so the
// modal can flag that row instead of showing one opaque error for the whole set.
type matcherError struct {
	Index int    `json:"index"`
	Error string `json:"error"`
}

type previewAlert struct {
	Alertname string            `json:"alertname"`
	Labels    map[string]string `json:"labels"`
}

type previewSourceResult struct {
	Source string         `json:"source"`
	Count  int            `json:"count"`
	Sample []previewAlert `json:"sample"`
}

type createSourceResult struct {
	Source    string `json:"source"`
	Success   bool   `json:"success"`
	SilenceID string `json:"silenceId,omitempty"`
	Error     string `json:"error,omitempty"`
}

// previewSampleCap bounds the alert sample list per source — bandwidth, not policy:
// the count itself stays exact and uncapped.
const previewSampleCap = 20

// compileDraftMatchers compiles matcher rows individually so an invalid regex is
// reported against its own row instead of zeroing the whole set the way
// compileMatchers does. Rows with an empty name are skipped: the modal previews while
// the user is still typing. requireNames turns those skipped rows into errors instead
// (creation, where every submitted row must be complete).
func compileDraftMatchers(in []webuimodels.SilenceMatcher, requireNames bool) ([]compiledMatcher, []matcherError) {
	compiled := make([]compiledMatcher, 0, len(in))
	errs := []matcherError{}
	for i, m := range in {
		if m.Name == "" {
			if requireNames {
				errs = append(errs, matcherError{Index: i, Error: "matcher name is required"})
			}
			continue
		}
		cm := compiledMatcher{name: m.Name, value: m.Value, isEqual: m.IsEqual}
		if m.IsRegex {
			// Same anchoring as compileMatchers — and as Alertmanager itself.
			re, err := regexp.Compile("^(?:" + m.Value + ")$")
			if err != nil {
				errs = append(errs, matcherError{Index: i, Error: "invalid regex: " + err.Error()})
				continue
			}
			cm.re = re
		}
		compiled = append(compiled, cm)
	}
	return compiled, errs
}

// previewMatches counts the cached alerts of one source the compiled matchers hit and
// returns a capped sample for the expandable preview list. An empty matcher set matches
// nothing — matchLabels would vacuously match every alert.
func previewMatches(matchers []compiledMatcher, source string, alerts []*webuimodels.DashboardAlert) (int, []previewAlert) {
	sample := []previewAlert{}
	if len(matchers) == 0 {
		return 0, sample
	}
	count := 0
	for _, alert := range alerts {
		if alert.Source != source || !matchLabels(matchers, alert.Labels) {
			continue
		}
		count++
		if len(sample) < previewSampleCap {
			sample = append(sample, previewAlert{Alertname: alert.Labels["alertname"], Labels: alert.Labels})
		}
	}
	return count, sample
}

// validateDraftSources checks the requested sources against the configured
// Alertmanagers and returns a user-facing error message, or "" when valid.
func validateDraftSources(sources, known []string) string {
	if len(sources) == 0 {
		return "at least one source is required"
	}
	for _, source := range sources {
		if !slices.Contains(known, source) {
			return "unknown alertmanager source: " + source
		}
	}
	return ""
}

// buildDraftSilence validates a create request and assembles the silence to post.
// Validation is syntactic only — deliberately no upper duration bound:
// maxSilenceExtension is the extend endpoint's contract, not creation's.
func buildDraftSilence(request silenceDraftRequest, now time.Time) (models.Silence, []matcherError, error) {
	if len(request.Matchers) == 0 {
		return models.Silence{}, nil, errors.New("at least one matcher is required")
	}
	if _, matcherErrors := compileDraftMatchers(request.Matchers, true); len(matcherErrors) > 0 {
		return models.Silence{}, matcherErrors, errors.New("invalid matchers")
	}
	if strings.TrimSpace(request.Comment) == "" {
		return models.Silence{}, nil, errors.New("comment is required")
	}

	startsAt := now
	if request.StartsAt != "" {
		parsed, err := time.Parse(time.RFC3339, request.StartsAt)
		if err != nil {
			return models.Silence{}, nil, errors.New("invalid startsAt: " + err.Error())
		}
		startsAt = parsed
	}

	var endsAt time.Time
	switch {
	case request.EndsAt != "":
		parsed, err := time.Parse(time.RFC3339, request.EndsAt)
		if err != nil {
			return models.Silence{}, nil, errors.New("invalid endsAt: " + err.Error())
		}
		endsAt = parsed
	case request.Duration != "":
		duration, err := parseExtendedDuration(request.Duration)
		if err != nil {
			return models.Silence{}, nil, err
		}
		endsAt = startsAt.Add(duration)
	default:
		return models.Silence{}, nil, errors.New("endsAt or duration is required")
	}
	if !endsAt.After(startsAt) {
		return models.Silence{}, nil, errors.New("endsAt must be after startsAt")
	}

	matchers := make([]models.SilenceMatcher, len(request.Matchers))
	for i, m := range request.Matchers {
		matchers[i] = models.SilenceMatcher{Name: m.Name, Value: m.Value, IsRegex: m.IsRegex, IsEqual: m.IsEqual}
	}

	return models.Silence{
		Matchers: matchers,
		StartsAt: startsAt,
		EndsAt:   endsAt,
		Comment:  strings.TrimSpace(request.Comment),
	}, nil, nil
}

// createSilenceOn is swappable in tests; production goes through the multi-client.
var createSilenceOn = func(source string, silence models.Silence) (*models.Silence, error) {
	return alertmanagerClient.CreateSilenceOnAlertmanager(source, silence)
}

// fanOutSilence posts the silence to every requested source and reports each outcome
// individually — a partial failure names the source that needs a retry instead of
// collapsing to one status the way processSilenceAction does.
func fanOutSilence(sources []string, silence models.Silence) ([]createSourceResult, bool) {
	results := make([]createSourceResult, 0, len(sources))
	allSucceeded := true
	for _, source := range sources {
		created, err := createSilenceOn(source, silence)
		if err != nil {
			allSucceeded = false
			results = append(results, createSourceResult{Source: source, Error: err.Error()})
			continue
		}
		results = append(results, createSourceResult{Source: source, Success: true, SilenceID: created.ID})
	}
	return results, allSucceeded
}

// PreviewSilence reports, per requested source, how many cached alerts the
// in-progress matcher set would silence — called by the modal on every (debounced)
// edit, before anything is created.
func PreviewSilence(c *gin.Context) {
	if alertmanagerClient == nil || alertCache == nil {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Alertmanager client not available"))
		return
	}

	var request silenceDraftRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid request format: "+err.Error()))
		return
	}
	if msg := validateDraftSources(request.Sources, alertmanagerClient.GetClientNames()); msg != "" {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse(msg))
		return
	}

	matchers, matcherErrors := compileDraftMatchers(request.Matchers, false)
	alerts := alertCache.GetAllAlerts()

	results := make([]previewSourceResult, 0, len(request.Sources))
	for _, source := range request.Sources {
		count, sample := previewMatches(matchers, source, alerts)
		results = append(results, previewSourceResult{Source: source, Count: count, Sample: sample})
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"sources":       results,
		"matcherErrors": matcherErrors,
	}))
}

// CreateSilence authors a silence from scratch on every requested Alertmanager.
// Unlike the extend endpoint there is no duration cap — "no artificial limits" is the
// contract here, validation is syntactic only.
func CreateSilence(c *gin.Context) {
	if alertmanagerClient == nil {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Alertmanager client not available"))
		return
	}

	var request silenceDraftRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Invalid request format: "+err.Error()))
		return
	}
	if msg := validateDraftSources(request.Sources, alertmanagerClient.GetClientNames()); msg != "" {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse(msg))
		return
	}

	silence, matcherErrors, err := buildDraftSilence(request, time.Now())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error(), "matcherErrors": matcherErrors})
		return
	}

	// Same createdBy resolution as processSilenceAction: impersonation-aware, with a
	// Username → Email → user-ID fallback so the display field never ends up empty.
	createdBy := middleware.GetEffectiveUserID(c)
	if u := middleware.GetEffectiveUser(c); u != nil {
		switch {
		case u.Username != "":
			createdBy = u.Username
		case u.Email != "":
			createdBy = u.Email
		}
	}
	silence.CreatedBy = createdBy

	results, allSucceeded := fanOutSilence(request.Sources, silence)
	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"results":      results,
		"allSucceeded": allSucceeded,
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
