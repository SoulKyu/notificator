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
	// Mirrors contains() (internal/webui/handlers/dashboard_handlers.go): exact,
	// case-sensitive match, so notification scope never diverges from dashboard scope.
	function listMatches(list, value) {
		if (!list || list.length === 0) {
			return true;
		}
		const normalized = String(value || "");
		return list.some(function (v) {
			return String(v) === normalized;
		});
	}

	// Mirrors matchesSearch (internal/webui/handlers/dashboard_handlers.go): case-insensitive
	// substring match against the same alert fields plus labels.
	function matchesSearch(alert, search) {
		if (!search) {
			return true;
		}
		const searchLower = String(search).toLowerCase();
		const fields = [alert.alertName, alert.instance, alert.summary, alert.team, alert.source];
		if (fields.some((f) => String(f || "").toLowerCase().includes(searchLower))) {
			return true;
		}
		const labels = alert.labels || {};
		return Object.keys(labels).some(function (key) {
			return key.toLowerCase().includes(searchLower) || String(labels[key] || "").toLowerCase().includes(searchLower);
		});
	}

	// Mirrors alertMatchesHiddenRule (internal/webui/templates/scripts/dashboard_data.templ).
	function matchesHiddenRule(alert, rule) {
		if (!rule || !rule.is_enabled) {
			return false;
		}
		const labelValue = (alert.labels || {})[rule.label_key];
		if (labelValue === undefined) {
			return false;
		}
		if (rule.is_regex) {
			if (rule.label_value === "") {
				return false;
			}
			try {
				return new RegExp(rule.label_value).test(labelValue);
			} catch (e) {
				return false;
			}
		}
		return rule.label_value === "" || rule.label_value === labelValue;
	}

	function isHidden(alert, filterData) {
		const hiddenAlerts = filterData.hidden_alerts || [];
		if (hiddenAlerts.some((h) => h.fingerprint === alert.fingerprint)) {
			return true;
		}
		const hiddenRules = filterData.hidden_rules || [];
		return hiddenRules.some((rule) => matchesHiddenRule(alert, rule));
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
		if (!matchesSearch(alert, filterData.search)) {
			return false;
		}
		if (isHidden(alert, filterData)) {
			return false;
		}

		return true;
	}

	return matchesNotificationFilterPreset;
});
