# OAuth Development Testing with ngrok

This guide explains how to test OAuth functionality in development using ngrok to create secure HTTPS tunnels that OAuth providers require.

## Why ngrok is needed

OAuth providers like GitHub, Google, and Microsoft require HTTPS callback URLs for security. Since your development server typically runs on `http://localhost`, you need ngrok to:

1. **Create HTTPS tunnels** - OAuth providers reject HTTP callback URLs
2. **Provide stable URLs** - OAuth apps need consistent redirect URIs
3. **Enable external access** - Some providers validate that URLs are publicly accessible

## Prerequisites

1. **Install ngrok**: Download from [ngrok.com](https://ngrok.com/) or use package managers:
   ```bash
   # macOS
   brew install ngrok/ngrok/ngrok
   
   # Linux (snap)
   sudo snap install ngrok
   
   # Or download binary directly
   ```

2. **Sign up for ngrok account** (optional but recommended for stable domains)

## Development Setup

### 1. Start your Notificator development server

```bash
# Start the webui in development mode
go run main.go webui

# Or if using make
make dev-webui
```

Your server should be running on `http://localhost:8080` (or your configured port).

### 2. Start ngrok tunnel

```bash
# Basic tunnel (random subdomain each time)
ngrok http 8080

# With custom subdomain (requires ngrok account)
ngrok http 8080 --subdomain=my-notificator-dev

# With custom domain (requires ngrok paid plan)
ngrok http 8080 --hostname=notificator-dev.example.com
```

You'll see output like:
```
ngrok by @inconshreveable

Session Status                online
Account                       your-email@example.com
Version                       3.x.x
Region                        United States (us)
Forwarding                    https://abc123.ngrok.io -> http://localhost:8080
Forwarding                    http://abc123.ngrok.io -> http://localhost:8080

Connections                   ttl     opn     rt1     rt5     p50     p90
                              0       0       0.00    0.00    0.00    0.00
```

**Important**: Use the HTTPS URL (`https://abc123.ngrok.io`) for OAuth configuration.

### 3. Configure OAuth providers

Update your OAuth applications with the ngrok HTTPS URL:

#### GitHub OAuth App
- **Homepage URL**: `https://abc123.ngrok.io`
- **Authorization callback URL**: `https://abc123.ngrok.io/oauth/github/callback`

#### Google OAuth App  
- **Authorized JavaScript origins**: `https://abc123.ngrok.io`
- **Authorized redirect URIs**: `https://abc123.ngrok.io/oauth/google/callback`

#### Microsoft OAuth App
- **Redirect URIs**: `https://abc123.ngrok.io/oauth/microsoft/callback`

### 4. Update your environment configuration

Set your environment variables to use the ngrok URL:

```bash
# .env or environment
OAUTH_REDIRECT_URL=https://abc123.ngrok.io

# GitHub OAuth
OAUTH_GITHUB_ENABLED=true
OAUTH_GITHUB_CLIENT_ID=your_github_client_id
OAUTH_GITHUB_CLIENT_SECRET=your_github_client_secret

# Google OAuth  
OAUTH_GOOGLE_ENABLED=true
OAUTH_GOOGLE_CLIENT_ID=your_google_client_id.googleusercontent.com
OAUTH_GOOGLE_CLIENT_SECRET=your_google_client_secret

# Microsoft OAuth
OAUTH_MICROSOFT_ENABLED=true
OAUTH_MICROSOFT_CLIENT_ID=your_microsoft_client_id
OAUTH_MICROSOFT_CLIENT_SECRET=your_microsoft_client_secret
```

### 5. Test OAuth flows

1. **Access your app** via the ngrok HTTPS URL: `https://abc123.ngrok.io`
2. **Navigate to login page** and you should see OAuth provider buttons
3. **Click OAuth provider** to test the authentication flow
4. **Verify redirect** happens correctly after authentication

## Development Tips

### Stable Subdomains

For consistent development, consider:

```bash
# Reserve a stable subdomain (requires ngrok account)
ngrok http 8080 --subdomain=notificator-dev

# This gives you: https://notificator-dev.ngrok.io
```

This way your OAuth redirect URLs stay consistent between development sessions.

### Configuration Management

Create a development-specific configuration:

```bash
# config/dev.yaml
oauth:
  enabled: true
  redirect_url: "https://your-stable-subdomain.ngrok.io"
  disable_classic_auth: false  # Keep classic auth for easier dev testing
  providers:
    github:
      enabled: true
      client_id: "${OAUTH_GITHUB_CLIENT_ID}"
      client_secret: "${OAUTH_GITHUB_CLIENT_SECRET}"
      scopes: ["user:email", "read:org"]
    google:
      enabled: true
      client_id: "${OAUTH_GOOGLE_CLIENT_ID}"
      client_secret: "${OAUTH_GOOGLE_CLIENT_SECRET}"
      scopes: ["openid", "profile", "email"]
```

### Local Testing Script

Create a helper script for development:

```bash
#!/bin/bash
# scripts/dev-oauth.sh

echo "Starting OAuth development environment..."

# Check if ngrok is running
if ! pgrep -f "ngrok" > /dev/null; then
    echo "Starting ngrok tunnel..."
    ngrok http 8080 --subdomain=notificator-dev &
    sleep 3
fi

# Get the ngrok URL
NGROK_URL=$(curl -s http://localhost:4040/api/tunnels | jq -r '.tunnels[0].public_url' | sed 's/http:/https:/')
echo "ngrok URL: $NGROK_URL"

# Export for the application
export OAUTH_REDIRECT_URL=$NGROK_URL

# Start the application
echo "Starting Notificator WebUI..."
go run main.go webui
```

## Troubleshooting

### Common Issues

1. **"redirect_uri_mismatch" error**
   - Verify the OAuth app redirect URI exactly matches your ngrok URL
   - Ensure you're using HTTPS, not HTTP
   - Check for trailing slashes in URLs

2. **"invalid_client" error**
   - Verify client ID and secret are correct
   - Check that the OAuth app is properly configured in the provider

3. **ngrok tunnel expired**
   - Free ngrok tunnels expire after 8 hours
   - Restart ngrok and update OAuth app configurations
   - Consider ngrok paid plan for longer sessions

4. **CORS issues**
   - Ensure your CORS configuration allows the ngrok domain
   - Check that requests are being made to HTTPS URLs

### Debugging OAuth Flow

Enable debug logging to trace the OAuth process:

```bash
# Enable debug logging
export LOG_LEVEL=debug
export OAUTH_DEBUG=true

# Or in code, add debug prints in oauth_handlers.go
```

### Testing Multiple Providers

Test each provider separately:

```bash
# Test GitHub only
export OAUTH_GITHUB_ENABLED=true
export OAUTH_GOOGLE_ENABLED=false
export OAUTH_MICROSOFT_ENABLED=false

# Then test Google only, etc.
```

## Security Notes

⚠️ **Important for Development**:

1. **Never commit** ngrok URLs to version control
2. **Use different OAuth apps** for development vs production  
3. **Rotate secrets** if accidentally exposed
4. **Monitor ngrok dashboard** at http://localhost:4040 for request inspection

## Production Deployment

When moving to production:

1. **Replace ngrok URLs** with your actual domain
2. **Update OAuth app configurations** with production URLs
3. **Use environment-specific** client IDs and secrets
4. **Enable HTTPS** on your production server
5. **Set secure session cookies** and CSRF protection

This setup allows you to fully test OAuth flows in development with HTTPS requirements satisfied through ngrok tunneling.