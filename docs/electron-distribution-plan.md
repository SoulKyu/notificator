# Electron Desktop App Distribution Plan

This document outlines the plan for implementing a feature where the Notificator WebUI can distribute pre-configured Electron desktop applications to users.

## Overview

Allow users to download platform-specific Electron desktop apps directly from the WebUI, with the apps automatically configured to connect to the correct server URL.

## Architecture

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│  GitHub Actions │────▶│  GitHub Releases │◀────│  WebUI Server   │
│  (Builds Apps)  │     │  (Stores Bins)   │     │  (Serves Apps)  │
└─────────────────┘     └──────────────────┘     └─────────────────┘
                                                           │
                                                           ▼
                                                    ┌─────────────┐
                                                    │    User     │
                                                    │  Downloads  │
                                                    └─────────────┘
```

## Implementation Phases

### Phase 1: GitHub Actions Build System

#### 1.1 Create Build Workflow
Create `.github/workflows/electron-build.yml`:

```yaml
name: Build Electron Apps
on:
  push:
    tags:
      - 'electron-v*'
  workflow_dispatch:

jobs:
  build:
    strategy:
      matrix:
        os: [ubuntu-latest, windows-latest, macos-latest]
        include:
          - os: ubuntu-latest
            platform: linux
          - os: windows-latest
            platform: win
          - os: macos-latest
            platform: mac
    
    runs-on: ${{ matrix.os }}
    
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-node@v3
        with:
          node-version: '18'
      
      - name: Install dependencies
        working-directory: electron
        run: npm ci
      
      - name: Build Electron App
        working-directory: electron
        run: npm run dist:${{ matrix.platform }}
      
      - name: Upload artifacts
        uses: actions/upload-artifact@v3
        with:
          name: electron-${{ matrix.platform }}
          path: electron/dist/*
```

#### 1.2 Configure electron-builder
Update `electron/package.json` build configuration:

```json
{
  "build": {
    "artifactName": "notificator-desktop-${version}-${platform}-${arch}.${ext}",
    "generateUpdatesFilesForAllChannels": true,
    "publish": {
      "provider": "github",
      "owner": "yourusername",
      "repo": "notificator"
    }
  }
}
```

### Phase 2: Configuration Injection System

#### 2.1 Electron App Configuration Support
Modify `electron/main.js` to support external configuration:

```javascript
// Configuration loading priority:
// 1. External config file (highest priority)
// 2. Environment variables
// 3. Default config

function loadConfig() {
  const externalConfigPath = path.join(
    process.platform === 'darwin' 
      ? path.dirname(path.dirname(path.dirname(process.execPath)))
      : path.dirname(process.execPath),
    'config.override.json'
  );
  
  if (fs.existsSync(externalConfigPath)) {
    const overrideConfig = JSON.parse(fs.readFileSync(externalConfigPath, 'utf8'));
    Object.assign(config, overrideConfig);
  }
  
  // Rest of existing config loading...
}
```

#### 2.2 Configuration Injection Methods

**For Windows (.exe)**:
- Create `config.override.json` alongside the executable
- Or use rcedit to modify embedded resources

**For macOS (.app)**:
- Inject into `Notificator.app/Contents/Resources/config.override.json`
- Preserve app signature with ad-hoc signing after modification

**For Linux (AppImage)**:
- Create `config.override.json` in the same directory as AppImage
- Or mount AppImage and modify internal files

### Phase 3: WebUI Implementation

#### 3.1 Download Service
Create `internal/webui/desktop_download.go`:

```go
type DesktopDownloadService struct {
    githubToken string
    githubOwner string
    githubRepo  string
    cacheDir    string
}

func (s *DesktopDownloadService) GetLatestRelease() (*GithubRelease, error) {
    // Fetch latest release info from GitHub API
}

func (s *DesktopDownloadService) DownloadAndConfigure(platform, serverURL string) (io.ReadCloser, error) {
    // 1. Get latest release
    // 2. Find appropriate asset for platform
    // 3. Download binary from GitHub
    // 4. Inject server configuration
    // 5. Return configured binary stream
}

func (s *DesktopDownloadService) InjectConfiguration(binaryPath, serverURL string) error {
    // Platform-specific configuration injection
}
```

#### 3.2 Download Routes
Add to `internal/webui/routes.go`:

```go
// Desktop app downloads
r.GET("/api/desktop/info", h.getDesktopInfo)
r.GET("/api/desktop/download/:platform", h.downloadDesktop)
```

Handler implementation:

```go
func (h *Handler) downloadDesktop(c *gin.Context) {
    platform := c.Param("platform")
    
    // Get current server URL from request
    proto := "http"
    if c.Request.TLS != nil {
        proto = "https"
    }
    serverURL := fmt.Sprintf("%s://%s", proto, c.Request.Host)
    
    // Download and configure
    reader, err := h.desktopService.DownloadAndConfigure(platform, serverURL)
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }
    defer reader.Close()
    
    // Set appropriate headers
    filename := fmt.Sprintf("notificator-desktop-%s.%s", platform, getExtension(platform))
    c.Header("Content-Type", "application/octet-stream")
    c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
    
    // Stream to client
    io.Copy(c.Writer, reader)
}
```

### Phase 4: UI Integration

#### 4.1 Add Download Section to Settings
Update the settings template to include desktop download options:

```html
<div class="desktop-downloads">
    <h3>Desktop Application</h3>
    <p>Download Notificator desktop app pre-configured for this server.</p>
    
    <div class="download-buttons">
        <!-- Platform detection and dynamic buttons -->
        <button onclick="downloadDesktop('windows')" class="btn-download">
            <i class="icon-windows"></i> Download for Windows
        </button>
        <button onclick="downloadDesktop('mac')" class="btn-download">
            <i class="icon-apple"></i> Download for macOS
        </button>
        <button onclick="downloadDesktop('linux')" class="btn-download">
            <i class="icon-linux"></i> Download for Linux
        </button>
    </div>
    
    <div class="download-info">
        <small>Version: <span id="desktop-version">Loading...</span></small>
    </div>
</div>
```

#### 4.2 JavaScript Implementation
```javascript
async function downloadDesktop(platform) {
    const btn = event.target;
    btn.disabled = true;
    btn.textContent = 'Preparing download...';
    
    try {
        const response = await fetch(`/api/desktop/download/${platform}`);
        if (!response.ok) throw new Error('Download failed');
        
        const blob = await response.blob();
        const url = window.URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `notificator-desktop-${platform}`;
        document.body.appendChild(a);
        a.click();
        window.URL.revokeObjectURL(url);
        document.body.removeChild(a);
        
        btn.textContent = 'Download complete!';
        setTimeout(() => {
            btn.disabled = false;
            btn.textContent = `Download for ${platform}`;
        }, 3000);
    } catch (error) {
        btn.disabled = false;
        btn.textContent = 'Download failed';
        console.error('Download error:', error);
    }
}
```

### Phase 5: Security & Polish

#### 5.1 Security Measures
1. **Checksum Verification**
   - Generate SHA256 checksums during build
   - Verify checksums after download
   - Display checksums to users

2. **Code Signing** (Optional)
   - Windows: Authenticode signing
   - macOS: Developer ID signing
   - Linux: GPG signatures

3. **HTTPS Only**
   - Enforce HTTPS for download endpoints
   - Verify GitHub API SSL certificates

#### 5.2 Caching Strategy
```go
type CacheEntry struct {
    Version     string
    Platform    string
    DownloadURL string
    Checksum    string
    CachedAt    time.Time
}

// Cache GitHub releases for 1 hour
// Cache modified binaries for 24 hours
```

#### 5.3 Analytics
Track:
- Download counts by platform
- Download failures
- Configuration injection success rate
- Geographic distribution

### Phase 6: Advanced Features (Future)

1. **Auto-update Integration**
   - Use electron-updater with custom feed URL
   - Server provides update manifest

2. **Team-specific Configurations**
   - Different configs for different teams/environments
   - Role-based access to desktop apps

3. **Progressive Web App Alternative**
   - Offer PWA installation as alternative
   - Unified codebase approach

## Implementation Timeline

1. **Week 1-2**: GitHub Actions setup and build configuration
2. **Week 3-4**: Configuration injection system
3. **Week 5-6**: WebUI endpoints and download service
4. **Week 7**: UI integration and platform detection
5. **Week 8**: Testing, security, and documentation

## Technical Considerations

### Platform-Specific Challenges

**Windows**:
- UAC prompts for unsigned apps
- Antivirus false positives
- Configuration in %APPDATA%

**macOS**:
- Gatekeeper restrictions
- Notarization requirements
- App Transport Security

**Linux**:
- AppImage vs native packages
- Desktop integration varies by distro
- Permissions for auto-update

### Performance Optimization

1. **CDN Distribution**
   - Use GitHub's CDN for artifacts
   - Consider CloudFlare for caching

2. **Parallel Downloads**
   - Support resume/retry
   - Progress indication

3. **Binary Size Optimization**
   - Tree-shaking unused Electron modules
   - Platform-specific builds

## Security Checklist

- [ ] HTTPS only for all downloads
- [ ] Checksum verification
- [ ] Code signing (where applicable)
- [ ] Input validation for server URLs
- [ ] Rate limiting on download endpoints
- [ ] Audit logging for downloads
- [ ] Regular security updates

## Testing Strategy

1. **Automated Testing**
   - GitHub Actions build verification
   - Configuration injection tests
   - Cross-platform download tests

2. **Manual Testing**
   - Fresh OS installations
   - Various network conditions
   - Firewall/antivirus compatibility

3. **User Acceptance Testing**
   - Beta program for early adopters
   - Feedback collection system

## Documentation Requirements

1. **User Documentation**
   - Download instructions per platform
   - Troubleshooting guide
   - FAQ section

2. **Admin Documentation**
   - Build process overview
   - Configuration options
   - Monitoring and metrics

3. **Developer Documentation**
   - Architecture decisions
   - Extension points
   - Contribution guidelines

## Success Metrics

- Download success rate > 95%
- Configuration injection success rate > 99%
- User satisfaction score > 4.5/5
- Support ticket reduction by 50%
- Adoption rate > 60% of active users

## References

- [Electron Builder Documentation](https://www.electron.build/)
- [GitHub Releases API](https://docs.github.com/en/rest/releases)
- [Electron Auto-updater](https://www.electron.build/auto-update)
- [Code Signing Best Practices](https://developer.apple.com/documentation/security/notarizing_macos_software_before_distribution)