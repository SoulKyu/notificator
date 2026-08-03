// Pure renderer: turns comment text into HTML with @mentions highlighted.
// Applies the same handle rule as mentionsUsername
// (internal/webui/handlers/activity_handlers.go), so what is highlighted as addressed
// to you is exactly what the server counts as a mention of you.
(function (root, factory) {
	if (typeof module === "object" && module.exports) {
		module.exports = factory();
	} else {
		root.renderMentionText = factory();
	}
})(typeof self !== "undefined" ? self : this, function () {
	function escapeHtml(value) {
		return String(value == null ? "" : value)
			.replace(/&/g, "&amp;")
			.replace(/</g, "&lt;")
			.replace(/>/g, "&gt;")
			.replace(/"/g, "&quot;")
			.replace(/'/g, "&#39;");
	}

	// A handle is the whole run after "@" up to the first character that cannot occur in
	// a username, compared whole. Deliberately not an allowlist of handle characters:
	// usernames have no charset rule, and every character missing from such a list
	// truncated the handle and highlighted someone else's mention as yours - a class
	// without "." rendered "@bob.smith" as "@bob" + ".smith" for bob.
	// "@" and "+" are handle characters, not stops: e-mail-shaped usernames
	// (bob@corp.com) and plus-tags (bob+oncall) must be read whole, mirroring
	// mentionStopRunes in activity_handlers.go.
	const STOP_CHARS = "<>\"`()[]{},;!?\\/|=*&#%^~$";
	const TRIM_CHARS = ".:'";
	const ADDRESS_END_RE = /[\p{L}\p{N}_\-.]$/u;

	// handleAt returns the handle beginning at start (just past an "@"), trailing sentence
	// punctuation removed so "cc @bob." mentions bob while "@bob.smith" does not.
	function handleAt(text, start) {
		let end = start;
		for (const ch of text.slice(start)) {
			if (/\s/.test(ch) || STOP_CHARS.includes(ch)) break;
			end += ch.length;
		}
		let handle = text.slice(start, end);
		while (handle && TRIM_CHARS.includes(handle[handle.length - 1])) {
			handle = handle.slice(0, -1);
		}
		return handle;
	}

	// Scanning runs on the raw text and each chunk is escaped as it is emitted, so an
	// escape sequence ("&amp;") is never mistaken for handle characters, and everything
	// that reaches the DOM - handles included - is still escaped.
	function renderMentionText(content, currentUsername) {
		const text = String(content == null ? "" : content);
		const me = String(currentUsername || "").toLowerCase();
		let out = "";
		let plain = 0;
		let i = 0;
		while (i < text.length) {
			const at = text.indexOf("@", i);
			if (at === -1) break;
			const handle = handleAt(text, at + 1);
			// A handle glued to a preceding word is an address, not a mention:
			// "ops-team@bob", "db01@bob.internal". Tested against the full text
			// (not a single code unit) so a supplementary-plane letter before "@"
			// (a surrogate pair) is still recognized as an address character.
			const prevIsAddress = ADDRESS_END_RE.test(text.slice(0, at));
			if (!handle || prevIsAddress) {
				i = at + 1;
				continue;
			}
			const cls = me !== "" && handle.toLowerCase() === me
				? "mention mention-me inline-block px-1 rounded bg-blue-600 text-white font-semibold"
				: "mention inline-block px-1 rounded bg-blue-100 text-blue-800 dark:bg-blue-900/50 dark:text-blue-300 font-medium";
			out += escapeHtml(text.slice(plain, at));
			out += '<span class="' + cls + '">' + escapeHtml("@" + handle) + "</span>";
			i = at + 1 + handle.length;
			plain = i;
		}
		return out + escapeHtml(text.slice(plain));
	}

	return renderMentionText;
});
