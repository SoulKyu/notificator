const { test } = require("node:test");
const assert = require("node:assert/strict");
const { activeMentionQuery, applyMention } = require("./mention_autocomplete.js");

test("caret right after @ yields empty query", () => {
	assert.deepEqual(activeMentionQuery("@", 1), { start: 0, query: "" });
});

test("caret inside a token yields the typed prefix", () => {
	assert.deepEqual(activeMentionQuery("cc @mar", 7), { start: 3, query: "mar" });
});

test("caret before the @ is not a mention", () => {
	assert.equal(activeMentionQuery("cc @mar", 2), null);
});

test("whitespace between @ and caret ends the token", () => {
	assert.equal(activeMentionQuery("@bob hello", 8), null);
});

test("email-shaped text is an address, not a mention", () => {
	assert.equal(activeMentionQuery("bob@co", 6), null);
});

test("stop char ends the token", () => {
	assert.equal(activeMentionQuery("(@bob)", 6), null);
	assert.deepEqual(activeMentionQuery("(@bob", 5), { start: 1, query: "bob" });
});

test("dots and dashes stay in the query", () => {
	assert.deepEqual(activeMentionQuery("@bob.sm", 7), { start: 0, query: "bob.sm" });
});

test("applyMention replaces the token and appends a space", () => {
	assert.deepEqual(applyMention("cc @mar tail", 3, 7, "marie"), {
		text: "cc @marie  tail",
		caret: 10,
	});
});

test("applyMention at end of text", () => {
	assert.deepEqual(applyMention("@m", 0, 2, "marie"), { text: "@marie ", caret: 7 });
});
