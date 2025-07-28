# OAuth Configuration Guide

## Overview

Notificator supports OAuth authentication alongside classic username/password authentication. When OAuth is enabled, you can configure whether to allow both authentication methods or enforce OAuth-only mode.

## Environment Variables

### Basic OAuth Configuration

```bash
# Enable OAuth authentication
OAUTH_ENABLED=true

# Disable classic username/password authentication (OAuth-only mode)
# Default: true (when OAuth is enabled, classic auth is disabled by default)
OAUTH_DISABLE_CLASSIC_AUTH=true

# OAuth redirect URL (must match your OAuth app configuration)
OAUTH_REDIRECT_URL=https://your-domain.com/api/v1/oauth

# Session encryption key (MUST be changed in production)
OAUTH_SESSION_KEY=your-secure-session-key-change-me-in-production
```

### GitHub Provider Example

```bash
# GitHub OAuth configuration
OAUTH_GITHUB_CLIENT_ID=your_github_client_id
OAUTH_GITHUB_CLIENT_SECRET=your_github_client_secret
OAUTH_GITHUB_SCOPES=user:email,read:org,read:user
```

## Authentication Modes

### 1. Classic Authentication Only (Default)
```bash
# OAuth not configured or disabled
OAUTH_ENABLED=false
```
- Users can only login with username/password
- Registration is available

### 2. Mixed Authentication Mode
```bash
OAUTH_ENABLED=true
OAUTH_DISABLE_CLASSIC_AUTH=false
```
- Users can login with either OAuth or username/password
- Registration is still available
- OAuth users are created without passwords
- Classic users can't use OAuth without linking (not yet implemented)

### 3. OAuth-Only Mode (Recommended for Production)
```bash
OAUTH_ENABLED=true
OAUTH_DISABLE_CLASSIC_AUTH=true  # This is the default when OAuth is enabled
```
- Only OAuth login is allowed
- Registration is disabled
- All users must authenticate via OAuth providers
- Better security and centralized authentication

## Security Considerations

1. **Session Duration**: Both OAuth and classic authentication use the same session duration (7 days by default)

2. **OAuth-Only Users**: Users created via OAuth cannot login with username/password (no password is set)

3. **Classic Auth Users**: Users with passwords cannot use OAuth login unless they have OAuth details linked

## Docker Compose Example

```yaml
services:
  notificator-backend:
    environment:
      # OAuth Configuration
      - OAUTH_ENABLED=true
      - OAUTH_DISABLE_CLASSIC_AUTH=true
      - OAUTH_REDIRECT_URL=https://notificator.example.com/api/v1/oauth
      - OAUTH_SESSION_KEY=${OAUTH_SESSION_KEY}
      
      # GitHub Provider
      - OAUTH_GITHUB_CLIENT_ID=${GITHUB_CLIENT_ID}
      - OAUTH_GITHUB_CLIENT_SECRET=${GITHUB_CLIENT_SECRET}
      - OAUTH_GITHUB_SCOPES=user:email,read:org,read:user
```

## Troubleshooting

### "Registration is disabled" Error
This occurs when OAuth is enabled with `OAUTH_ENABLED=true`. Users must use OAuth to sign in.

### "This account uses [Provider] authentication" Error
This occurs when an OAuth user tries to login with username/password. They must use their OAuth provider.

### OAuth Not Working
1. Check that OAuth is enabled in the backend (not webui)
2. Verify all required environment variables are set
3. Ensure redirect URL matches your OAuth app configuration
4. Check backend logs for OAuth initialization messages