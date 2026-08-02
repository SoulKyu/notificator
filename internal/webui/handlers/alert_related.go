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
	LabelKey   string `json:"labelKey"`
	LabelValue string `json:"labelValue"`
	// Count is every alert of the dashboard's classic view carrying this
	// label=value, the open one included — i.e. exactly the number of rows
	// "Filter dashboard on this" leaves in the table.
	Count int `json:"count"`
	// OtherCount is Count minus the open alert: how many entries the list below
	// would hold if it were not capped.
	OtherCount int                   `json:"otherCount"`
	Alerts     []RelatedAlertSummary `json:"alerts"`
	Truncated  bool                  `json:"truncated"`
}

// computeRelatedAlertGroups groups the currently-firing alerts by which of the
// target alert's labels they share the same value for. active is the dashboard's
// classic view (see isClassicViewAlert) minus the user's hidden alerts, target
// included when it belongs there — counting over the very set the table renders
// is what keeps a group's "N firing" equal to the rows the same filter yields.
//
// A label is dropped as noise when only one other alert shares it (not a pattern)
// or when it covers essentially the whole active set (not a distinguishing
// signal). Groups are ordered by count desc (label key asc as tiebreak); the
// listed alerts exclude the target, are capped, and are ordered most recently
// started first.
func computeRelatedAlertGroups(target *webuimodels.DashboardAlert, active []*webuimodels.DashboardAlert) []RelatedLabelGroup {
	if target == nil || len(active) == 0 {
		return nil
	}

	groups := make([]RelatedLabelGroup, 0, len(target.Labels))
	for labelKey, labelValue := range target.Labels {
		count := 0
		var others []*webuimodels.DashboardAlert
		for _, alert := range active {
			if alert.Labels[labelKey] != labelValue {
				continue
			}
			count++
			if alert.Fingerprint != target.Fingerprint {
				others = append(others, alert)
			}
		}

		if len(others) <= 1 {
			continue // shared by at most one other alert — not a pattern
		}
		if float64(count)/float64(len(active)) >= degenerateShareRatio {
			continue // covers essentially the whole active set
		}

		sort.Slice(others, func(i, j int) bool {
			if !others[i].StartsAt.Equal(others[j].StartsAt) {
				return others[i].StartsAt.After(others[j].StartsAt)
			}
			return others[i].Fingerprint < others[j].Fingerprint
		})

		listed := others
		truncated := len(listed) > maxRelatedAlertsPerGroup
		if truncated {
			listed = listed[:maxRelatedAlertsPerGroup]
		}

		alerts := make([]RelatedAlertSummary, len(listed))
		for i, alert := range listed {
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
			OtherCount: len(others),
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

	// Same population as the dashboard table in classic mode: firing only
	// (getStandardAlerts) minus the session's hidden alerts (applyDashboardFilters).
	// Counting over anything wider is how the panel used to claim alerts as
	// "firing" that the dashboard had already resolved or acknowledged away.
	all := alertCache.GetAllAlerts()
	active := make([]*webuimodels.DashboardAlert, 0, len(all))
	for _, alert := range all {
		if !isClassicViewAlert(alert) {
			continue
		}
		if hiddenAlertsService != nil && hiddenAlertsService.IsAlertHidden(sessionID, alert) {
			continue
		}
		active = append(active, alert)
	}

	groups := computeRelatedAlertGroups(target, active)

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{
		"fingerprint": fingerprint,
		"groups":      groups,
	}))
}
