// Run with: node --test internal/webui/static/js/notification_matcher.test.js
const { test } = require("node:test");
const assert = require("node:assert/strict");
const matchesNotificationFilterPreset = require("./notification_matcher.js");

const alert = {
	source: "prod-am",
	severity: "critical",
	status: { state: "firing" },
	team: "team-a",
	alertName: "HighCPU",
	fingerprint: "fp-1",
	labels: { team: "team-a" },
};

test("no filter (All alerts) always matches", () => {
	assert.equal(matchesNotificationFilterPreset(alert, null), true);
	assert.equal(matchesNotificationFilterPreset(alert, undefined), true);
});

test("empty filter lists impose no constraint", () => {
	assert.equal(
		matchesNotificationFilterPreset(alert, { severities: [], teams: [] }),
		true,
	);
});

test("matches when alert satisfies every populated list", () => {
	assert.equal(
		matchesNotificationFilterPreset(alert, {
			severities: ["critical", "warning"],
			teams: ["team-a", "team-b"],
			alert_names: ["HighCPU"],
		}),
		true,
	);
});

test("does not match when one populated list excludes the alert", () => {
	assert.equal(
		matchesNotificationFilterPreset(alert, { teams: ["team-b"] }),
		false,
	);
});

test("does not match when alert name is not in the list", () => {
	assert.equal(
		matchesNotificationFilterPreset(alert, { alert_names: ["OtherAlert"] }),
		false,
	);
});

test("search matches when a searched field contains the term", () => {
	assert.equal(
		matchesNotificationFilterPreset(alert, { search: "highcpu" }),
		true,
	);
});

test("search excludes when no field contains the term", () => {
	assert.equal(
		matchesNotificationFilterPreset(alert, { search: "infrastructure" }),
		false,
	);
});

test("hidden_alerts excludes an alert matched by fingerprint", () => {
	assert.equal(
		matchesNotificationFilterPreset(alert, {
			hidden_alerts: [{ fingerprint: "fp-1" }],
		}),
		false,
	);
});

test("hidden_rules excludes an alert matched by an enabled label rule", () => {
	assert.equal(
		matchesNotificationFilterPreset(alert, {
			hidden_rules: [{ label_key: "team", label_value: "team-a", is_enabled: true }],
		}),
		false,
	);
});

test("hidden_rules does not exclude when the rule is disabled", () => {
	assert.equal(
		matchesNotificationFilterPreset(alert, {
			hidden_rules: [{ label_key: "team", label_value: "team-a", is_enabled: false }],
		}),
		true,
	);
});
