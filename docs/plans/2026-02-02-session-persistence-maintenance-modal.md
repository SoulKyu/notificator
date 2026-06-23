# Session Persistence & Maintenance Modal Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Prevent unnecessary SSO redirects during backend restarts by showing a maintenance modal and persisting sessions across restarts.

**Architecture:** Add a global MaintenanceModal component that monitors `/health/backend` every 5 seconds. When backend is unreachable, show a blur overlay with "Maintenance in progress" message. Configure `NOTIFICATOR_SESSION_SECRET` for session persistence.

**Tech Stack:** Go templ, Alpine.js, Tailwind CSS, Docker Compose

---

## Task 1: Create MaintenanceModal Component

**Files:**
- Create: `internal/webui/templates/components/MaintenanceModal.templ`

**Step 1: Create the component file**

```go
package components

templ MaintenanceModal() {
	<!-- Maintenance Modal - monitors backend health and shows overlay when unavailable -->
	<div x-data="maintenanceMonitor()" x-init="init()">
		<!-- Modal Overlay -->
		<div x-show="backendDown"
			 x-cloak
			 x-transition:enter="transition ease-out duration-300"
			 x-transition:enter-start="opacity-0"
			 x-transition:enter-end="opacity-100"
			 x-transition:leave="transition ease-in duration-200"
			 x-transition:leave-start="opacity-100"
			 x-transition:leave-end="opacity-0"
			 class="fixed inset-0 z-[100] flex items-center justify-center bg-slate-900/50 backdrop-blur-sm">
			<!-- Modal Card -->
			<div class="bg-white dark:bg-slate-800 rounded-xl shadow-2xl p-8 max-w-md mx-4 text-center">
				<!-- Icon -->
				<div class="mx-auto w-16 h-16 bg-amber-100 dark:bg-amber-900/30 rounded-full flex items-center justify-center mb-6">
					<svg class="w-8 h-8 text-amber-600 dark:text-amber-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z"></path>
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"></path>
					</svg>
				</div>
				<!-- Title -->
				<h2 class="text-xl font-semibold text-slate-900 dark:text-white mb-2">
					Maintenance in progress
				</h2>
				<!-- Message -->
				<p class="text-slate-600 dark:text-slate-400 mb-6">
					Please wait while we restore the service...
				</p>
				<!-- Loading Spinner -->
				<div class="flex justify-center mb-4">
					<svg class="animate-spin h-6 w-6 text-amber-600 dark:text-amber-400" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
						<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
						<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
					</svg>
				</div>
				<!-- Countdown -->
				<p class="text-sm text-slate-500 dark:text-slate-500">
					Reconnecting in <span x-text="countdown" class="font-medium text-amber-600 dark:text-amber-400"></span>s...
				</p>
			</div>
		</div>
	</div>

	<script>
		function maintenanceMonitor() {
			return {
				backendDown: false,
				countdown: 5,
				intervalId: null,
				countdownId: null,

				init() {
					// Start monitoring after a short delay to avoid flash on page load
					setTimeout(() => {
						this.checkBackend();
						this.startMonitoring();
					}, 1000);
				},

				async checkBackend() {
					try {
						const controller = new AbortController();
						const timeoutId = setTimeout(() => controller.abort(), 3000);

						const response = await fetch('/health/backend', {
							signal: controller.signal
						});
						clearTimeout(timeoutId);

						if (response.ok) {
							this.onBackendRestored();
						} else {
							this.onBackendDown();
						}
					} catch (error) {
						this.onBackendDown();
					}
				},

				onBackendDown() {
					if (!this.backendDown) {
						this.backendDown = true;
						this.startCountdown();
						console.log('[MaintenanceMonitor] Backend is down, showing maintenance modal');
					}
				},

				onBackendRestored() {
					if (this.backendDown) {
						this.backendDown = false;
						this.stopCountdown();
						console.log('[MaintenanceMonitor] Backend restored, hiding maintenance modal');
					}
				},

				startMonitoring() {
					this.intervalId = setInterval(() => this.checkBackend(), 5000);
				},

				startCountdown() {
					this.countdown = 5;
					this.countdownId = setInterval(() => {
						this.countdown--;
						if (this.countdown <= 0) {
							this.countdown = 5;
						}
					}, 1000);
				},

				stopCountdown() {
					if (this.countdownId) {
						clearInterval(this.countdownId);
						this.countdownId = null;
					}
				},

				destroy() {
					if (this.intervalId) {
						clearInterval(this.intervalId);
					}
					this.stopCountdown();
				}
			};
		}
	</script>
}
```

**Step 2: Verify the file was created**

Run: `ls -la internal/webui/templates/components/MaintenanceModal.templ`
Expected: File exists with correct content

**Step 3: Generate templ code**

Run: `templ generate`
Expected: `(✓) Complete` with no errors

---

## Task 2: Import MaintenanceModal in Base Layout

**Files:**
- Modify: `internal/webui/templates/layouts/Base.templ:1-5`

**Step 1: Add import for components package**

At the top of `Base.templ`, add the import:

```go
package layouts

import "notificator/internal/webui/templates/components"

templ Base(title string, content templ.Component) {
```

