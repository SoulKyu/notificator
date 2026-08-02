// Run with: node --test internal/webui/static/js/notification_scope.test.js
//
// Covers the two ways notification scope can go wrong at runtime, both of which
// shipped as bugs: notifying on a preset the user has edited away from, and
// persisting default preferences over the saved ones after a failed load.
// The service lives inside a <script> in notification_service.templ, so the
// object literal is extracted and evaluated here with stubbed browser globals.
const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const matchesNotificationFilterPreset = require("./notification_matcher.js");

const templPath = path.join(
	__dirname,
	"../../templates/scripts/notification_service.templ",
);
const source = fs.readFileSync(templPath, "utf8");
const literal = source.match(
	/window\.NotificationService = (\{[\s\S]*?\n\t\t\});/,
);
assert.ok(literal, "could not extract NotificationService literal from templ");

function makeService(backend) {
	const notifications = [];
	const posts = [];
	const fetchStub = async (url, options) => {
		if (url.startsWith("/api/v1/dashboard/filter-presets")) {
			if (!backend.presetsOk) {
				return { ok: false, status: 503, json: async () => ({}) };
			}
			return {
				ok: true,
				status: 200,
				json: async () => ({ success: true, presets: backend.presets }),
			};
		}
		if (url.startsWith("/api/v1/notifications/preferences")) {
			if (options && options.method === "POST") {
				posts.push(JSON.parse(options.body));
				return {
					ok: true,
					status: 200,
					json: async () => ({ success: true }),
				};
			}
			if (!backend.preferencesOk) {
				return { ok: false, status: 503, json: async () => ({}) };
			}
			return {
				ok: true,
				status: 200,
				json: async () => ({ success: true, data: backend.preferences }),
			};
		}
		throw new Error("unexpected fetch: " + url);
	};

	const NotificationStub = function (title) {
		notifications.push(title);
	};
	const service = new Function(
		"matchesNotificationFilterPreset",
		"window",
		"Notification",
		"Audio",
		"localStorage",
		"fetch",
		"return " + literal[1] + ";",
	)(
		matchesNotificationFilterPreset,
		{},
		NotificationStub,
		function () {},
		{ getItem: () => null, setItem: () => {} },
		fetchStub,
	);
	return { service, notifications, posts };
}

const alertFor = (team, fingerprint) => ({
	fingerprint,
	alertName: "HighCPU",
	severity: "critical",
	team,
	labels: { team, severity: "critical" },
});

test("editing the selected preset re-scopes notifications without a reload", async () => {
	const backend = {
		presetsOk: true,
		presets: [{ id: "p1", filter_data: { teams: ["team-a"] } }],
	};
	const { service, notifications } = makeService(backend);
	service.permissionGranted = true;
	service.preferencesLoaded = true;
	service.preferences = {
		browserNotificationsEnabled: true,
		enabledSeverities: ["critical"],
		soundNotificationsEnabled: false,
		notificationFilterPresetId: "p1",
	};

	await service.showNotification(alertFor("team-a", "fp-1"));
	assert.equal(notifications.length, 1, "team-a is in scope");

	// The user edits the preset in another tab / another window.
	backend.presets = [{ id: "p1", filter_data: { teams: ["team-b"] } }];

	await service.showNotification(alertFor("team-a", "fp-2"));
	assert.equal(notifications.length, 1, "team-a is no longer in scope");
	await service.showNotification(alertFor("team-b", "fp-3"));
	assert.equal(notifications.length, 2, "team-b is now in scope");
});

test("switching preset fails closed while the refresh is in flight", async () => {
	const backend = {
		presetsOk: true,
		presets: [
			{ id: "p1", filter_data: { teams: ["team-a"] } },
			{ id: "p2", filter_data: { teams: ["team-b"] } },
		],
	};
	const { service } = makeService(backend);
	service.preferences.notificationFilterPresetId = "p1";
	await service.refreshFilterPresetData();
	assert.equal(service.matchesFilterPreset(alertFor("team-a", "fp-1")), true);

	service.preferences.notificationFilterPresetId = "p2";
	const inFlight = service.refreshFilterPresetData();
	assert.equal(
		service.matchesFilterPreset(alertFor("team-a", "fp-2")),
		false,
		"old preset's scope must not be reused while p2 loads",
	);
	await inFlight;
	assert.equal(service.matchesFilterPreset(alertFor("team-b", "fp-3")), true);
});

test("a failed preferences load never writes defaults back", async () => {
	const backend = {
		presetsOk: true,
		presets: [{ id: "p1", filter_data: { teams: ["team-a"] } }],
		preferencesOk: false,
	};
	const { service, posts } = makeService(backend);
	await service.loadPreferences();
	assert.equal(service.preferencesLoaded, false);

	service.preferences.browserNotificationsEnabled = true;
	assert.equal(await service.savePreferences(service.preferences), false);
	assert.deepEqual(posts, [], "nothing may be persisted over the saved values");
});

test("preferences that loaded are saved normally", async () => {
	const backend = {
		presetsOk: true,
		presets: [{ id: "p1", filter_data: { teams: ["team-a"] } }],
		preferencesOk: true,
		preferences: {
			browser_notifications_enabled: true,
			enabled_severities: ["critical"],
			sound_notifications_enabled: false,
			notification_filter_preset_id: "p1",
		},
	};
	const { service, posts } = makeService(backend);
	await service.loadPreferences();
	assert.equal(service.preferencesLoaded, true);

	assert.equal(await service.savePreferences(service.preferences), true);
	assert.equal(posts.length, 1);
	assert.equal(posts[0].notification_filter_preset_id, "p1");
});
