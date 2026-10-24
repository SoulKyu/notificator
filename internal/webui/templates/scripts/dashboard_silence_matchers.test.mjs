// Regression guard for silence matcher seeding.
// Both silenceAlert() (table row / keyboard shortcut) and silenceCurrentAlert()
// (alert detail modal) must pre-populate silenceMatcherRows from the alert's
// labels identically. Logic lives in prepareSilenceModalForAlert() in
// dashboard_actions.templ, called by both entry points.
//
// No dependencies, no config:
//   node --test internal/webui/templates/scripts/dashboard_silence_matchers.test.mjs

import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';

const dir = import.meta.dirname;
const actionsSrc = fs.readFileSync(path.join(dir, 'dashboard_actions.templ'), 'utf8');
const modalSrc   = fs.readFileSync(path.join(dir, 'dashboard_modal.templ'), 'utf8');

const actionsMixinSource = actionsSrc.match(/<script>([\s\S]*?)<\/script>/)[1];
const modalMixinSource   = modalSrc.match(/<script>([\s\S]*?)<\/script>/)[1];

function makeApp(alerts = [], alertDetails = null) {
	globalThis.window = { dashboardActionsMixin: {}, dashboardModalMixin: {} };
	// eslint-disable-next-line no-eval
	eval(actionsMixinSource);
	// eslint-disable-next-line no-eval
	eval(modalMixinSource);

	const loadCalls    = [];
	const previewCalls = [];

	const app = Object.assign({
		alerts,
		alertDetails: alertDetails ? { alert: alertDetails } : null,
		showSilenceModal: false,
		showSilenceMatchers: false,
		silenceMatcherRows: [],
		silenceMatcherErrors: [],
		silencePreviewCount: null,
		silencePreviewError: '',
		silenceAction: '',
		silenceReason: '',
		silenceError: '',
		silenceDuration: '',
		silenceDurationType: '',
		customSilenceDuration: '',
		customDurationError: '',
		currentSilenceAlert: null,
	}, globalThis.window.dashboardActionsMixin, globalThis.window.dashboardModalMixin);

	// Override after mixin merge so stubs win over the real implementations.
	app.loadSilenceLabelOptions = () => loadCalls.push(true);
	app.scheduleSilencePreview  = () => previewCalls.push(true);
	app._loadCalls    = loadCalls;
	app._previewCalls = previewCalls;
	return app;
}

const ALERT = {
	fingerprint: 'fp1',
	labels: {
		alertname: 'HighCPU',
		severity:  'critical',
		instance:  'host:9090',
		__name__:  'up',
		__tmp_foo: 'bar',
		__tmp_:    'exact',
	},
};

test('silenceAlert seeds matchers from alert labels', () => {
	const app = makeApp([ALERT]);
	app.silenceAlert('fp1');

	assert.equal(app.showSilenceModal, true);
	assert.equal(app.silenceAction, 'single');
	assert.equal(app.currentSilenceAlert, ALERT);
	assert.equal(app.showSilenceMatchers, false);

	const names = app.silenceMatcherRows.map(r => r.name);
	assert.ok(names.includes('alertname'), 'alertname must be seeded');
	assert.ok(names.includes('severity'),  'severity must be seeded');
	assert.ok(names.includes('instance'),  'instance must be seeded');
	assert.ok(!names.includes('__name__'), '__name__ must be filtered');
	assert.ok(!names.includes('__tmp_foo'), '__tmp_foo must be filtered');
	assert.ok(!names.includes('__tmp_'),   '__tmp_ exact must be filtered');

	const row = app.silenceMatcherRows.find(r => r.name === 'alertname');
	assert.equal(row.value, 'HighCPU');
	assert.equal(row.op,    '=');
});

test('silenceCurrentAlert seeds matchers identically', () => {
	const app = makeApp([], ALERT);
	app.silenceCurrentAlert();

	assert.equal(app.showSilenceModal, true);
	assert.equal(app.silenceAction, 'single');
	assert.equal(app.currentSilenceAlert, ALERT);
	assert.equal(app.showSilenceMatchers, false);

	const names = app.silenceMatcherRows.map(r => r.name);
	assert.ok(names.includes('alertname'));
	assert.ok(!names.includes('__name__'));
	assert.ok(!names.includes('__tmp_foo'));
});

test('both paths produce identical matcher rows', () => {
	const app1 = makeApp([ALERT]);
	app1.silenceAlert('fp1');

	const app2 = makeApp([], ALERT);
	app2.silenceCurrentAlert();

	assert.deepEqual(app1.silenceMatcherRows, app2.silenceMatcherRows);
});

test('both paths call loadSilenceLabelOptions and scheduleSilencePreview', () => {
	const app1 = makeApp([ALERT]);
	app1.silenceAlert('fp1');
	assert.equal(app1._loadCalls.length,    1, 'silenceAlert must call loadSilenceLabelOptions');
	assert.equal(app1._previewCalls.length, 1, 'silenceAlert must call scheduleSilencePreview');

	const app2 = makeApp([], ALERT);
	app2.silenceCurrentAlert();
	assert.equal(app2._loadCalls.length,    1, 'silenceCurrentAlert must call loadSilenceLabelOptions');
	assert.equal(app2._previewCalls.length, 1, 'silenceCurrentAlert must call scheduleSilencePreview');
});

test('silenceMatchersInvalid is false when labels are seeded', () => {
	const app = makeApp([ALERT]);
	app.silenceAlert('fp1');
	assert.equal(app.silenceMatchersInvalid(), false);
});

test('silenceAlert is a no-op for unknown fingerprint', () => {
	const app = makeApp([ALERT]);
	app.silenceAlert('unknown');
	assert.equal(app.showSilenceModal, false);
	assert.equal(app.silenceMatcherRows.length, 0);
});

test('silenceCurrentAlert is a no-op when no alert details', () => {
	const app = makeApp();
	app.silenceCurrentAlert();
	assert.equal(app.showSilenceModal, false);
});

test('alert with no labels seeds empty rows without error', () => {
	const bare = { fingerprint: 'fp2', labels: {} };
	const app = makeApp([bare]);
	app.silenceAlert('fp2');
	assert.equal(app.silenceMatcherRows.length, 0);
	assert.equal(app.showSilenceModal, true);
});
