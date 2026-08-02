// Pure renderer: turns comment text into HTML with @mentions highlighted.
// Mirrors mentionsUsername (internal/webui/handlers/activity_handlers.go) closely
// enough for display purposes: any @token is treated as a mention, and the one
// matching currentUsername (case-insensitive) is styled distinctly.
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

	function renderMentionText(content, currentUsername) {
		const escaped = escapeHtml(content);
		const me = String(currentUsername || "").toLowerCase();
		return escaped.replace(/(?<![A-Za-z0-9_-])@([A-Za-z0-9_-]+)/g, function (match, name) {
			const isMe = me !== "" && name.toLowerCase() === me;
			const cls = isMe
				? "mention mention-me inline-block px-1 rounded bg-blue-600 text-white font-semibold"
				: "mention inline-block px-1 rounded bg-blue-100 text-blue-800 dark:bg-blue-900/50 dark:text-blue-300 font-medium";
			return '<span class="' + cls + '">' + match + "</span>";
		});
	}

	return renderMentionText;
});
