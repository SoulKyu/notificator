package handlers

import (
	"net/http"
	"sort"
	"time"

	"notificator/internal/webui/middleware"
	webuimodels "notificator/internal/webui/models"

	"github.com/gin-gonic/gin"
)

const (
	maxRelatedLabelGroups    = 5
	maxRelatedAlertsPerGroup = 20
	// degenerateShareRatio treats a label as noise once it covers this much of the
	// other active alerts (e.g. env=prod on nearly everything tells you nothing).
	degenerateShareRatio = 0.9
)

// RelatedAlertSummary is the compact per-alert payload inside a related-label group.
type RelatedAlertSummary struct {
	Fingerprint string    `json:"fingerprint"`
	AlertName   string    `json:"alertName"`
	Severity    string    `json:"severity"`
	Instance    string    `json:"instance"`
	StartsAt    time.Time `json:"startsAt"`
}

// RelatedLabelGroup is one correlating label=value with the other firing alerts sharing it.
type RelatedLabelGroup struct {
	LabelKey   string                `json:"labelKey"`
	LabelValue string                `json:"labelValue"`
	Count      int                   `json:"count"` // other firing alerts sharing this label value
	Alerts     []RelatedAlertSummary `json:"alerts"`
	Truncated  bool                  `json:"truncated"`
}

// computeRelatedAlertGroups groups the other currently-firing alerts by which of
// the target alert's labels they share the same value for. A label is dropped as
// noise when only one other alert shares it (not a pattern) or when it covers
// essentially every other alert (not a distinguishing signal). Groups are ordered
// by count desc (label key asc as tiebreak); alerts within a group are capped and
// ordered by most recently started first.
func computeRelatedAlertGroups(target *webuimodels.DashboardAlert, others []*webuimodels.DashboardAlert) []RelatedLabelGroup {
	if target == nil || len(others) == 0 {
		return nil
	}

	groups := make([]RelatedLabelGroup, 0, len(target.Labels))
	for labelKey, labelValue := range target.Labels {
		var matches []*webuimodels.DashboardAlert
		for _, alert := range others {
			if alert.Labels[labelKey] == labelValue {
				matches = append(matches, alert)
			}
		}

		count := len(matches)
		if count <= 1 {
			continue // shared by at most one other alert — not a pattern
		}
		if float64(count)/float64(len(others)) >= degenerateShareRatio {
			continue // covers essentially the whole active set
		}

		sort.Slice(matches, func(i, j int) bool {
			if !matches[i].StartsAt.Equal(matches[j].StartsAt) {
				return matches[i].StartsAt.After(matches[j].StartsAt)
			}
			return matches[i].Fingerprint < matches[j].Fingerprint
		})

		truncated := len(matches) > maxRelatedAlertsPerGroup
		if truncated {
			matches = matches[:maxRelatedAlertsPerGroup]
		}

		alerts := make([]RelatedAlertSummary, len(matches))
		for i, alert := range matches {
			alerts[i] = RelatedAlertSummary{
				Fingerprint: alert.Fingerprint,
				AlertName:   alert.AlertName,
				Severity:    alert.Severity,
				Instance:    alert.Instance,
				StartsAt:    alert.StartsAt,
			}
		}

		groups = append(groups, RelatedLabelGroup{
			LabelKey:   labelKey,
			LabelValue: labelValue,
			Count:      count,
			Alerts:     alerts,
			Truncated:  truncated,
		})
	}

	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Count != groups[j].Count {
			return groups[i].Count > groups[j].Count
		}
		return groups[i].LabelKey < groups[j].LabelKey
	})

	if len(groups) > maxRelatedLabelGroups {
		groups = groups[:maxRelatedLabelGroups]
	}

	return groups
}

// HandleGetRelatedAlerts returns the other currently-firing alerts that share a
// label value with the given alert, grouped by correlating label. Cache-only:
// no Alertmanager call, no backend RPC.
func HandleGetRelatedAlerts(c *gin.Context) {
	fingerprint := c.Param("fingerprint")
	if fingerprint == "" {
		c.JSON(http.StatusBadRequest, webuimodels.ErrorResponse("Alert fingerprint is required"))
		return
	}

	if alertCache == nil {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Dashboard cache not ready"))
		return
	}

	target := alertCache.GetLiveAlert(fingerprint)
	if target == nil {
		c.JSON(http.StatusNotFound, webuimodels.ErrorResponse("Alert not found"))
		return
	}

	sessionID := middleware.GetSessionID(c)

	all := alertCache.GetAllAlerts()
	others := make([]*webuimodels.DashboardAlert, 0, len(all))
	for _, alert := range all {
		if alert.Fingerprint == fingerprint {
			continue
		}
		if hiddenAlertsService != nil && hiddenAlertsService.IsAlertHidden(sessionID, alert) {
			continue
		}
		others = append(others, alert)
	}

	groups := computeRelatedAlertGroups(target, others)

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"fingerprint": fingerprint,
		"groups":      groups,
	}))
}
