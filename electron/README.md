# Notificator Desktop Client

A desktop client for the Notificator alert management system built with Electron.

## Features

- 🖥️ Native desktop application for Windows, macOS, and Linux
- 🔔 System tray integration with quick access to alerts
- 🔄 Automatic server connection with configurable URL
- 💾 Persistent window state and settings
- 🎨 Native menus and keyboard shortcuts
- 🔐 OAuth authentication handling (Google, GitHub, Microsoft, etc.)
- 💾 Session persistence across app restarts
- 🔒 Secure context isolation
- 🌐 External links open in default browser

## Prerequisites

- Node.js 16.x or higher
- npm or yarn package manager
- Notificator WebUI server running (default: http://localhost:8081)

## Development

### Setup

```bash
# Navigate to electron directory
cd electron

# Install dependencies
npm install
```

### Running in Development

```bash
# Start the Electron app (connects to localhost:8081)
npm start

# Start with custom server URL
NOTIFICATOR_URL=https://your-notificator-server.com npm start

# Start with environment name
NOTIFICATOR_ENV=Production npm start
```

### Environment Variables

- `NOTIFICATOR_URL` - WebUI server URL (default: `http://localhost:8081`)
- `NOTIFICATOR_ENV` - Environment name shown in title (default: `Development`)

## Building

### Build for Current Platform

```bash
# Create distributable for current OS
npm run dist
```

### Build for Specific Platforms

```bash
# macOS
npm run dist:mac

# Windows
npm run dist:win

# Linux
npm run dist:linux

# All platforms (requires appropriate build environment)
npm run dist:all
```

### Build Output

Built applications will be in the `dist/` directory:

- **macOS**: `Notificator-1.0.0.dmg`, `Notificator-1.0.0-mac.zip`
- **Windows**: `Notificator Setup 1.0.0.exe`, `Notificator 1.0.0.exe` (portable)
- **Linux**: `Notificator-1.0.0.AppImage`, `.deb`, `.rpm`

## Configuration

The app stores configuration in the user data directory:

- **Windows**: `%APPDATA%/notificator-desktop/config.json`
- **macOS**: `~/Library/Application Support/notificator-desktop/config.json`
- **Linux**: `~/.config/notificator-desktop/config.json`

### Configuration Options

```json
{
  "url": "https://your-notificator-server.com",
  "env": "Production",
  "windowState": {
    "width": 1400,
    "height": 900,
    "x": 100,
    "y": 100
  }
}
```

## Icons

For production builds, replace the placeholder icons in `assets/`:

1. **icon.png** - Main app icon (512x512 recommended)
2. **icon.ico** - Windows icon (multiple resolutions)
3. **icon.icns** - macOS icon (multiple resolutions)
4. **tray-icon.png** - System tray icon (16x16)

### Creating Icons

```bash
# Convert PNG to ICO (Windows)
convert icon.png -define icon:auto-resize=16,32,48,256 icon.ico

# Create ICNS (macOS)
mkdir icon.iconset
sips -z 16 16     icon.png --out icon.iconset/icon_16x16.png
sips -z 32 32     icon.png --out icon.iconset/icon_16x16@2x.png
sips -z 32 32     icon.png --out icon.iconset/icon_32x32.png
sips -z 64 64     icon.png --out icon.iconset/icon_32x32@2x.png
sips -z 128 128   icon.png --out icon.iconset/icon_128x128.png
sips -z 256 256   icon.png --out icon.iconset/icon_128x128@2x.png
sips -z 256 256   icon.png --out icon.iconset/icon_256x256.png
sips -z 512 512   icon.png --out icon.iconset/icon_256x256@2x.png
sips -z 512 512   icon.png --out icon.iconset/icon_512x512.png
sips -z 1024 1024 icon.png --out icon.iconset/icon_512x512@2x.png
iconutil -c icns icon.iconset
```

## Code Signing

### macOS

1. Obtain a Developer ID certificate from Apple
2. Add to electron-builder config:
   ```json
   "mac": {
     "identity": "Developer ID Application: Your Name (XXXXXXXXXX)"
   }
   ```

### Windows

1. Obtain a code signing certificate
2. Add to electron-builder config:
   ```json
   "win": {
     "certificateFile": "path/to/certificate.pfx",
     "certificatePassword": "password"
   }
   ```

## Auto Updates

The app is configured for GitHub releases. To enable:

1. Update `package.json` with your GitHub repository
2. Create a GitHub release with your built files
3. The app will check for updates automatically

## Troubleshooting

### App won't connect to server

- Check that the WebUI server is running
- Verify the server URL in Settings (gear icon in tray menu)
- Check firewall settings

### Blank window on startup

- Server might be down or unreachable
- Try refreshing (Cmd/Ctrl + R)
- Check the console (View → Toggle Developer Tools)

### System tray icon not showing

- **Linux**: Ensure you have a system tray extension installed
- **Windows**: Check if it's hidden in the system tray overflow

## Security

- Context isolation is enabled
- Node integration is disabled
- External navigation is restricted
- All external links open in default browser
- OAuth authentication handled securely within app context
- Session cookies filtered and stored securely

## OAuth Authentication

The app automatically detects and handles OAuth login flows:

1. **Automatic Detection**: Recognizes OAuth URLs from common providers (Google, GitHub, Microsoft, etc.)
2. **In-App Authentication**: Opens OAuth in a secure Electron window instead of external browser
3. **Session Persistence**: Saves authentication state between app restarts
4. **Cookie Syncing**: Properly transfers authentication cookies to main window

### Supported OAuth Providers

- Google (accounts.google.com)
- GitHub (github.com)
- Microsoft (login.microsoftonline.com)
- Auth0, Okta, and other common providers
- Any URL containing OAuth parameters or patterns

## License

[Your License]

## Contributing

1. Fork the repository
2. Create your feature branch
3. Commit your changes
4. Push to the branch
5. Create a Pull Request