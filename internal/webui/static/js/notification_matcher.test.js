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
