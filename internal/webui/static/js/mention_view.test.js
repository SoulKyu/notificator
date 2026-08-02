// Run with: node --test internal/webui/static/js/mention_view.test.js
//
// Covers the way the mentions badge shipped as a bug: the badge counted a 7-day
// window, the view opened at 1 hour, and the click that opened it marked
// everything seen - so a mention older than an hour was counted, never shown,
// then silently consumed. The watermark now belongs to the view and only moves
// over mentions the view actually rendered.
//
// The page component lives inside a <script> in Activity.templ, so the object
// literal is extracted and evaluated here with stubbed browser globals.
const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const templPath = path.join(__dirname, "../../templates/pages/Activity.templ");
const source = fs.readFileSync(templPath, "utf8");
const literal = source.match(
	/function activityPage\(\) \{\n\t{3}return (\{[\s\S]*?\n\t{3}\});/,
);
assert.ok(literal, "could not extract activityPage literal from templ");

const NOW = 1_700_000_000_000;
const MENTION_WINDOW = 10080;
const mention = (id, ageMs) => ({
	id,
	alertKey: "alert-" + id,
	content: "hey @bob",
	createdAt: new Date(NOW - ageMs).toISOString(),
});

function makePage(overrides = {}) {
	const store = new Map(Object.entries(overrides.storage || {}));
	const dispatched = [];

	const windowStub = {
		location: { search: "?mentions=1", href: "/activity?mentions=1" },
		dispatchEvent: (event) => dispatched.push(event.type),
		CustomEvent: class {
			constructor(type) {
				this.type = type;
			}
		},
	};

	const page = new Function(
		"window",
		"localStorage",
		"CustomEvent",
		"URLSearchParams",
		"fetch",
		"document",
		"console",
		"return " + literal[1] + ";",
	)(
		windowStub,
		{
			getItem: (key) => (store.has(key) ? store.get(key) : null),
			setItem: (key, value) => store.set(key, value),
		},
		windowStub.CustomEvent,
		URLSearchParams,
		async () => {
			throw new Error("no fetch in these tests");
		},
		{ addEventListener: () => {}, visibilityState: "hidden" },
		{ error: () => {}, log: () => {} },
	);

	page.userId = "bob";
	page.mentionWindowMinutes = MENTION_WINDOW;
	page.windowMinutes = MENTION_WINDOW;
	page.mentionsOnly = true;
	Object.assign(page, overrides.state || {});
	return { page, store, dispatched };
}

test("the window selector offers the same window the badge counts", () => {
	const { page } = makePage();
	const values = page.windowOptions.map((o) => o.value);
	assert.ok(
		values.includes(MENTION_WINDOW),
		"the mentions window must be selectable: " + values.join(","),
	);
	assert.equal(page.windowLabel(), "Last 7 days");
});

test("rendering the mentions marks exactly them as seen", () => {
	const { page, store, dispatched } = makePage({
		storage: { notificator_mention_seen_bob: String(NOW - 86_400_000) },
	});
	page.events = [mention("a", 3 * 3_600_000), mention("b", 60_000)];

	page.markMentionsSeen();

	assert.equal(store.get("notificator_mention_seen_bob"), String(NOW - 60_000));
	assert.deepEqual(dispatched, ["notificator:mentions-seen"]);
});

test("an empty mentions view never moves the watermark backwards", () => {
	const { page, store } = makePage({
		storage: { notificator_mention_seen_bob: String(NOW - 60_000) },
	});
	page.events = [];

	page.markMentionsSeen();

	assert.equal(store.get("notificator_mention_seen_bob"), String(NOW - 60_000));
});

test("a narrowed view shows a subset, so it consumes nothing", () => {
	const narrowings = [
		{ search: "payment" },
		{ kinds: ["ack"] },
		{ scope: "mine" },
		{ windowMinutes: 60 },
		{ filters: { severities: ["critical"], teams: [], alertmanagers: [], alertNames: [] } },
	];
	for (const state of narrowings) {
		const { page, store, dispatched } = makePage({
			storage: { notificator_mention_seen_bob: String(NOW - 86_400_000) },
			state,
		});
		page.events = [mention("a", 60_000)];

		page.markMentionsSeen();

		assert.equal(
			store.get("notificator_mention_seen_bob"),
			String(NOW - 86_400_000),
			"watermark moved under " + JSON.stringify(state),
		);
		assert.deepEqual(dispatched, [], "badge cleared under " + JSON.stringify(state));
	}
});

test("the plain activity feed leaves the mentions watermark alone", () => {
	const { page, store } = makePage({
		storage: { notificator_mention_seen_bob: String(NOW - 86_400_000) },
		state: { mentionsOnly: false, windowMinutes: 60 },
	});
	page.events = [mention("a", 60_000)];

	page.markMentionsSeen();

	assert.equal(store.get("notificator_mention_seen_bob"), String(NOW - 86_400_000));
});

test("opening via ?mentions=1 loads the full mentions window", () => {
	const { page } = makePage();
	page.load = async () => {};
	page.syncPolling = () => {};
	page.$el = { dataset: { userId: "bob", mentionWindow: String(MENTION_WINDOW) } };
	page.windowMinutes = 60;

	page.init();

	assert.equal(page.mentionsOnly, true);
	assert.equal(page.windowMinutes, MENTION_WINDOW);
	assert.equal(page.userId, "bob");
});
