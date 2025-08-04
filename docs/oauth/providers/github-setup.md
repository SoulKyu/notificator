# GitHub OAuth Setup Guide

This guide will help you set up GitHub OAuth authentication for Notificator WebUI, including organization and team membership sync.

## Prerequisites

- GitHub account with admin access to your organization (for group sync)
- Notificator application deployed with HTTPS access
- Access to modify your Notificator configuration

## Step 1: Create GitHub OAuth App

1. **Go to GitHub Settings**
   - Navigate to [GitHub Settings](https://github.com/settings/profile)
   - Click on **Developer settings** (bottom left)
   - Click on **OAuth Apps**

2. **Create New OAuth App**
   - Click **"New OAuth App"**
   - Fill in the application details:
     - **Application name**: `Notificator WebUI`
     - **Homepage URL**: `https://your-domain.com`
     - **Application description**: `Alert management dashboard OAuth integration`
     - **Authorization callback URL**: `https://your-domain.com/api/v1/oauth/github/callback`

3. **Save Application**
   - Click **"Register application"**
   - Copy your **Client ID** and **Client Secret** (you can generate a new secret if needed)

## Step 2: Configure Organization Access (For Groups)

### For Organization Members
1. **Organization Settings**
   - Go to your GitHub Organization → **Settings**
   - Navigate to **Third-party access** (under "Access" section)
   - Click **"Third-party application access policy"**

2. **Grant Application Access**
   - Find your Notificator OAuth app in the list
   - Click **"Grant"** or **"Request"** access
   - Approve the application

### For Organization Admins
1. **Enable OAuth App Access**
   - In Organization Settings → **Third-party access**
   - Under **"OAuth App access restrictions"**
   - Either disable restrictions or explicitly allow your OAuth app

2. **Member Data Access**
   - Ensure **"Organization member data"** access is enabled
   - This allows the app to read organization membership

## Step 3: Required OAuth Scopes

Your application will request these scopes:

| Scope | Purpose | Required |
|-------|---------|----------|
| `user:email` | Access user's email address | ✅ Yes |
| `read:org` | Read organization membership | ✅ Yes (for groups) |
| `read:user` | Read user profile information | ✅ Yes |

## Step 4: Configuration

Add the following to your `config.json`:

```json
{
  "oauth": {
    "enabled": true,
    "redirect_url": "https://your-domain.com/api/v1/oauth",
    "session_key": "your-secure-session-key-change-me",
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
        "scopes": ["user:email", "read:org", "read:user"],
        "auth_url": "https://github.com/login/oauth/authorize",
        "token_url": "https://github.com/login/oauth/access_token",
        "user_info_url": "https://api.github.com/user",
        "groups_url": "https://api.github.com/user/orgs",
        "group_scopes": ["read:org"],
        "group_mapping": {
          "admin": "administrator",
          "maintainer": "editor",
          "member": "viewer"
        }
      }
    }
  }
}
```

### Environment Variables Alternative

```bash
# OAuth Configuration
OAUTH_ENABLED=true
OAUTH_REDIRECT_URL=https://your-domain.com/api/v1/oauth

# GitHub OAuth
OAUTH_GITHUB_CLIENT_ID=your_github_client_id
OAUTH_GITHUB_CLIENT_SECRET=your_github_client_secret
OAUTH_GITHUB_SCOPES=user:email,read:org,read:user

# Group Sync
OAUTH_GROUP_SYNC_ENABLED=true
OAUTH_GROUP_SYNC_ON_LOGIN=true
```

## Step 5: Group Mapping Configuration

GitHub provides organization roles that you can map to your application roles:

### GitHub Organization Roles
- **Owner** - Full organization access
- **Member** - Organization member
- **Admin** - Repository admin rights
- **Maintainer** - Repository maintainer rights  
- **Developer** - Repository developer rights

### Example Mapping
```json
{
  "group_mapping": {
    "owner": "administrator",
    "admin": "administrator", 
    "maintainer": "editor",
    "member": "viewer",
    "developer": "viewer"
  }
}
```

## Step 6: Testing Your Setup

1. **Start your application** with OAuth configuration
2. **Navigate to login page**: `https://your-domain.com/login`
3. **Click "Login with GitHub"**
4. **Authorize the application** when redirected to GitHub
5. **Verify successful login** and check that groups are synced

### Debug Information
Check your application logs for:
```
[INFO] OAuth: GitHub user authenticated: username
[INFO] OAuth: Synced 2 organizations for user: username
[INFO] OAuth: User role assigned: editor (from maintainer)
```

## Step 7: Development Setup

For local development, GitHub OAuth requires HTTPS. Use ngrok:

```bash
# Install ngrok if you haven't already
brew install ngrok  # macOS
# or download from https://ngrok.com/

# Start your local application
./notificator webui --port 3000

# In another terminal, create HTTPS tunnel
ngrok http 3000

# Update your GitHub OAuth app callback URL to:
# https://abc123.ngrok.io/api/v1/oauth/github/callback
```

## Troubleshooting

### Common Issues

#### "Application not authorized by organization"
**Problem**: User gets error during OAuth flow
**Solution**: 
- Admin must approve OAuth app in organization settings
- Check third-party access policy
- Ensure OAuth app restrictions allow your application

#### "redirect_uri_mismatch"
**Problem**: Callback URL doesn't match registered URL
**Solution**:
- Verify callback URL exactly matches: `https://your-domain.com/api/v1/oauth/github/callback`
- Check for trailing slashes consistency
- Update GitHub OAuth app settings if URL changed

#### Groups not syncing
**Problem**: User organizations not appearing in application
**Solutions**:
- User must be public member of organization, or
- OAuth app must be approved by organization admin
- Verify `read:org` scope is included
- Check organization third-party access settings

#### "insufficient_scope" error
**Problem**: Missing required permissions
**Solution**: Ensure all required scopes are configured:
```json
"scopes": ["user:email", "read:org", "read:user"]
```

### Debug Mode

Enable OAuth debugging in your configuration:
```json
{
  "oauth": {
    "debug": true,
    "log_level": "debug"
  }
}
```

## Security Considerations

1. **Keep Client Secret Secure**
   - Never commit client secret to version control
   - Use environment variables for production
   - Rotate secrets regularly

2. **Validate Callback URLs**
   - Only use HTTPS in production
   - Whitelist specific callback URLs
   - Validate state parameter

3. **Organization Access**
   - Only grant necessary organization access
   - Regularly audit approved applications
   - Monitor organization member data access

## Next Steps

- **Test with your team**: Have team members test the OAuth flow
- **Configure role mappings**: Set up group-to-role mappings for your organization structure
- **Set up monitoring**: Monitor OAuth authentication success/failure rates
- **Enable other providers**: Consider adding Google or Microsoft OAuth for broader access

## Support

If you encounter issues:
1. Check the [common issues guide](../troubleshooting/common-issues.md)
2. Enable debug logging
3. Verify your GitHub organization settings
4. Test with a simple OAuth flow first

Happy OAuth-ing! 🚀