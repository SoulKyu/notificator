// Regression guard for the alert modal's history state machine, the part that
// browsers own and Go tests cannot reach: opening a related alert from inside the
// modal must stack a real history entry (Back returns to the previous alert), and
// closing must pop the whole chain in one step.
//
// No dependencies, no config:
//   node --test internal/webui/templates/scripts/dashboard_modal_history.test.mjs
// It evals the <script> block out of dashboard_modal.templ, so it tests the
// shipped source rather than a copy.

import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';

const templ = fs.readFileSync(path.join(import.meta.dirname, 'dashboard_modal.templ'), 'utf8');
const mixinSource = templ.match(/<script>([\s\S]*?)<\/script>/)[1];

// Minimal history-owning browser: an entry list plus a synchronous popstate queue.
function browser(initialURL, initialState = {}) {
	const entries = [{ url: initialURL, state: initialState }];
	const pending = [];
	let index = 0;
	let onPopState = null;

	const window = {
		get location() {
			const parsed = new URL('http://host' + entries[index].url);
			return { pathname: parsed.pathname, search: parsed.search, href: 'http://host' + entries[index].url, origin: 'http://host' };
		},
		history: {
			pushState(state, _title, url) {
				entries.splice(index + 1);
				entries.push({ url, state });
				index = entries.length - 1;
			},
			replaceState(state, _title, url) {
				entries[index] = { url, state };
			},
			go(delta) {
				const target = index + delta;
				if (target < 0 || target >= entries.length) return;
				index = target;
				pending.push(() => onPopState({ state: entries[index].state }));
			},
			back() { this.go(-1); }
		},
		addEventListener() {},
		open() {}
	};

	return {
		window,
		url: () => entries[index].url,
		flush: async () => { while (pending.length) await pending.shift()(); },
		setPopStateHandler: (fn) => { onPopState = fn; }
	};
}

function dashboard(initialURL, initialState) {
	const env = browser(initialURL, initialState);
	globalThis.window = env.window;
	let nextFingerprint = null;
	globalThis.fetch = async () => ({
		ok: true,
		json: async () => ({ success: true, data: { alert: { fingerprint: nextFingerprint } } })
	});

	// eslint-disable-next-line no-eval
	eval(mixinSource);

	const app = Object.assign({
		alertModalHistoryDepth: 0,
		alertModalClosing: false,
		showAlertModal: false,
		alertDetails: null,
		currentAlertTab: 'overview',
		alertDetailsLoading: false,
		alertHistory: null,
		sparkline: null,
		relatedAlerts: null,
		relatedError: null,
		alertLinkCopied: false,
		newCommentContent: '',
		commentSubmitting: false,
		commentDeleting: {},
		resolvedAlerts: [],
		loadAlertHistory() {},
		showActionError() {}
	}, env.window.dashboardModalMixin);

	env.setPopStateHandler((event) => app.syncModalWithLocation(event));

	return {
		app,
		history: env.window.history,
		url: env.url,
		open: async (fingerprint) => {
			nextFingerprint = fingerprint;
			await app.showAlertDetails(fingerprint);
			await env.flush();
		},
		// the fingerprint a popstate-driven reopen will resolve to
		expect: (fingerprint) => { nextFingerprint = fingerprint; },
		flush: env.flush,
		openFingerprint: () => (app.showAlertModal ? app.alertDetails?.alert?.fingerprint : null)
	};
}

test('Back from a related alert restores the alert it was opened from', async () => {
	const d = dashboard('/dashboard?severities=critical');

	await d.open('A');
	assert.equal(d.url(), '/dashboard/alert/A?severities=critical');

	await d.open('B'); // clicked in the Related tab
	assert.equal(d.url(), '/dashboard/alert/B?severities=critical');

	d.expect('A');
	d.history.back();
	await d.flush();
	assert.equal(d.openFingerprint(), 'A', 'Back must reopen A, not close the modal');
	assert.equal(d.url(), '/dashboard/alert/A?severities=critical');

	d.history.back();
	await d.flush();
	assert.equal(d.openFingerprint(), null);
	assert.equal(d.url(), '/dashboard?severities=critical', 'pre-modal filters survive');
});

test('closing pops the whole chain, not just the last alert', async () => {
	const d = dashboard('/dashboard?severities=critical');

	await d.open('A');
	await d.open('B');
	await d.open('C');

	d.app.closeAlertModal();
	await d.flush();

	assert.equal(d.openFingerprint(), null);
	assert.equal(d.url(), '/dashboard?severities=critical');
});

test('a deep link has no entry to pop: closing rewrites in place', async () => {
	const d = dashboard('/dashboard/alert/A');
	Object.assign(d.app, { showAlertModal: true, alertDetails: { alert: { fingerprint: 'A' } } });

	await d.open('B');
	d.app.closeAlertModal();
	await d.flush();

	assert.equal(d.openFingerprint(), null, 'closing must not land back on the deep-linked alert');
	assert.equal(d.url(), '/dashboard');
});

test('Forward after a close reopens the alert', async () => {
	const d = dashboard('/dashboard');

	await d.open('A');
	d.app.closeAlertModal();
	await d.flush();
	assert.equal(d.url(), '/dashboard');

	d.expect('A');
	d.history.go(1);
	await d.flush();
	assert.equal(d.openFingerprint(), 'A');
});
