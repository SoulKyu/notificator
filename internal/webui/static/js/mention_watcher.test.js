// Run with: node --test internal/webui/static/js/mention_watcher.test.js
//
// Covers the way mention delivery shipped as a bug: the notified watermark was
// advanced on every poll, so a mention polled before the notification stack was
// ready - or on a page that never loads it - was marked delivered and never shown.
// The watcher lives inside a <script> in PageNavigator.templ, so the object
// literal is extracted and evaluated here with stubbed browser globals.
const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const templPath = path.join(
	__dirname,
	"../../templates/components/PageNavigator.templ",
);
const source = fs.readFileSync(templPath, "utf8");
const literal = source.match(
	/function mentionsWatcher\(\) \{\n\t{3}return (\{[\s\S]*?\n\t{3}\});/,
);
assert.ok(literal, "could not extract mentionsWatcher literal from templ");

const NOW = 1_700_000_000_000;
const mention = (id, ageMs) => ({
	id,
	alertKey: "alert-" + id,
	content: "hey @bob",
	createdAt: new Date(NOW - ageMs).toISOString(),
});

// Emulates just enough of the Web Locks API (unavailable under `node --test`)
// to prove the fix serializes notifyFresh across watcher instances: calls
// for the same lock name queue behind one another, callback-then-release.
function makeLocksStub() {
	const queues = new Map();
	return {
		request: async (name, callback) => {
			const prev = queues.get(name) || Promise.resolve();
			let release;
			const gate = new Promise((resolve) => {
				release = resolve;
			});
			queues.set(
				name,
				prev.then(() => gate),
			);
			await prev;
			try {
				return await callback();
			} finally {
				release();
			}
		},
	};
}

function makeWatcher(backend, shared = {}) {
	const notifications = [];
	const store = shared.store || new Map(Object.entries(backend.storage || {}));

	const fetchStub = async (url) => {
		if (url.startsWith("/api/v1/notifications/preferences")) {
			if (!backend.preferencesOk) {
				return { ok: false, status: 503, json: async () => ({}) };
			}
			return {
				ok: true,
				status: 200,
				json: async () => ({
					success: true,
					data: {
						browser_notifications_enabled: backend.browserEnabled,
						enabled_severities: ["info"],
					},
				}),
			};
		}
		throw new Error("unexpected fetch: " + url);
	};

	const NotificationStub = function (title, options) {
		notifications.push({ title, ...options });
	};
	NotificationStub.permission = backend.permission || "granted";

	const windowStub = {
		Notification: NotificationStub,
		location: { pathname: "/silences", href: "/silences" },
	};

	const watcher = new Function(
		"window",
		"Notification",
		"localStorage",
		"fetch",
		"Date",
		"console",
		"navigator",
		"return " + literal[1] + ";",
	)(
		windowStub,
		NotificationStub,
		{
			getItem: (key) => (store.has(key) ? store.get(key) : null),
			setItem: (key, value) => store.set(key, value),
		},
		fetchStub,
		class FrozenDate extends Date {
			static now() {
				return NOW;
			}
		},
		{ error: () => {}, log: () => {} },
		shared.navigator || {},
	);
	watcher.userId = "bob";
	return { watcher, notifications, store };
}

test("first poll seeds the watermark and stays silent on the backlog", async () => {
	const { watcher, notifications, store } = makeWatcher({
		preferencesOk: true,
		browserEnabled: true,
	});

	await watcher.notifyFresh([mention("old", 60_000)]);

	assert.deepEqual(notifications, [], "a pre-existing backlog must not fire");
	assert.equal(store.get(watcher.notifiedKey()), String(NOW));
});

test("a mention that arrives while the page is loading is still notified", async () => {
	const { watcher, notifications, store } = makeWatcher({
		preferencesOk: true,
		browserEnabled: true,
		storage: { notificator_mention_notified_bob: String(NOW - 600_000) },
	});

	await watcher.notifyFresh([mention("fresh", 60_000)]);

	assert.equal(notifications.length, 1);
	assert.equal(notifications[0].title, "You were mentioned");
	assert.equal(store.get(watcher.notifiedKey()), String(NOW - 60_000));
});

test("unreachable preferences leave the mention for the next poll", async () => {
	const backend = {
		preferencesOk: false,
		browserEnabled: true,
		storage: { notificator_mention_notified_bob: String(NOW - 600_000) },
	};
	const { watcher, notifications, store } = makeWatcher(backend);

	await watcher.notifyFresh([mention("fresh", 60_000)]);
	assert.deepEqual(notifications, [], "nothing shown while preferences are down");
	assert.equal(
		store.get(watcher.notifiedKey()),
		String(NOW - 600_000),
		"the watermark must not eat an undelivered mention",
	);

	backend.preferencesOk = true;
	await watcher.notifyFresh([mention("fresh", 60_000)]);
	assert.equal(notifications.length, 1, "the retry delivers it");
	assert.equal(store.get(watcher.notifiedKey()), String(NOW - 60_000));
});

test("notifications turned off drop the mention instead of retrying forever", async () => {
	const { watcher, notifications, store } = makeWatcher({
		preferencesOk: true,
		browserEnabled: false,
		storage: { notificator_mention_notified_bob: String(NOW - 600_000) },
	});

	await watcher.notifyFresh([mention("fresh", 60_000)]);

	assert.deepEqual(notifications, []);
	assert.equal(store.get(watcher.notifiedKey()), String(NOW - 60_000));
});

test("denied browser permission drops the mention without asking the backend", async () => {
	const { watcher, notifications, store } = makeWatcher({
		preferencesOk: false, // any fetch here would be a 503 -> a retry loop
		permission: "denied",
		storage: { notificator_mention_notified_bob: String(NOW - 600_000) },
	});

	await watcher.notifyFresh([mention("fresh", 60_000)]);

	assert.deepEqual(notifications, []);
	assert.equal(store.get(watcher.notifiedKey()), String(NOW - 60_000));
});

test("only the fresh mentions are notified, once", async () => {
	const { watcher, notifications } = makeWatcher({
		preferencesOk: true,
		browserEnabled: true,
		storage: { notificator_mention_notified_bob: String(NOW - 300_000) },
	});
	const events = [
		mention("old", 600_000),
		mention("a", 120_000),
		mention("b", 60_000),
	];

	await watcher.notifyFresh(events);
	assert.deepEqual(
		notifications.map((n) => n.tag),
		["mention-a", "mention-b"],
	);

	await watcher.notifyFresh(events);
	assert.equal(notifications.length, 2, "a second poll must not re-notify");
});

test("two tabs racing on the same watermark notify only once", async () => {
	const store = new Map(
		Object.entries({
			notificator_mention_notified_bob: String(NOW - 600_000),
		}),
	);
	const navigatorStub = { locks: makeLocksStub() };
	const backend = { preferencesOk: true, browserEnabled: true };

	const a = makeWatcher(backend, { store, navigator: navigatorStub });
	const b = makeWatcher(backend, { store, navigator: navigatorStub });

	await Promise.all([
		a.watcher.notifyFresh([mention("fresh", 60_000)]),
		b.watcher.notifyFresh([mention("fresh", 60_000)]),
	]);

	assert.equal(
		a.notifications.length + b.notifications.length,
		1,
		"only one tab should deliver the mention",
	);
});