**Step 2: Verify import added**

Run: `head -5 internal/webui/templates/layouts/Base.templ`
Expected: Shows `import "notificator/internal/webui/templates/components"`

---

## Task 3: Include MaintenanceModal in Base Layout Body

**Files:**
- Modify: `internal/webui/templates/layouts/Base.templ:21` (after `<body>` tag)

**Step 1: Add MaintenanceModal component after body tag**

Find the line:
```html
<body class="h-full bg-gray-50 dark:bg-dark-bg-primary">
```

Add immediately after it:
```html
		@components.MaintenanceModal()
```

The result should be:
```html
	<body class="h-full bg-gray-50 dark:bg-dark-bg-primary">
		@components.MaintenanceModal()
		<!-- Impersonation Banner (hidden by default, shown via JS when impersonating) -->
```

**Step 2: Verify the change**

Run: `grep -A1 '<body class="h-full' internal/webui/templates/layouts/Base.templ`
Expected: Shows `@components.MaintenanceModal()` after body tag

**Step 3: Generate templ code**

Run: `templ generate`
Expected: `(✓) Complete` with no errors

---

## Task 4: Add NOTIFICATOR_SESSION_SECRET to docker-compose.yml

**Files:**
- Modify: `docker-compose.yml:113-130` (webui service environment section)

**Step 1: Add session secret environment variable**

In the `webui` service `environment` section, add after line 130 (`NOTIFICATOR_SENTRY_GLOBAL_TOKEN`):

```yaml
      # Session persistence (generate with: openssl rand -hex 32)
      - NOTIFICATOR_SESSION_SECRET=${NOTIFICATOR_SESSION_SECRET:-}
```

**Step 2: Verify the change**

Run: `grep -A2 'NOTIFICATOR_SESSION_SECRET' docker-compose.yml`
Expected: Shows the new environment variable

---

## Task 5: Create .env.example with SESSION_SECRET

**Files:**
- Create: `.env.example` (if not exists, or modify if exists)

**Step 1: Check if .env.example exists**

Run: `ls -la .env.example 2>/dev/null || echo "File does not exist"`

**Step 2: Add session secret example**

If file exists, append to it. If not, create it with:

```bash
# Notificator Environment Variables

# Session secret for cookie encryption (REQUIRED for session persistence across restarts)
# Generate with: openssl rand -hex 32
NOTIFICATOR_SESSION_SECRET=your-64-character-hex-secret-here
```

**Step 3: Verify the file**

Run: `grep NOTIFICATOR_SESSION_SECRET .env.example`
Expected: Shows the session secret line

---

## Task 6: Build and Test Locally

**Step 1: Generate all templ files**

Run: `templ generate`
Expected: `(✓) Complete` with no errors

**Step 2: Build Docker images**

Run: `make test`
Expected: All images build successfully, services start

**Step 3: Verify session secret warning (without .env)**

Run: `docker logs notificator-webui 2>&1 | grep -i session`
Expected: Shows warning about no `NOTIFICATOR_SESSION_SECRET` configured

---

## Task 7: Manual Testing - Maintenance Modal

**Step 1: Open the application**

Open browser to: `http://localhost:8081`

**Step 2: Stop the backend to trigger modal**

Run: `docker stop notificator-backend`
Expected: Within 5 seconds, maintenance modal appears with blur effect

**Step 3: Verify modal content**

Expected modal shows:
- Gear icon
- "Maintenance in progress" title
- "Please wait while we restore the service..." message
- Spinning loader
- "Reconnecting in Xs..." countdown

**Step 4: Restart backend to verify auto-recovery**

Run: `docker start notificator-backend`
Expected: Modal disappears automatically when backend is healthy (within 5-10 seconds)

---

## Task 8: Manual Testing - Session Persistence

**Step 1: Generate a session secret**

Run: `openssl rand -hex 32`
Copy the output (64-character hex string)

**Step 2: Create .env file with secret**

Create `.env` file:
```bash
NOTIFICATOR_SESSION_SECRET=<paste-your-64-char-secret>
```

**Step 3: Restart services with secret**

Run: `docker-compose down && docker-compose up -d`

**Step 4: Verify session secret is being used**

Run: `docker logs notificator-webui 2>&1 | grep -i session`
Expected: `✅ Using configured session secret from NOTIFICATOR_SESSION_SECRET`

**Step 5: Test session persistence**

1. Login to the application
2. Note the page you're on (e.g., `/statistics`)
3. Run: `docker restart notificator-webui`
4. Refresh the browser
5. Expected: Still logged in, on the same page

---

## Summary

| Task | Description | Files |
|------|-------------|-------|
| 1 | Create MaintenanceModal component | `components/MaintenanceModal.templ` |
| 2 | Add import to Base layout | `layouts/Base.templ` |
| 3 | Include modal in Base body | `layouts/Base.templ` |
| 4 | Add env var to docker-compose | `docker-compose.yml` |
| 5 | Create .env.example | `.env.example` |
| 6 | Build and verify | - |
| 7 | Test maintenance modal | - |
| 8 | Test session persistence | `.env` |

**Total estimated tasks:** 8 tasks with ~20 steps
