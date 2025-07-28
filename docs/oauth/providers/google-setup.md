# Google Workspace OAuth Setup Guide

This guide will help you set up Google Workspace OAuth authentication for Notificator WebUI, including Google Groups membership sync.

## Prerequisites

- Google Workspace account with admin privileges (for group sync)
- Google Cloud Console access
- Notificator application deployed with HTTPS access
- Access to modify your Notificator configuration

## Step 1: Create Google Cloud Project

1. **Go to Google Cloud Console**
   - Navigate to [Google Cloud Console](https://console.cloud.google.com/)
   - Select an existing project or create a new one

2. **Create New Project** (if needed)
   - Click the project dropdown at the top
   - Click **"New Project"**
   - Enter project name: `Notificator OAuth`
   - Select your organization
   - Click **"Create"**

## Step 2: Enable Required APIs

1. **Navigate to APIs & Services**
   - Go to **APIs & Services** → **Library**
   - Search for and enable these APIs:

2. **Enable Google+ API** (for user info)
   - Search: "Google+ API"
   - Click **"Enable"**

3. **Enable Admin SDK API** (for group membership)
   - Search: "Admin SDK API"
   - Click **"Enable"**
   - Note: Requires Google Workspace admin to use

4. **Enable People API** (alternative for user info)
   - Search: "People API"
   - Click **"Enable"**

## Step 3: Configure OAuth Consent Screen

1. **Go to OAuth Consent Screen**
   - Navigate to **APIs & Services** → **OAuth consent screen**

2. **Choose User Type**
   - Select **"Internal"** (for Google Workspace users only)
   - Or **"External"** (for any Google account) - requires verification for production
   - Click **"Create"**

3. **Fill OAuth Consent Screen**
   - **App name**: `Notificator WebUI`
   - **User support email**: Your admin email
   - **App logo**: Upload your app logo (optional)
   - **App domain**: `https://your-domain.com`
   - **Authorized domains**: Add your domain (e.g., `your-domain.com`)
   - **Developer contact email**: Your admin email

4. **Add Scopes**
   - Click **"Add or Remove Scopes"**
   - Add these scopes:
     - `openid` - OpenID Connect
     - `email` - Email address
     - `profile` - Basic profile info
     - `https://www.googleapis.com/auth/admin.directory.group.readonly` - Read groups

5. **Test Users** (if External)
   - Add test user email addresses
   - These users can test OAuth before app verification

## Step 4: Create OAuth Credentials

1. **Go to Credentials**
   - Navigate to **APIs & Services** → **Credentials**

2. **Create OAuth 2.0 Client ID**
   - Click **"+ Create Credentials"** → **"OAuth 2.0 Client ID"**
   - Choose **"Web application"**

3. **Configure Web Application**
   - **Name**: `Notificator WebUI OAuth`
   - **Authorized JavaScript origins**: `https://your-domain.com`
   - **Authorized redirect URIs**: `https://your-domain.com/api/v1/oauth/google/callback`

4. **Save Credentials**
   - Click **"Create"**
   - Copy your **Client ID** and **Client Secret**
   - Download the JSON file for backup

## Step 5: Google Workspace Admin Setup (For Groups)

### Enable Domain-Wide Delegation (Optional, for service account)
1. **Go to Admin Console**
   - Navigate to [Google Admin Console](https://admin.google.com/)
   - Go to **Security** → **API Controls**

2. **Domain-wide Delegation**
   - Click **"Domain-wide delegation"**
   - Click **"Add new"**
   - Enter your Client ID
   - Add OAuth scopes: `https://www.googleapis.com/auth/admin.directory.group.readonly`

### Admin SDK Access
- Ensure your OAuth application has Admin SDK API enabled
- The authenticating user must be a Google Workspace admin
- Regular users cannot access group membership via Admin SDK

## Step 6: Configuration

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
      "google": {
        "client_id": "your_google_client_id.apps.googleusercontent.com",
        "client_secret": "your_google_client_secret",
        "scopes": [
          "openid",
          "email",
          "profile",
          "https://www.googleapis.com/auth/admin.directory.group.readonly"
        ],
        "auth_url": "https://accounts.google.com/o/oauth2/auth",
        "token_url": "https://oauth2.googleapis.com/token",
        "user_info_url": "https://openidconnect.googleapis.com/v1/userinfo",
        "groups_url": "https://admin.googleapis.com/admin/directory/v1/groups",
        "group_scopes": ["https://www.googleapis.com/auth/admin.directory.group.readonly"],
        "group_mapping": {
          "admin": "administrator",
          "editor": "editor",
          "viewer": "viewer"
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

# Google OAuth
OAUTH_GOOGLE_CLIENT_ID=your_google_client_id.apps.googleusercontent.com
OAUTH_GOOGLE_CLIENT_SECRET=your_google_client_secret
OAUTH_GOOGLE_SCOPES=openid,email,profile,https://www.googleapis.com/auth/admin.directory.group.readonly

# Group Sync
OAUTH_GROUP_SYNC_ENABLED=true
OAUTH_GROUP_SYNC_ON_LOGIN=true
```

## Step 7: Group Mapping Configuration

Google Groups can be mapped to application roles based on:

### Google Group Types
- **Security Groups** - Traditional security groups
- **Mailing Lists** - Email distribution lists  
- **Dynamic Groups** - Rule-based membership
- **Custom Groups** - Manually managed groups

### Example Group Mapping by Name
```json
{
  "group_mapping": {
    "notificator-admins@your-domain.com": "administrator",
    "notificator-editors@your-domain.com": "editor", 
    "notificator-viewers@your-domain.com": "viewer",
    "engineering@your-domain.com": "editor",
    "support@your-domain.com": "viewer"
  }
}
```

### Example Group Mapping by Pattern
```json
{
  "group_patterns": {
    "*-admin*": "administrator",
    "*-editor*": "editor", 
    "engineering-*": "editor",
    "*": "viewer"
  }
}
```

## Step 8: Testing Your Setup

1. **Start your application** with OAuth configuration
2. **Navigate to login page**: `https://your-domain.com/login`
3. **Click "Login with Google"**
4. **Sign in with Google Workspace account**
5. **Grant permissions** when prompted
6. **Verify successful login** and check that groups are synced

### Debug Information
Check your application logs for:
```
[INFO] OAuth: Google user authenticated: user@your-domain.com
[INFO] OAuth: Synced 3 groups for user: user@your-domain.com
[INFO] OAuth: User role assigned: editor (from engineering@your-domain.com)
```

## Step 9: Development Setup

For local development with Google OAuth:

```bash
# Start your local application
./notificator webui --port 3000

# Use ngrok for HTTPS (required by Google OAuth)
ngrok http 3000

# Update your Google OAuth client redirect URI to:
# https://abc123.ngrok.io/api/v1/oauth/google/callback
```

## Troubleshooting

### Common Issues

#### "Error 403: access_denied"
**Problem**: User cannot access Admin SDK APIs
**Solutions**: 
- User must be Google Workspace admin
- Enable Admin SDK API in Google Cloud Console
- Grant domain-wide delegation if using service account
- Check OAuth consent screen approval status

#### "redirect_uri_mismatch"
**Problem**: Callback URL doesn't match registered URL
**Solution**:
- Verify redirect URI exactly matches: `https://your-domain.com/api/v1/oauth/google/callback`
- Check Google Cloud Console OAuth client settings
- Ensure HTTPS is used (required by Google)

#### Groups not syncing
**Problem**: User groups not appearing in application
**Solutions**:
- Verify Admin SDK API is enabled
- User must be Google Workspace admin (for directory access)
- Check group membership in Google Admin Console
- Ensure correct OAuth scopes are requested

#### "This app isn't verified"
**Problem**: Google shows unverified app warning
**Solutions**:
- Use "Internal" user type for Workspace users only
- Submit app for verification if using "External" type
- Add users to test user list during development

#### "insufficient_scope" error
**Problem**: Missing Admin SDK permissions
**Solution**: Ensure Admin SDK scope is included:
```json
"scopes": [
  "openid", "email", "profile",
  "https://www.googleapis.com/auth/admin.directory.group.readonly"
]
```

### Debug Mode

Enable OAuth debugging:
```json
{
  "oauth": {
    "debug": true,
    "log_level": "debug",
    "google": {
      "log_api_calls": true
    }
  }
}
```

## Security Considerations

1. **Client Secret Protection**
   - Store client secret securely (environment variables)
   - Never commit secrets to version control
   - Rotate secrets regularly

2. **Scope Minimization**
   - Only request necessary OAuth scopes
   - Admin SDK access should be limited to necessary groups
   - Regular users don't need directory access

3. **App Verification**
   - Submit for Google app verification in production
   - Use internal apps for Workspace-only access
   - Monitor OAuth consent screen usage

4. **Domain Validation**
   - Verify authorized domains in OAuth consent screen
   - Use HTTPS for all OAuth callbacks
   - Validate state parameter to prevent CSRF

## Advanced Configuration

### Service Account (Alternative to User OAuth)
For server-to-server group sync without user OAuth:

```json
{
  "google": {
    "service_account": {
      "enabled": true,
      "credentials_file": "/path/to/service-account.json",
      "subject": "admin@your-domain.com",
      "scopes": ["https://www.googleapis.com/auth/admin.directory.group.readonly"]
    }
  }
}
```

### Custom Claims from ID Token
Extract custom claims from Google ID token:
```json
{
  "google": {
    "custom_claims": {
      "department": "user.department",
      "employee_id": "user.employee_id"
    }
  }
}
```

## Next Steps

- **Test with different user types**: Admin vs regular users
- **Configure group-based role mappings**: Set up your organization's group structure
- **Set up monitoring**: Monitor OAuth success/failure rates
- **Consider other providers**: Add GitHub or Microsoft for broader access
- **Implement group-based features**: Use group membership for authorization

## Support

If you encounter issues:
1. Check Google Cloud Console logs
2. Verify Google Workspace admin permissions
3. Test OAuth flow with minimal scopes first
4. Review the [common issues guide](../troubleshooting/common-issues.md)

Google OAuth can be complex, but it provides powerful integration with Google Workspace! 🌟