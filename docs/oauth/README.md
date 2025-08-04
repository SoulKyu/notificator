# OAuth Authentication for Notificator WebUI

## Overview
OAuth authentication allows users to login using their existing accounts from:
- GitHub (with organization/team membership)
- Google Workspace (with group membership) 
- Microsoft Azure AD (with group membership)
- Okta (with group membership)
- Generic OIDC providers

## Features
- **Single Sign-On (SSO)** - Users login with existing accounts
- **Group/Organization Sync** - Automatically sync user's group memberships
- **Role-Based Access Control** - Map OAuth groups to application roles
- **Seamless Integration** - Works alongside existing username/password authentication
- **Multi-Provider Support** - Support multiple OAuth providers simultaneously

## Quick Start
1. Choose your OAuth provider(s)
2. Follow the provider-specific setup guide in `docs/oauth/providers/`
3. Configure your application
4. Update your configuration file
5. Restart the application

## Supported Providers

| Provider | User Info | Groups | Setup Guide |
|----------|-----------|---------|-------------|
| GitHub | ✅ | ✅ Organizations/Teams | [github-setup.md](providers/github-setup.md) |
| Google Workspace | ✅ | ✅ Groups | [google-setup.md](providers/google-setup.md) |
| Microsoft Azure AD | ✅ | ✅ Groups | [microsoft-setup.md](providers/microsoft-setup.md) |
| Okta | ✅ | ✅ Groups | [okta-setup.md](providers/okta-setup.md) |
| Generic OIDC | ✅ | ✅ Claims | [generic-oidc-setup.md](providers/generic-oidc-setup.md) |

## Configuration Overview

Basic OAuth configuration in your `config.json`:

```json
{
  "oauth": {
    "enabled": true,
    "redirect_url": "https://your-domain.com/api/v1/oauth",
    "session_key": "your-secure-session-key",
    "group_sync": {
      "enabled": true,
      "sync_on_login": true,
      "cache_timeout": "1h",
      "default_role": "viewer"
    },
    "providers": {
      "github": {
        "client_id": "your_github_client_id",
        "client_secret": "your_github_client_secret",
        "scopes": ["user:email", "read:org"]
      }
    }
  }
}
```

## Environment Variables

You can also configure OAuth via environment variables:

```bash
OAUTH_ENABLED=true
OAUTH_REDIRECT_URL=https://your-domain.com/api/v1/oauth
OAUTH_GITHUB_CLIENT_ID=your_github_client_id
OAUTH_GITHUB_CLIENT_SECRET=your_github_client_secret
OAUTH_GOOGLE_CLIENT_ID=your_google_client_id
OAUTH_GOOGLE_CLIENT_SECRET=your_google_client_secret
```

## Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   OAuth Provider │    │  Notificator     │    │  User Browser   │
│   (GitHub, etc.) │    │  Backend/WebUI   │    │                 │
└─────────────────┘    └──────────────────┘    └─────────────────┘
         │                       │                       │
         │              1. Login Request                 │
         │◄──────────────────────────────────────────────│
         │                       │                       │
         │              2. Auth Code                     │
         │───────────────────────────────────────────────►│
         │                       │                       │
         │         3. Exchange Code for Token            │
         │◄──────────────────────┤                       │
         │                       │                       │
         │         4. Get User Info + Groups             │
         │◄──────────────────────┤                       │
         │                       │                       │
         │                  5. Store User                │
         │                  6. Create Session            │
         │                  7. Redirect to App           │
         │                       ├───────────────────────►│
```

## Security Features

- **State Parameter Validation** - Prevents CSRF attacks
- **Secure Token Storage** - OAuth tokens encrypted and stored securely
- **Scope Minimization** - Only requests necessary permissions
- **Session Management** - Integrates with existing session system
- **Group Validation** - Server-side validation of group memberships

## Development Setup

For development with HTTPS (required by most OAuth providers), see our detailed guide:

📖 **[Development Testing with ngrok](dev-testing-ngrok.md)** - Complete guide for testing OAuth in development

Quick start:
```bash
# Using ngrok for HTTPS tunnel
ngrok http 8080

# Update your OAuth app callback URLs to use the ngrok URL
# Example: https://abc123.ngrok.io/oauth/github/callback
```

## Need Help?

- **Setup Issues**: Check [troubleshooting/common-issues.md](troubleshooting/common-issues.md)
- **Configuration**: See [examples/config-examples.json](examples/config-examples.json)
- **Debugging**: Follow [troubleshooting/debugging.md](troubleshooting/debugging.md)

## Next Steps

1. Choose your OAuth provider from the table above
2. Follow the detailed setup guide for your provider
3. Test your configuration
4. Configure group mappings for role-based access (optional)
5. Deploy and enjoy seamless OAuth authentication!