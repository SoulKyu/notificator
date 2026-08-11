const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

// Same trick as notification_scope.test.js: the component lives inline in the
// templ source, so extract it and run the real code rather than a copy.
const templPath = path.join(__dirname, "../../templates/pages/Profile.templ");
const source = fs.readFileSync(templPath, "utf8");
const literal = source.match(/\n\t\tfunction profilePage\(\) \{\n([\s\S]*?)\n\t\t\}\n/);
assert.ok(literal, "could not extract profilePage() from Profile.templ");
const profilePage = new Function(literal[1]);

function deferredFetch() {
	const calls = [];
	let settle;
	global.fetch = (url, options) => {
		calls.push(JSON.parse(options.body));
		return new Promise((resolve) => {
			settle = (ok, body) => resolve({ ok, json: async () => body });
		});
	};
	return { calls, settle: (ok, body) => settle(ok, body) };
}

function openWithCredentials() {
	const component = profilePage();
	component.openChangePasswordModal();
	component.currentPassword = "old";
	component.newPassword = "NewPass123";
	component.confirmPassword = "NewPass123";
	return component;
}

test("a second submit while the request is in flight never reaches the server", async () => {
	const fetcher = deferredFetch();
	const component = openWithCredentials();

	const inFlight = component.changePassword();
	await component.changePassword(); // the Enter key pressed again

	assert.equal(fetcher.calls.length, 1);

	fetcher.settle(true, { success: true });
	await inFlight;

	assert.equal(component.changePasswordSuccess, true);
	assert.equal(component.changePasswordError, "");
});

test("submitting again after success does not paint an error over the banner", async () => {
	const fetcher = deferredFetch();
	const component = openWithCredentials();

	const inFlight = component.changePassword();
	fetcher.settle(true, { success: true });
	await inFlight;

	await component.changePassword(); // fields are cleared, Enter pressed again

	assert.equal(fetcher.calls.length, 1);
	assert.equal(component.changePasswordSuccess, true);
	assert.equal(component.changePasswordError, "");
});

test("a response landing after close + reopen cannot write to the modal", async () => {
	const fetcher = deferredFetch();
	const component = openWithCredentials();

	const inFlight = component.changePassword();
	component.closeChangePasswordModal();
	component.openChangePasswordModal();

	fetcher.settle(false, { success: false, error: "Current password is incorrect" });
	await inFlight;

	assert.equal(component.showChangePasswordModal, true);
	assert.equal(component.changePasswordError, "");
	assert.equal(component.changePasswordSuccess, false);
	assert.equal(component.changePasswordLoading, false);
});

test("validation still rejects bad input before any request", async () => {
	const fetcher = deferredFetch();
	const component = profilePage();
	component.openChangePasswordModal();

	await component.changePassword();
	assert.equal(component.changePasswordError, "All fields are required");

	component.currentPassword = "old";
	component.newPassword = "NewPass123";
	component.confirmPassword = "Mismatch";
	await component.changePassword();
	assert.equal(component.changePasswordError, "New password and confirmation do not match");

	component.confirmPassword = "abc";
	component.newPassword = "abc";
	await component.changePassword();
	assert.equal(component.changePasswordError, "New password must be at least 4 characters long");

	assert.equal(fetcher.calls.length, 0);
});
