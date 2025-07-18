# Notificator

Ever wanted to hear when your servers are on fire? This is the tool for you!

![alt text](img/preview.gif "Preview")

## What's this?

Notificator is a desktop app that connects to your Prometheus Alertmanager and makes sure you never miss an important alert. I built it because I was tired of constantly checking the Alertmanager web UI, and I wanted something that would literally scream at me when things go wrong.

Here's what it does:
- Shows all your alerts in a nice GUI (much better than the default one)
- Plays sounds when critical stuff happens
- Sends desktop notifications so you notice even if the app is minimized
- Updates in real-time (no more F5 spam!)
- Lets you search and filter alerts easily
- You can create silences directly from the app

## Let's get started

First, build the thing:
```bash
go build -o notificator
```

Then run it:
```bash
./notificator
```

That's it! It will try to connect to localhost:9093 by default. If your Alertmanager is somewhere else, check the configuration section below.

## Configuration

The app creates a config file at `~/.config/notificator/config.json` on first run. Here's what you can tweak:

### Basic example

```json
{
  "alertmanagers": [
    {
      "name": "production",
      "url": "http://localhost:9093"
    },
    {
      "name": "staging",
      "url": "http://staging:9093"
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

**Multiple Alertmanagers** - Yeah, you can connect to multiple ones at the same time! Just add them to the list. Each one gets its own name so you know where alerts come from.

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

### Quick tip for authentication

If your Alertmanager needs auth headers, you can set them like this:

```bash
export METRICS_PROVIDER_HEADERS="X-API-Key=your-key"
# or for multiple headers
export METRICS_PROVIDER_HEADERS="X-API-Key=key1,Authorization=Bearer token123"
```

Much easier than putting secrets in config files!

## What can it do?

### The basics
- Watches multiple Alertmanagers at once (yeah, I have too many too)
- Makes noise when things break (configurable, don't worry)
- Shows desktop notifications that actually work
- Search and filter alerts without going crazy
- Create silences when you're working on something
- Light/dark theme (because we all work at night sometimes)
- Tracks resolved alerts so you know when things got fixed

### Team features (needs backend)
So you want to use this with your team? I got you covered:

- **Everyone sees the same thing** - Real-time updates for all connected users
- **"I got this"** - Acknowledge alerts so others know you're on it
- **Leave notes** - Add comments to alerts (super useful during incidents)
- **Who did what** - Full history of actions on each alert
- **Secure access** - Login system so not everyone can acknowledge your alerts

📚 **Want to learn more?** Check out the [detailed collaboration guide](docs/notificator-collaboration.md) with screenshots and best practices!

## Running modes

### Just for me (default)
```bash
./notificator
```
Simple. No setup. Just works.

### For the whole team
First, someone needs to run the backend:

```bash
# Start the backend server
./notificator --backend
```

Then everyone connects to it by setting this in their config:
```json
{
  "backend": {
    "enabled": true,
    "grpc_client": "where-your-backend-lives:50051"
  }
}
```

### What you get with the backend

When connected to a backend, you get:
- Login screen (no more anonymous alerts!)
- Ack button on each alert
- Comments section
- Live updates when your colleague acks something
- Filters for "show only unacked" and stuff like that

The backend runs on port 50051 (gRPC) and 8080 (health checks). It works with SQLite for testing or PostgreSQL for real deployments.

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
Just run it. Seriously. It works out of the box with fake data if you want to test:

```bash
# Start fake alertmanager for testing
cd alertmanager/fake
python fake_alertmanager.py

# In another terminal
./notificator
```

### Production deployment
Check out the `helm/` folder - there's a Helm chart if you want to deploy the backend on Kubernetes. Or just run it on a VM, it's not picky.

### Docker? 
Not yet, but feel free to make a Dockerfile. I just use `go build` and copy the binary around.

## Troubleshooting

**"Can't connect to Alertmanager"**
- Is it running? Check with `curl http://localhost:9093/api/v1/alerts`
- Using authentication? Set those env vars (see above)
- Behind a proxy? The app respects HTTP_PROXY and HTTPS_PROXY

**"No sound on Linux"**
- Install `pulseaudio-utils` or `alsa-utils`
- The app tries different sound systems until one works
- Still nothing? Check if other apps can play sound

**"Backend connection failed"**
- Check if the backend is actually running: `telnet backend-host 50051`
- Firewall issues? Port 50051 needs to be open
- Wrong address in config? Double-check the grpc_client setting

**"I see nothing!"**
- Check the logs (they're pretty verbose)
- Try running with just `./notificator` first, then add complexity
- Make sure your Alertmanager actually has alerts

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
- `internal/gui/` - All the UI stuff (Fyne framework)
- `internal/alertmanager/` - Talks to Alertmanager API
- `internal/backend/` - The team collaboration magic
- `internal/notifier/` - Makes noise and sends notifications

Just fork it, make your changes, and send a PR. I usually merge stuff pretty quick.

## Why I built this

I was on-call and kept missing alerts because:
1. The Alertmanager UI is... functional (but not great)
2. No sound notifications = missed alerts at 3 AM
3. Switching between multiple Alertmanagers was painful
4. No way to see who was handling what during incidents

So I built this over a weekend. Then my team started using it. Then we added the backend for collaboration. Now we actually respond to alerts on time!

The best part? When an alert fires, everyone knows about it. And when someone acks it, everyone else can relax. No more "are you handling this?" messages.

Hope it helps you too. If not, at least it's fun to watch the alerts pop up with sounds 🔔

---

*PS: Yes, I know the code could be cleaner. Yes, I know there should be more tests. But it works, and that's what matters when you're on-call!*
