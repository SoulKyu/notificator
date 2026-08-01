// Pure matcher: does an alert fall within a saved filter preset's scope?
// Same list semantics as the server-side alertPassesAlertLevelFilters
// (internal/webui/handlers/dashboard_handlers.go): an empty list means
// "no constraint", a non-empty list means "OR match any value in the list".
(function (root, factory) {
	if (typeof module === "object" && module.exports) {
		module.exports = factory();
	} else {
		root.matchesNotificationFilterPreset = factory();
	}
})(typeof self !== "undefined" ? self : this, function () {
	function listMatches(list, value) {
		if (!list || list.length === 0) {
			return true;
		}
		const normalized = String(value || "").toLowerCase();
		return list.some(function (v) {
			return String(v).toLowerCase() === normalized;
		});
	}

	// filterData is a preset's filter_data (or null/undefined for "All alerts", which always matches).
	function matchesNotificationFilterPreset(alert, filterData) {
		if (!filterData) {
			return true;
		}

		if (!listMatches(filterData.alertmanagers, alert.source)) {
			return false;
		}
		if (!listMatches(filterData.severities, alert.severity || alert.labels?.severity)) {
			return false;
		}
		if (!listMatches(filterData.statuses, alert.status?.state || alert.status)) {
			return false;
		}
		if (!listMatches(filterData.teams, alert.team || alert.labels?.team)) {
			return false;
		}
		if (filterData.alert_names && filterData.alert_names.length > 0) {
			if (!filterData.alert_names.includes(alert.alertName)) {
				return false;
			}
		}

		return true;
	}

	return matchesNotificationFilterPreset;
});
