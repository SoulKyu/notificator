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
