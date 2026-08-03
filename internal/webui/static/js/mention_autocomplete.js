// @mention autocomplete for the comment textareas. Pure token helpers are
// exported for tests; mentionAutocompleteMixin() is spread into the Alpine
// component that owns newCommentContent (dashboard modal, statistics modal).
// Token rules mirror mention_text.js / activity_handlers.go so what the
// dropdown completes is exactly what the server counts as a mention.
(function (root, factory) {
	if (typeof module === "object" && module.exports) {
		module.exports = factory();
	} else {
		const api = factory();
		root.mentionAutocompleteMixin = api.mixin;
	}
})(typeof self !== "undefined" ? self : this, function () {
	const STOP_CHARS = "<>\"`()[]{},;!?\\/|=*&#%^~$";
	const ADDRESS_END_RE = /[\p{L}\p{N}_\-.]$/u;

	// activeMentionQuery returns {start, query} when the caret sits inside an
	// @token being typed (start = index of "@"), or null. "bob@co" is an
	// address, not a mention, mirroring renderMentionText's prevIsAddress rule.
	function activeMentionQuery(text, caret) {
		const upto = String(text == null ? "" : text).slice(0, caret);
		for (let i = upto.length - 1; i >= 0; i--) {
			const ch = upto[i];
			if (ch === "@") {
				if (i > 0 && ADDRESS_END_RE.test(upto.slice(0, i))) {
					return null;
				}
				return { start: i, query: upto.slice(i + 1) };
			}
			if (/\s/.test(ch) || STOP_CHARS.includes(ch)) {
				return null;
			}
		}
		return null;
	}

	// applyMention replaces the @token between tokenStart and caret with
	// "@username " and returns the new text plus the caret position after it.
	function applyMention(text, tokenStart, caret, username) {
		const value = String(text == null ? "" : text);
		const before = value.slice(0, tokenStart) + "@" + username + " ";
		return { text: before + value.slice(caret), caret: before.length };
	}

	function mixin() {
		return {
			mentionAc: { open: false, items: [], index: 0, start: 0, el: null, seq: 0, timer: null },

			mentionAcOnInput(event) {
				const el = event.target;
				const found = activeMentionQuery(el.value, el.selectionStart);
				if (!found) {
					this.mentionAcClose();
					return;
				}
				this.mentionAc.el = el;
				this.mentionAc.start = found.start;
				clearTimeout(this.mentionAc.timer);
				this.mentionAc.timer = setTimeout(() => this.mentionAcFetch(found.query), 150);
			},

			async mentionAcFetch(query) {
				const seq = ++this.mentionAc.seq;
				try {
					const resp = await fetch("/api/v1/users/mentionable?q=" + encodeURIComponent(query));
					const data = await resp.json();
					if (seq !== this.mentionAc.seq) {
						return; // a newer query is in flight
					}
					const items = (data && data.users) || [];
					this.mentionAc.items = items;
					this.mentionAc.index = 0;
					this.mentionAc.open = items.length > 0;
				} catch (err) {
					if (seq === this.mentionAc.seq) {
						this.mentionAcClose();
					}
				}
			},

			mentionAcOnKeydown(event) {
				if (!this.mentionAc.open) {
					return;
				}
				const count = this.mentionAc.items.length;
				if (event.key === "ArrowDown") {
					event.preventDefault();
					this.mentionAc.index = (this.mentionAc.index + 1) % count;
				} else if (event.key === "ArrowUp") {
					event.preventDefault();
					this.mentionAc.index = (this.mentionAc.index + count - 1) % count;
				} else if ((event.key === "Enter" || event.key === "Tab") && !event.ctrlKey && !event.metaKey) {
					event.preventDefault();
					this.mentionAcSelect(this.mentionAc.items[this.mentionAc.index]);
				} else if (event.key === "Escape") {
					// Close only the dropdown, not the enclosing modal.
					event.stopPropagation();
					this.mentionAcClose();
				}
			},

			mentionAcSelect(user) {
				const el = this.mentionAc.el;
				if (!el || !user) {
					this.mentionAcClose();
					return;
				}
				const applied = applyMention(el.value, this.mentionAc.start, el.selectionStart, user.username);
				this.newCommentContent = applied.text;
				this.mentionAcClose();
				this.$nextTick(() => {
					el.focus();
					el.setSelectionRange(applied.caret, applied.caret);
				});
			},

			mentionAcClose() {
				clearTimeout(this.mentionAc.timer);
				this.mentionAc.open = false;
				this.mentionAc.items = [];
				this.mentionAc.index = 0;
			},
		};
	}

	return { activeMentionQuery: activeMentionQuery, applyMention: applyMention, mixin: mixin };
});
