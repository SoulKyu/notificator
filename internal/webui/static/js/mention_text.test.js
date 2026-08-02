const { test } = require("node:test");
const assert = require("node:assert/strict");
const renderMentionText = require("./mention_text.js");

test("plain text with no mention is escaped and unchanged otherwise", () => {
	assert.equal(renderMentionText("no mentions here", "bob"), "no mentions here");
});

test("wraps a mention in a span", () => {
	assert.equal(
		renderMentionText("hey @bob check this", "alice"),
		'hey <span class="mention inline-block px-1 rounded bg-blue-100 text-blue-800 dark:bg-blue-900/50 dark:text-blue-300 font-medium">@bob</span> check this',
	);
});

test("the mention addressed to the current user gets the distinct style", () => {
	const html = renderMentionText("hey @bob", "bob");
	assert.match(html, /mention-me/);
});

test("mention matching is case-insensitive", () => {
	const html = renderMentionText("hey @Bob", "bob");
	assert.match(html, /mention-me/);
});

test("html in comment content is escaped, not executed", () => {
	assert.equal(
		renderMentionText("<script>alert(1)</script>", "bob"),
		"&lt;script&gt;alert(1)&lt;/script&gt;",
	);
});

test("does not highlight a mention embedded after a longer token", () => {
	assert.equal(
		renderMentionText("contact db01@bob.internal now", "bob"),
		"contact db01@bob.internal now",
	);
});

test("a longer non-ascii handle is one mention, not addressed to the shorter user", () => {
	const html = renderMentionText("handover to @bobé only", "bob");
	assert.doesNotMatch(html, /mention-me/, "@bobé is not a mention of bob");
	assert.match(html, />@bobé</, "the handle is wrapped whole");
});

test("the non-ascii handle is addressed to its own owner", () => {
	assert.match(renderMentionText("handover to @bobé", "bobé"), /mention-me/);
});

test("a dotted handle is one mention, not addressed to its prefix", () => {
	const html = renderMentionText("@bob.smith please check", "bob");
	assert.doesNotMatch(html, /mention-me/, "@bob.smith is not a mention of bob");
	assert.match(html, />@bob\.smith</, "the handle is wrapped whole");
});

test("the dotted handle is addressed to its own owner", () => {
	assert.match(renderMentionText("@bob.smith please check", "bob.smith"), /mention-me/);
});

test("a trailing period is punctuation, not part of the handle", () => {
	const html = renderMentionText("please ping @bob.", "bob");
	assert.match(html, /mention-me/);
	assert.match(html, /<\/span>\.$/, "the period stays outside the handle");
});

test("an apostrophe handle is one mention", () => {
	assert.match(renderMentionText("handover to @o'brien now", "o'brien"), /mention-me/);
});

test("a combining mark makes a different handle", () => {
	assert.doesNotMatch(renderMentionText("handover to @bob́", "bob"), /mention-me/);
});

test("html around a mention is escaped and the handle is not extended into it", () => {
	const html = renderMentionText('@bob <img src=x onerror="x">', "bob");
	assert.match(html, /mention-me/);
	assert.match(html, />@bob</);
	assert.doesNotMatch(html, /<img/);
	assert.match(html, /&lt;img src=x onerror=&quot;x&quot;&gt;/);
});

test("an ampersand next to a mention is escaped once, not read as a handle character", () => {
	assert.equal(
		renderMentionText("cc @alice & carol", "carol"),
		'cc <span class="mention inline-block px-1 rounded bg-blue-100 text-blue-800 dark:bg-blue-900/50 dark:text-blue-300 font-medium">@alice</span> &amp; carol',
	);
});
