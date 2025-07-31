# Notificator

Ever wanted to hear when your servers are on fire? This is the tool for you!

🚀 **Try it now at [playground.notificator.org](https://playground.notificator.org)** - no installation needed!

![alt text](img/preview.gif "Preview")

## What's this?

So here's the thing - I built Notificator as a desktop app because I was tired of constantly checking the Alertmanager web UI. But then people on my team wanted to check alerts from their phones, tablets, and that one guy who only uses his iPad... So we built a WebUI! 

Now Notificator is primarily a web app that connects to ALL your Prometheus Alertmanagers in one place.

Here's what the WebUI does:
- **Connects to multiple Alertmanagers at once** - see prod, staging, and dev alerts together
- Shows all your alerts in a clean interface (much better than the default Alertmanager UI and Karma)
- Works on any device - phone, tablet, laptop, whatever (why not a home-assistant integration ?)
- Real-time updates for everyone (no more "did you see that alert?")
- Team collaboration built-in - see who's working on what
- Search, filter, acknowledge, comment - all the good stuff
- OAuth login support (because security matters)
- Each alert shows which Alertmanager it's from (no more guessing!)
- And yes, it still makes noise when things break!

Still prefer the desktop app? It's got:
- Local notifications that pop up on your screen
- Sound alerts (configurable, don't worry)
- System tray integration
- Works offline with cached data

## Let's get started

### Quickest start - Try the playground!
Just head to [playground.notificator.org](https://playground.notificator.org) and see it in action. We've got some fake alerts running so you can click around and see how it works.

You can register a new user if you wan't, or use the default one : `admin` : `admin`

### Running your own WebUI

The WebUI needs the backend running (that's what makes all the team features work). Here's the quickest way:

```bash
# Clone and build
git clone https://github.com/soulkyu/notificator
cd notificator
go build -o notificator

# Start the backend (required for WebUI)
./notificator backend

# In another terminal, start the WebUI
./notificator webui
```

Now open http://localhost:8081 and you're good to go! The WebUI will connect to your Alertmanager at localhost:9093 by default.

### Just want the desktop app?

Cool, that still works:
```bash
go build -o notificator
./notificator
```

That's it! The desktop app can run standalone (no backend needed) or connected to a backend for team features.

## Configuration

The app creates a config file at `~/.config/notificator/config.json` on first run. Here's what you can tweak:

### Basic example - Multiple Alertmanagers!

This is where it gets good. Got alerts in different places? Connect them all:

```json
{
  "alertmanagers": [
    {
      "name": "production-us-east",
      "url": "http://prod-1.alertmanager:9093"
    },
    {
      "name": "production-eu",
      "url": "http://prod-eu.alertmanager:9093"  
    },
    {
      "name": "staging",
      "url": "http://staging:9093"
    },
    {
      "name": "that-legacy-system",
      "url": "http://10.0.0.42:9093",
      "username": "admin",
      "password": "definitely-change-this"
    }
  ],
  "notifications": {
    "enabled": true,
    "sound_enabled": true,
    "critical_only": false  // set to true if you only want critical alerts
  }
}
```

### The important stuff

**Multiple Alertmanagers** - Yeah, you can connect to multiple ones at the same time. Just add them to the list. Each alert shows which one it's from, so you'll see things like "[production-us-east] Disk space low" or "[staging] API timeout". Pretty handy when you've got alerts spread across different environments or regions.

**Notifications settings** - This is where you control how annoying the app should be:
- `sound_enabled`: Want sounds? Keep this true
- `critical_only`: Only make noise for critical alerts
- `severity_rules`: Fine-tune which severities trigger notifications
- `cooldown_seconds`: How long before the same alert can notify again (default 5 minutes)

**Backend stuff** (if you want to share alerts with your team):
```json
"backend": {
  "enabled": true,
  "grpc_client": "your-backend-server:50051"
}
```

### Authentication tips

**For Alertmanager auth:**
If your Alertmanager needs auth headers, you can set them like this:

```bash
export METRICS_PROVIDER_HEADERS="X-API-Key=your-key"
# or for multiple headers
export METRICS_PROVIDER_HEADERS="X-API-Key=key1,Authorization=Bearer token123"
```

**For WebUI OAuth login:**
Want your team to login with Google/GitHub/whatever? Enable OAuth in the backend:

```bash
# Set these env vars before starting the backend
export OAUTH_ENABLED=true
export OAUTH_PROVIDER_GOOGLE_CLIENT_ID="your-client-id"
export OAUTH_PROVIDER_GOOGLE_CLIENT_SECRET="your-secret"
export OAUTH_REDIRECT_URL="http://localhost:8081/oauth/callback"

# Now start the backend
./notificator backend
```

Check out the [OAuth setup guide](docs/oauth/) for the full story. Or just use classic username/password - that works too!

## What can it do?

### The basics
- Watches multiple Alertmanagers at once (yeah, I have too many too)
- Makes noise when things break (configurable, don't worry)
- Shows desktop notifications that actually work
- Search and filter alerts without going crazy
- Create silences when you're working on something
- Light/dark theme (because we all work at night sometimes)
- Tracks resolved alerts so you know when things got fixed

### Why you need the backend

Here's the deal - the WebUI **requires** the backend. No backend = no WebUI. Why? Because the backend is what makes everything work:

- Stores all the alert history and team actions
- Handles user authentication (OAuth or classic login) 
- Syncs everything in real-time between users
- Manages acknowledgments and comments
- Basically, it's the brain of the operation

The desktop app can work without it (just shows alerts), but the WebUI is built for teams, so the backend is mandatory.

### Team features (powered by the backend)

When you've got the backend running, here's what you get:

- **Everyone sees the same thing** - Real-time updates for all connected users
- **"I got this"** - Acknowledge alerts so others know you're on it  
- **Leave notes** - Add comments to alerts (super useful during incidents)
- **Who did what** - Full history of actions on each alert
- **OAuth support** - Login with Google, GitHub, whatever you use
- **Secure access** - Not everyone can acknowledge your production alerts!

📚 **Want to learn more?** Check out the [detailed collaboration guide](docs/notificator-collaboration.md) with screenshots and best practices!

## Running modes

### WebUI mode (recommended)
This is how most people use Notificator now:

```bash
# Terminal 1: Start the backend (required!)
./notificator backend

# Terminal 2: Start the WebUI  
./notificator webui
```

Then open http://localhost:8081 in your browser. Done! Your whole team can now access it.

### Desktop app - standalone mode
Just want alerts on your laptop? No problem:

```bash
./notificator
```

This runs without a backend - just you and your alerts. Simple.

### Desktop app - team mode
Want desktop notifications AND team features? Connect the desktop app to a backend:

```bash
# First make sure a backend is running somewhere
# Then add this to your config:
```
```json
{
  "backend": {
    "enabled": true,
    "grpc_client": "your-backend-server:50051"
  }
}
```

### What each mode gives you

**WebUI (with backend):**
- Browser-based access from anywhere
- Full team collaboration
- OAuth/SSO login
- Comments and acknowledgments
- Real-time sync across all users
- Works on phones/tablets

**Desktop standalone:**
- Local alerts only
- System notifications
- No login required
- Works offline

**Desktop with backend:**
- Everything from standalone PLUS
- Team features (acks, comments)
- Login required
- Syncs with WebUI users

### Backend Configuration

```json
{
  "backend": {
    "enabled": true,
    "grpc_listen": ":50051",      // Port for gRPC server (backend mode)
    "grpc_client": "localhost:50051", // Address to connect to backend
    "http_listen": ":8080",        // Port for HTTP health/metrics
    "database": {
      "type": "sqlite",            // "sqlite" or "postgres"
      "sqlite_path": "./notificator.db",
      "host": "localhost",         // PostgreSQL host
      "port": 5432,                // PostgreSQL port
      "name": "notificator",       // PostgreSQL database name
      "user": "notificator",       // PostgreSQL user
      "password": "",              // PostgreSQL password
      "ssl_mode": "disable"        // PostgreSQL SSL mode
    }
  }
}
```

### Setting Up a Backend Server

1. **Install and run the backend**:
   ```bash
   # On your server
   ./notificator --backend            # Migrations run automatically on startup
   ```

2. **Configure clients to connect**:
   ```json
   {
     "backend": {
       "enabled": true,
       "grpc_client": "your-server:50051"
     }
   }
   ```

3. **Create user accounts** (via backend API or database):
   - Users can register/login through the desktop client
   - Admins can manage users directly in the database

### How it works (if you care)

```
Your laptop          Your colleague         That new guy
    │                     │                     │
    └─────────────────────┴─────────────────────┘
                          │
                    gRPC (50051)
                          │
                   Backend Server ← Does all the magic
                          │
                      Database ← Keeps everything
```

Everyone sees the same alerts, acks, and comments. It's like Slack but for alerts (and much simpler).

## Deployment

### Local development
Want to test everything locally? We've got you covered:

```bash
# Start fake alertmanager for testing
cd alertmanager/fake
python fake_alertmanager.py

# Start the backend
./notificator backend

# Start the WebUI
./notificator webui

# Now open http://localhost:8081
```

Or just use the playground at [playground.notificator.org](https://playground.notificator.org) - we keep it running with fake alerts for testing.

### Production deployment - The easy way

We've got Docker Compose for quick deployments:

```bash
# This starts everything: backend, webui, and even a fake alertmanager
docker-compose up -d

# WebUI will be at http://localhost:8081
# Backend at localhost:50051
```

### Production deployment - For real

Check out the `helm/` folder - there's a Helm chart that deploys the whole stack:

```bash
# Deploy everything on Kubernetes
helm install notificator ./helm/notificator

# This gives you:
# - Backend with PostgreSQL
# - WebUI with ingress
# - Proper health checks
# - OAuth ready to configure
```

Or if you're old school, just run the binaries on a VM:

```bash
# On your server
./notificator backend &
./notificator webui &

# Use systemd, supervisor, whatever you like
```

## Troubleshooting

**"WebUI won't start"**
- Is the backend running? The WebUI needs it! Check with `curl http://localhost:8080/health`
- Wrong ports? Backend uses 50051 (gRPC) and 8080 (HTTP), WebUI uses 8081
- Already running? `lsof -i:8081` to check if the port is taken

**"Can't login to WebUI"**  
- Using OAuth? Make sure OAuth is configured in the backend
- Classic auth? Did you register an account first?
- Check backend logs - they usually tell you what's wrong

**"Can't connect to Alertmanager"**
- Is it running? Check with `curl http://localhost:9093/api/v1/alerts`
- Using authentication? Set those env vars (see above)
- Behind a proxy? The app respects HTTP_PROXY and HTTPS_PROXY
- OAuth proxy? We support that too - check the config section

**"No sound on Linux"** (desktop app)
- Install `pulseaudio-utils` or `alsa-utils`
- The app tries different sound systems until one works
- Still nothing? Check if other apps can play sound

**"Backend connection failed"**
- Check if the backend is actually running: `telnet backend-host 50051`
- Firewall issues? Ports 50051 and 8080 need to be open
- Database issues? Check if PostgreSQL/SQLite is accessible

**"I see nothing!"**
- Check the logs (they're pretty verbose)
- Try the playground first: [playground.notificator.org](https://playground.notificator.org)
- Make sure your Alertmanager actually has alerts
- WebUI: Check browser console for errors (F12)

## GUI Scaling

The app uses the Fyne framework which supports system-aware scaling. Here's how to adjust the size:

### Option 1: Environment Variable (Recommended)
```bash
# Make everything 50% larger
FYNE_SCALE=1.5 ./notificator

# Make everything 20% smaller  
FYNE_SCALE=0.8 ./notificator

# Reset to default (1.0)
FYNE_SCALE=1.0 ./notificator
```

### Option 2: Fyne Settings App
Install and run the fyne settings app:
```bash
# Install fyne settings
go install fyne.io/fyne/v2/cmd/fyne_settings@latest

# Run it
fyne_settings
```

Then adjust the "Scale" slider to your preference. This affects all Fyne applications system-wide.

### Scale Values
- `0.5` = 50% (very small)
- `0.8` = 80% (smaller)
- `1.0` = 100% (default)
- `1.2` = 120% (larger)
- `1.5` = 150% (much larger)
- `2.0` = 200% (very large)

The app will remember your FYNE_SCALE setting, so you can add it to your shell profile or systemd service.

### Checking Current Scale
To see what scale is currently being used:
```bash
# Check current scale in fyne_settings
fyne_settings

# Or check by running notificator (scale is shown in logs)
./notificator
```

**Note**: If you've never used fyne_settings before, the default scale should be 1.0. If it's not, you can reset it by running `fyne_settings` and moving the Scale slider to the middle position (1.0).

## Contributing

Found a bug? Got an idea? PRs are welcome! The code is pretty straightforward:
- `internal/webui/` - The web interface (Go + Templ + HTMX + Alpine.js)
- `internal/gui/` - Desktop app UI (Fyne framework)
- `internal/alertmanager/` - Talks to Alertmanager API
- `internal/backend/` - The team collaboration magic (gRPC server)
- `internal/notifier/` - Makes noise and sends notifications

WebUI stack is modern but simple:
- Backend: Go with Gin
- Templates: Templ (type-safe HTML templates)
- Frontend: HTMX for interactions, Alpine.js for state, Tailwind for styling
- No npm, no webpack, no 10,000 dependencies!

Just fork it, make your changes, and send a PR. I usually merge stuff pretty quick.

## Why I built this

I was on-call and kept missing alerts because:
1. The Alertmanager UI is... functional (but not great)
2. No sound notifications = missed alerts at 3 AM
3. Switching between multiple Alertmanagers was painful
4. No way to see who was handling what during incidents

So I built this over a weekend. Then my team started using it. Then we added the backend for collaboration. Then people wanted to check alerts from their phones, so we built the WebUI. Now everyone can see alerts from anywhere - at home, in a meeting, or yeah, even from the bathroom (we've all been there).

The best part? When an alert fires, everyone knows about it instantly - whether they're using the WebUI on their phone or the desktop app on their laptop. And when someone acks it, everyone else can relax. No more "are you handling this?" messages in Mattermost or Slacks as you prefer.

Hope it helps you too. If not, at least it's fun to watch the alerts pop up with sounds 🔔

---

*PS: Yes, I know the code could be cleaner. Yes, I know there should be more tests. But it works, and that's what matters when you're on-call!*
