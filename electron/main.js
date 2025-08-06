const { app, BrowserWindow, Tray, Menu, nativeImage, shell, ipcMain, dialog, Notification } = require('electron');
const path = require('path');
const fs = require('fs');

let mainWindow;
let tray;
let isQuitting = false;
let oauthWindow = null;

// Configuration
const config = {
  url: process.env.NOTIFICATOR_URL || 'http://localhost:8081',
  env: process.env.NOTIFICATOR_ENV || 'Development',
  windowState: {
    width: 1400,
    height: 900,
    x: undefined,
    y: undefined
  }
};

// Load saved configuration
function loadConfig() {
  try {
    const configPath = path.join(app.getPath('userData'), 'config.json');
    if (fs.existsSync(configPath)) {
      const savedConfig = JSON.parse(fs.readFileSync(configPath, 'utf8'));
      Object.assign(config, savedConfig);
    }
  } catch (error) {
    console.error('Error loading config:', error);
  }
}

// Save configuration
function saveConfig() {
  try {
    const configPath = path.join(app.getPath('userData'), 'config.json');
    fs.writeFileSync(configPath, JSON.stringify(config, null, 2));
  } catch (error) {
    console.error('Error saving config:', error);
  }
}

// Save session data for persistence
async function saveSession() {
  try {
    const sessionPath = path.join(app.getPath('userData'), 'session.json');
    const cookies = await mainWindow.webContents.session.cookies.get({});
    
    // Filter cookies to only save relevant ones
    const relevantCookies = cookies.filter(cookie => {
      const domain = cookie.domain || '';
      return domain.includes(new URL(config.url).hostname) || 
             cookie.name.toLowerCase().includes('auth') ||
             cookie.name.toLowerCase().includes('session') ||
             cookie.name.toLowerCase().includes('token');
    });
    
    const sessionData = {
      cookies: relevantCookies,
      timestamp: Date.now(),
      url: config.url
    };
    
    fs.writeFileSync(sessionPath, JSON.stringify(sessionData, null, 2));
  } catch (error) {
    console.error('Error saving session:', error);
  }
}

// Load session data
async function loadSession() {
  try {
    const sessionPath = path.join(app.getPath('userData'), 'session.json');
    
    if (!fs.existsSync(sessionPath)) {
      return;
    }
    
    const sessionData = JSON.parse(fs.readFileSync(sessionPath, 'utf8'));
    
    // Check if session is not too old (24 hours)
    const maxAge = 24 * 60 * 60 * 1000; // 24 hours
    if (Date.now() - sessionData.timestamp > maxAge) {
      console.log('Session expired, removing old session file');
      fs.unlinkSync(sessionPath);
      return;
    }
    
    // Only restore session if URL matches
    if (sessionData.url !== config.url) {
      console.log('URL changed, not restoring session');
      return;
    }
    
    // Restore cookies
    for (const cookie of sessionData.cookies) {
      try {
        await mainWindow.webContents.session.cookies.set({
          url: config.url,
          name: cookie.name,
          value: cookie.value,
          domain: cookie.domain,
          path: cookie.path || '/',
          secure: cookie.secure,
          httpOnly: cookie.httpOnly,
          expirationDate: cookie.expirationDate
        });
      } catch (error) {
        console.error('Error restoring cookie:', cookie.name, error);
      }
    }
    
    console.log('Session restored successfully');
  } catch (error) {
    console.error('Error loading session:', error);
  }
}

// Create the main application window
function createWindow() {
  loadConfig();

  mainWindow = new BrowserWindow({
    width: config.windowState.width,
    height: config.windowState.height,
    x: config.windowState.x,
    y: config.windowState.y,
    minWidth: 800,
    minHeight: 600,
    icon: path.join(__dirname, 'assets', 'icon.png'),
    webPreferences: {
      nodeIntegration: false,
      contextIsolation: true,
      preload: path.join(__dirname, 'preload.js')
    },
    title: `Notificator - ${config.env}`,
    titleBarStyle: process.platform === 'darwin' ? 'hiddenInset' : 'default',
    frame: true,
    show: false // Don't show until ready
  });

  // Load the WebUI URL
  mainWindow.loadURL(config.url);

  // Show window when ready
  mainWindow.once('ready-to-show', () => {
    mainWindow.show();
    // Setup OAuth interception after window is ready
    setupOAuthInterception();
    // Load saved session
    loadSession();
    // Inject notification polyfill immediately
    setTimeout(() => {
      injectNotificationPolyfill();
      setupNotificationEventListener();
    }, 1000);
  });

  // Handle window state changes
  mainWindow.on('resize', saveWindowState);
  mainWindow.on('move', saveWindowState);

  // Handle close event
  mainWindow.on('close', (event) => {
    if (!isQuitting && process.platform !== 'darwin') {
      event.preventDefault();
      mainWindow.hide();
      return false;
    }
  });

  // Handle navigation - DO NOT open external browser for OAuth
  mainWindow.webContents.on('will-navigate', (event, url) => {
    // Let setupOAuthInterception handle OAuth URLs
    if (!url.startsWith(config.url)) {
      // Don't open external browser automatically - let OAuth handler decide
      console.log('Navigation blocked, will be handled by OAuth interceptor:', url);
    }
  });

  // Handle new window requests - DO NOT open external browser for OAuth
  mainWindow.webContents.setWindowOpenHandler(({ url }) => {
    console.log('Window open handler called for:', url);
    // Let setupOAuthInterception handle all external URLs
    return { action: 'deny' }; // Always deny, let OAuth handler manage
  });

  // Handle failed loads
  mainWindow.webContents.on('did-fail-load', (event, errorCode, errorDescription, validatedURL) => {
    if (errorCode === -3) return; // Ignore aborted requests
    
    console.error('Failed to load:', errorDescription);
    dialog.showMessageBox(mainWindow, {
      type: 'error',
      title: 'Connection Error',
      message: 'Failed to connect to Notificator server',
      detail: `Unable to reach ${config.url}\n\nError: ${errorDescription}`,
      buttons: ['Retry', 'Settings', 'Quit'],
      defaultId: 0
    }).then(result => {
      if (result.response === 0) {
        mainWindow.reload();
      } else if (result.response === 1) {
        showSettings();
      } else {
        app.quit();
      }
    });
  });
}

// Save window state
let saveWindowStateTimeout;
function saveWindowState() {
  clearTimeout(saveWindowStateTimeout);
  saveWindowStateTimeout = setTimeout(() => {
    if (!mainWindow.isMinimized() && !mainWindow.isMaximized()) {
      const bounds = mainWindow.getBounds();
      config.windowState = bounds;
      saveConfig();
    }
  }, 1000);
}

// Create system tray
function createTray() {
  const iconPath = path.join(__dirname, 'assets', 'tray-icon.png');
  const icon = nativeImage.createFromPath(iconPath);
  
  // On macOS, resize to 16x16
  if (process.platform === 'darwin') {
    tray = new Tray(icon.resize({ width: 16, height: 16 }));
  } else {
    tray = new Tray(icon);
  }

  updateTrayMenu();

  tray.on('click', () => {
    if (process.platform === 'win32') {
      mainWindow.isVisible() ? mainWindow.hide() : mainWindow.show();
    }
  });

  tray.on('double-click', () => {
    mainWindow.show();
  });
}

// Update tray menu
function updateTrayMenu() {
  const contextMenu = Menu.buildFromTemplate([
    {
      label: mainWindow && mainWindow.isVisible() ? 'Hide Notificator' : 'Show Notificator',
      click: () => {
        if (mainWindow.isVisible()) {
          mainWindow.hide();
        } else {
          mainWindow.show();
        }
      }
    },
    { type: 'separator' },
    {
      label: 'Go to Dashboard',
      click: () => {
        mainWindow.show();
        mainWindow.loadURL(config.url);
      }
    },
    {
      label: 'Go to Alerts',
      click: () => {
        mainWindow.show();
        mainWindow.loadURL(`${config.url}/alerts`);
      }
    },
    { type: 'separator' },
    {
      label: 'Settings...',
      click: showSettings
    },
    {
      label: 'Refresh',
      click: () => {
        mainWindow.reload();
      }
    },
    { type: 'separator' },
    {
      label: 'About Notificator',
      click: () => {
        dialog.showMessageBox(mainWindow, {
          type: 'info',
          title: 'About Notificator',
          message: 'Notificator Desktop Client',
          detail: `Version: ${app.getVersion()}\nElectron: ${process.versions.electron}\nNode: ${process.versions.node}\n\nA desktop client for Notificator alert management system.`,
          buttons: ['OK']
        });
      }
    },
    { type: 'separator' },
    {
      label: 'Quit',
      click: () => {
        isQuitting = true;
        app.quit();
      }
    }
  ]);

  tray.setContextMenu(contextMenu);
  tray.setToolTip(`Notificator - ${config.env}`);
}

// Show settings dialog
function showSettings() {
  const settingsWindow = new BrowserWindow({
    width: 500,
    height: 350,
    parent: mainWindow,
    modal: true,
    resizable: false,
    minimizable: false,
    maximizable: false,
    webPreferences: {
      nodeIntegration: true,
      contextIsolation: false
    },
    title: 'Notificator Settings'
  });

  const settingsHTML = `
    <!DOCTYPE html>
    <html>
    <head>
      <style>
        body {
          font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
          padding: 20px;
          margin: 0;
          background: #f5f5f5;
        }
        h2 {
          margin-top: 0;
          color: #333;
        }
        .form-group {
          margin-bottom: 20px;
        }
        label {
          display: block;
          margin-bottom: 5px;
          color: #555;
          font-weight: 500;
        }
        input[type="text"] {
          width: 100%;
          padding: 8px 12px;
          border: 1px solid #ddd;
          border-radius: 4px;
          font-size: 14px;
          box-sizing: border-box;
        }
        input[type="text"]:focus {
          outline: none;
          border-color: #4CAF50;
        }
        .help-text {
          font-size: 12px;
          color: #777;
          margin-top: 4px;
        }
        .button-group {
          display: flex;
          justify-content: flex-end;
          gap: 10px;
          margin-top: 30px;
        }
        button {
          padding: 8px 16px;
          border: none;
          border-radius: 4px;
          font-size: 14px;
          cursor: pointer;
          transition: background-color 0.2s;
        }
        .btn-primary {
          background: #4CAF50;
          color: white;
        }
        .btn-primary:hover {
          background: #45a049;
        }
        .btn-secondary {
          background: #ddd;
          color: #333;
        }
        .btn-secondary:hover {
          background: #ccc;
        }
      </style>
    </head>
    <body>
      <h2>Notificator Settings</h2>
      <form id="settings-form">
        <div class="form-group">
          <label for="server-url">Server URL</label>
          <input type="text" id="server-url" placeholder="http://localhost:8081" value="${config.url}">
          <div class="help-text">The URL of your Notificator WebUI server</div>
        </div>
        <div class="form-group">
          <label for="env-name">Environment Name</label>
          <input type="text" id="env-name" placeholder="Development" value="${config.env}">
          <div class="help-text">Displayed in the window title and tray tooltip</div>
        </div>
        <div class="button-group">
          <button type="button" class="btn-secondary" onclick="window.close()">Cancel</button>
          <button type="submit" class="btn-primary">Save</button>
        </div>
      </form>
      <script>
        const { ipcRenderer } = require('electron');
        
        document.getElementById('settings-form').addEventListener('submit', (e) => {
          e.preventDefault();
          const settings = {
            url: document.getElementById('server-url').value,
            env: document.getElementById('env-name').value
          };
          ipcRenderer.send('save-settings', settings);
        });

        ipcRenderer.on('settings-saved', () => {
          window.close();
        });
      </script>
    </body>
    </html>
  `;

  settingsWindow.loadURL(`data:text/html;charset=utf-8,${encodeURIComponent(settingsHTML)}`);
}

// OAuth Authentication Handler
function handleOAuthLogin(oauthUrl) {
  return new Promise((resolve, reject) => {
    // Close existing OAuth window if open
    if (oauthWindow) {
      oauthWindow.close();
      oauthWindow = null;
    }

    // Create OAuth window
    oauthWindow = new BrowserWindow({
      width: 500,
      height: 700,
      parent: mainWindow,
      modal: true,
      webPreferences: {
        nodeIntegration: false,
        contextIsolation: true,
        session: mainWindow.webContents.session // Share session with main window
      },
      title: 'Login to Notificator'
    });

    // Load the OAuth URL
    oauthWindow.loadURL(oauthUrl);

    // Handle successful authentication - multiple detection strategies
    oauthWindow.webContents.on('will-navigate', (event, navigationUrl) => {
      console.log('🔄 OAuth window navigating to:', navigationUrl);
      
      try {
        const url = new URL(navigationUrl);
        const configUrl = new URL(config.url);
        
        // Strategy 1: Check if we're being redirected back to the main app domain
        const isReturnToApp = url.origin === configUrl.origin || 
                              url.hostname === configUrl.hostname ||
                              navigationUrl.includes(configUrl.hostname);
        
        // Strategy 2: Check for OAuth success indicators in URL
        const hasSuccessParams = navigationUrl.includes('code=') || 
                                navigationUrl.includes('access_token=') ||
                                navigationUrl.includes('id_token=') ||
                                navigationUrl.includes('state=');
        
        // Strategy 3: Check for common OAuth callback paths
        const isCallbackPath = url.pathname.includes('/callback') ||
                              url.pathname.includes('/auth') ||
                              url.pathname.includes('/oauth') ||
                              url.pathname.includes('/login');
        
        if (isReturnToApp || hasSuccessParams || isCallbackPath) {
          console.log('✅ OAuth success detected! Copying session...', {
            isReturnToApp,
            hasSuccessParams,
            isCallbackPath,
            url: navigationUrl
          });
          
          // Copy cookies from OAuth window to main window
          oauthWindow.webContents.session.cookies.get({})
            .then((cookies) => {
              console.log(`📄 Found ${cookies.length} cookies to transfer`);
              
              // Filter cookies to only transfer relevant ones for our app
              const relevantCookies = cookies.filter(cookie => {
                const targetDomain = new URL(config.url).hostname;
                const cookieDomain = cookie.domain || '';
                
                // Only transfer cookies for our app's domain or session cookies
                const isAppCookie = cookieDomain.includes(targetDomain) || 
                                   cookieDomain === targetDomain ||
                                   cookie.name.toLowerCase().includes('session') ||
                                   cookie.name.toLowerCase().includes('notificator');
                
                if (isAppCookie) {
                  console.log('🍪 Transferring relevant cookie:', cookie.name, 'for domain:', cookieDomain);
                }
                
                return isAppCookie;
              });
              
              console.log(`📄 Filtered to ${relevantCookies.length} relevant cookies from ${cookies.length} total`);
              
              const cookiePromises = relevantCookies.map(cookie => {
                // More flexible cookie setting
                const cookieOptions = {
                  url: config.url,
                  name: cookie.name,
                  value: cookie.value,
                  path: cookie.path || '/',
                  secure: cookie.secure,
                  httpOnly: cookie.httpOnly
                };
                
                // Handle domain properly - only for our app's domain
                const targetDomain = new URL(config.url).hostname;
                if (cookie.domain && (cookie.domain.includes(targetDomain) || cookie.domain === targetDomain)) {
                  cookieOptions.domain = cookie.domain.startsWith('.') ? 
                    cookie.domain.substring(1) : cookie.domain;
                } else {
                  // Don't set domain for cross-domain cookies, let browser handle it
                  delete cookieOptions.domain;
                }
                
                // Handle expiration
                if (cookie.expirationDate) {
                  cookieOptions.expirationDate = cookie.expirationDate;
                }
                
                console.log('🍪 Setting cookie:', cookieOptions);
                return mainWindow.webContents.session.cookies.set(cookieOptions);
              });
              
              return Promise.all(cookiePromises);
            })
            .then(() => {
              console.log('✅ All cookies transferred successfully');
              
              // Close OAuth window and navigate to dashboard
              if (oauthWindow) {
                oauthWindow.close();
                oauthWindow = null;
              }
              
              // Navigate to dashboard instead of just reloading
              const dashboardUrl = `${config.url}/dashboard`;
              console.log('🏠 Navigating to dashboard:', dashboardUrl);
              mainWindow.loadURL(dashboardUrl);
              
              // Save session after successful OAuth
              setTimeout(() => {
                console.log('💾 Saving session...');
                saveSession();
              }, 3000); // Give more time for page to load
              
              resolve(true);
            })
            .catch((error) => {
              console.error('❌ Error copying cookies:', error);
              reject(error);
            });
        }
      } catch (error) {
        console.error('❌ Error in OAuth navigation handler:', error);
      }
    });

    // Also listen for page loads in OAuth window (additional detection)
    oauthWindow.webContents.on('did-finish-load', () => {
      const currentUrl = oauthWindow.webContents.getURL();
      console.log('📄 OAuth window finished loading:', currentUrl);
      
      // Check if we ended up on the main app after OAuth
      try {
        const url = new URL(currentUrl);
        const configUrl = new URL(config.url);
        
        if (url.hostname === configUrl.hostname) {
          console.log('✅ OAuth window loaded main app - assuming success');
          
          // Trigger the same success flow
          setTimeout(() => {
            if (oauthWindow) {
              oauthWindow.webContents.executeJavaScript('window.location.href')
                .then(finalUrl => {
                  console.log('🎯 Final OAuth URL:', finalUrl);
                  // Force success detection
                  oauthWindow.webContents.emit('will-navigate', null, finalUrl);
                });
            }
          }, 1000);
        }
      } catch (error) {
        console.error('Error checking OAuth success on page load:', error);
      }
    });

    // Handle window closed
    oauthWindow.on('closed', () => {
      oauthWindow = null;
      reject(new Error('OAuth window was closed'));
    });

    // Handle navigation errors
    oauthWindow.webContents.on('did-fail-load', (event, errorCode, errorDescription) => {
      if (errorCode !== -3) { // Ignore aborted requests
        console.error('OAuth window failed to load:', errorDescription);
        reject(new Error(`OAuth failed to load: ${errorDescription}`));
      }
    });

    // Set timeout for OAuth process
    setTimeout(() => {
      if (oauthWindow) {
        oauthWindow.close();
        oauthWindow = null;
        reject(new Error('OAuth timeout'));
      }
    }, 5 * 60 * 1000); // 5 minute timeout
  });
}

// Detect and intercept OAuth login attempts
function setupOAuthInterception() {
  console.log('Setting up OAuth interception...');
  
  // Handle ALL new window requests
  mainWindow.webContents.setWindowOpenHandler(({ url }) => {
    console.log('🚀 New window requested:', url);
    
    // Always handle OAuth URLs in-app
    if (isOAuthUrl(url)) {
      console.log('🔐 Intercepting OAuth URL:', url);
      
      handleOAuthLogin(url)
        .then(() => {
          console.log('✅ OAuth login successful');
        })
        .catch((error) => {
          console.error('❌ OAuth login failed:', error);
          dialog.showMessageBox(mainWindow, {
            type: 'error',
            title: 'Login Failed',
            message: 'Authentication failed',
            detail: error.message || 'Please try again or check your internet connection.',
            buttons: ['OK']
          });
        });
      
      return { action: 'deny' }; // Prevent default popup
    }
    
    // For other external URLs, open in browser
    console.log('🌐 Opening external URL in browser:', url);
    shell.openExternal(url);
    return { action: 'deny' };
  });

  // Intercept ALL navigation attempts
  mainWindow.webContents.on('will-navigate', (event, navigationUrl) => {
    console.log('🧭 Navigation requested:', navigationUrl);
    
    try {
      const currentUrl = new URL(config.url);
      const targetUrl = new URL(navigationUrl);
      
      // If navigating away from our domain
      if (targetUrl.origin !== currentUrl.origin) {
        console.log('🔄 External navigation detected');
        
        // If it's OAuth, handle it in-app
        if (isOAuthUrl(navigationUrl)) {
          console.log('🔐 Intercepting OAuth navigation:', navigationUrl);
          event.preventDefault();
          
          handleOAuthLogin(navigationUrl)
            .then(() => {
              console.log('✅ OAuth navigation successful');
            })
            .catch((error) => {
              console.error('❌ OAuth navigation failed:', error);
              dialog.showMessageBox(mainWindow, {
                type: 'error',
                title: 'Login Failed',
                message: 'Authentication failed',
                detail: error.message || 'Please try again.',
                buttons: ['OK']
              });
            });
        } else {
          // For non-OAuth external URLs, open in browser
          event.preventDefault();
          console.log('🌐 Opening external navigation in browser:', navigationUrl);
          shell.openExternal(navigationUrl);
        }
      }
    } catch (error) {
      console.error('Error handling navigation:', error);
    }
  });
  
  // Inject DOM interceptors on ready (optional - navigation interception is primary)
  mainWindow.webContents.on('dom-ready', () => {
    console.log('📄 DOM ready, injecting OAuth interceptors and notification polyfill...');
    // Use setTimeout to avoid blocking the main thread
    setTimeout(() => {
      injectOAuthDOMInterceptors();
      injectNotificationPolyfill();
      setupNotificationEventListener();
    }, 100);
  });
  
  // Re-inject interceptors on every page load (with delay)
  mainWindow.webContents.on('did-finish-load', () => {
    console.log('📄 Page finished loading, re-injecting DOM interceptors and notification polyfill...');
    // Use setTimeout to avoid blocking page load
    setTimeout(() => {
      injectOAuthDOMInterceptors();
      injectNotificationPolyfill();
      setupNotificationEventListener();
    }, 500);
  });
}

// Inject OAuth DOM interceptors safely - Ultra simplified version
function injectOAuthDOMInterceptors() {
  // Ultra simple script that should work in any context
  const injectionScript = `
    (function() {
      try {
        console.log('🔧 Installing simple OAuth interceptors...');
        
        // Skip if already installed
        if (window._oauthInstalled) {
          console.log('✅ OAuth interceptors already installed');
          return;
        }
        
        // Very simple click handler
        document.addEventListener('click', function(e) {
          try {
            var target = e.target;
            var element = target.closest ? target.closest('button') : target;
            
            if (element && element.textContent) {
              var text = element.textContent.toLowerCase();
              
              if (text.indexOf('google') !== -1 && text.indexOf('continue') !== -1) {
                console.log('🔐 Google OAuth button detected');
                // Don't preventDefault here - let navigation interception handle it
              }
            }
          } catch (err) {
            console.log('OAuth click handler error:', err);
          }
        }, true);
        
        // Simple window.open override
        if (window.open && !window._origOpen) {
          window._origOpen = window.open;
          window.open = function(url) {
            console.log('window.open called:', url);
            if (url && url.indexOf('oauth') !== -1) {
              console.log('OAuth window.open detected');
            }
            return window._origOpen.apply(this, arguments);
          };
        }
        
        window._oauthInstalled = true;
        console.log('✅ Simple OAuth interceptors installed');
        
      } catch (err) {
        console.log('OAuth injection error:', err.message || err);
      }
    })();
  `;
  
  // Execute with comprehensive error handling
  if (mainWindow && mainWindow.webContents) {
    mainWindow.webContents.executeJavaScript(injectionScript)
      .then(() => {
        console.log('✅ Simple OAuth interceptors injected successfully');
      })
      .catch((error) => {
        console.error('❌ Still failed to inject OAuth interceptors:', error.message || error);
        // At this point, navigation interception still works, so OAuth functionality is preserved
      });
  }
}

// Setup notification event listener to catch custom events from web content
function setupNotificationEventListener() {
  // Inject a script that forwards custom notification events to console
  const listenerScript = `
    (function() {
      // Listen for custom notification requests
      window.addEventListener('electron-notification-request', (event) => {
        console.log('🔔 Notification request intercepted:', event.detail);
        
        // Send to main process via a different method
        // Since we can't use IPC directly, we'll use a special console message
        console.log('ELECTRON_NOTIFICATION_REQUEST:' + JSON.stringify(event.detail));
      });
    })();
  `;
  
  if (mainWindow && mainWindow.webContents) {
    mainWindow.webContents.executeJavaScript(listenerScript)
      .then(() => {
        console.log('✅ Notification event listener setup complete');
      })
      .catch((error) => {
        console.error('❌ Failed to setup notification event listener:', error);
      });
  }
  
  // Listen for console messages that contain notification requests
  mainWindow.webContents.on('console-message', (event, level, message, line, sourceId) => {
    if (message.startsWith('ELECTRON_NOTIFICATION_REQUEST:')) {
      try {
        const notificationData = JSON.parse(message.replace('ELECTRON_NOTIFICATION_REQUEST:', ''));
        console.log('📢 Processing notification from console message:', notificationData);
        
        // Call our notification handler directly
        handleNotificationRequest(notificationData);
      } catch (error) {
        console.error('Failed to parse notification request from console:', error);
      }
    }
  });
}

// Handle notification requests (extracted from IPC handler)
async function handleNotificationRequest(options) {
  try {
    // Check if notifications are supported on this platform
    if (!Notification.isSupported()) {
      console.warn('Notifications are not supported on this platform');
      return null;
    }
    
    // Generate unique ID for this notification
    const notificationId = `notif-${++notificationIdCounter}`;
    
    // Create notification options
    const notificationOptions = {
      title: options.title || 'Notificator Alert',
      body: options.body || '',
      silent: options.silent || false,
      urgency: options.requireInteraction ? 'critical' : 'normal'
    };
    
    // Add icon if available
    if (options.icon) {
      try {
        // If icon is a URL path, try to load it
        if (options.icon.startsWith('/')) {
          const iconPath = path.join(__dirname, 'assets', 'icon.png');
          if (fs.existsSync(iconPath)) {
            notificationOptions.icon = nativeImage.createFromPath(iconPath);
          }
        }
      } catch (error) {
        console.warn('Failed to load notification icon:', error);
      }
    }
    
    // Create the notification
    const notification = new Notification(notificationOptions);
    
    // Store notification reference
    activeNotifications.set(notificationId, {
      notification: notification,
      options: options,
      timestamp: Date.now()
    });
    
    // Handle notification click
    notification.on('click', () => {
      console.log('Notification clicked:', notificationId);
      
      // Bring window to front
      if (mainWindow) {
        if (mainWindow.isMinimized()) {
          mainWindow.restore();
        }
        mainWindow.show();
        mainWindow.focus();
        
        // Send click event to renderer via custom event
        const clickScript = `
          window.dispatchEvent(new CustomEvent('electron-notification-click', {
            detail: { id: '${options.id || notificationId}' }
          }));
        `;
        mainWindow.webContents.executeJavaScript(clickScript);
      }
      
      // Clean up
      activeNotifications.delete(notificationId);
    });
    
    // Handle notification close
    notification.on('close', () => {
      console.log('Notification closed:', notificationId);
      activeNotifications.delete(notificationId);
    });
    
    // Show the notification
    notification.show();
    console.log('✅ Native notification shown:', notificationOptions.title);
    
    // Auto-close non-critical notifications after 10 seconds
    if (!options.requireInteraction) {
      setTimeout(() => {
        if (activeNotifications.has(notificationId)) {
          notification.close();
          activeNotifications.delete(notificationId);
        }
      }, 10000);
    }
    
    return notificationId;
  } catch (error) {
    console.error('Failed to show notification:', error);
    throw error;
  }
}

// Check if a URL is likely an OAuth URL
function isOAuthUrl(url) {
  try {
    const urlObj = new URL(url);
    const hostname = urlObj.hostname.toLowerCase();
    const pathname = urlObj.pathname.toLowerCase();
    const fullUrl = url.toLowerCase();
    
    // Common OAuth providers
    const oauthProviders = [
      'accounts.google.com',
      'github.com',
      'login.microsoftonline.com',
      'login.microsoft.com',
      'oauth.vk.com',
      'www.facebook.com',
      'api.twitter.com',
      'oauth.twitter.com',
      'auth0.com',
      'okta.com',
      'login.salesforce.com',
      'appleid.apple.com',
      'discord.com',
      'slack.com'
    ];
    
    // OAuth-related path patterns
    const oauthPaths = [
      '/oauth',
      '/auth',
      '/login',
      '/signin',
      '/sso',
      '/authorize',
      '/authentication',
      '/connect'
    ];
    
    // OAuth query parameters
    const oauthParams = [
      'response_type',
      'client_id',
      'redirect_uri',
      'scope',
      'state',
      'code_challenge',
      'oauth_token'
    ];
    
    // Check OAuth providers
    if (oauthProviders.some(provider => hostname.includes(provider))) {
      return true;
    }
    
    // Check OAuth paths
    if (oauthPaths.some(path => pathname.includes(path))) {
      return true;
    }
    
    // Check OAuth parameters
    if (oauthParams.some(param => fullUrl.includes(param))) {
      return true;
    }
    
    // Check for common OAuth keywords in the full URL
    const oauthKeywords = ['oauth', 'openid', 'saml', 'sso'];
    if (oauthKeywords.some(keyword => fullUrl.includes(keyword))) {
      return true;
    }
    
    return false;
  } catch (error) {
    console.error('Error checking OAuth URL:', error);
    return false;
  }
}

// Inject notification polyfill to bridge web notifications to Electron
function injectNotificationPolyfill() {
  const notificationScript = `
    (function() {
      try {
        console.log('🔔 Installing Electron notification bridge...');
        
        // Save the original Notification API (if needed for fallback)
        const OriginalNotification = window.Notification;
        
        // Create unique ID generator
        let notificationCounter = 0;
        const pendingNotifications = new Map();
        
        // Create our custom Notification class
        class ElectronNotification {
          constructor(title, options = {}) {
            this.title = title;
            this.options = options;
            this.onclick = null;
            this.onclose = null;
            this.onerror = null;
            this.onshow = null;
            this._id = 'web-notif-' + (++notificationCounter);
            
            // Store this notification instance
            pendingNotifications.set(this._id, this);
            
            // Create notification data
            const notificationData = {
              id: this._id,
              title: title,
              body: options.body || '',
              icon: options.icon || '',
              tag: options.tag || '',
              requireInteraction: options.requireInteraction || false,
              silent: options.silent || false,
              data: options.data || {}
            };
            
            // Try multiple communication methods
            // Method 1: Use electronAPI if available
            if (window.electronAPI && window.electronAPI.showNotification) {
              window.electronAPI.showNotification(notificationData)
                .then(() => {
                  if (this.onshow) {
                    this.onshow(new Event('show'));
                  }
                })
                .catch((error) => {
                  console.error('ElectronAPI notification failed:', error);
                  this.fallbackNotification(notificationData);
                });
            } else {
              // Method 2: Use custom event with detail
              console.log('Using custom event for notification');
              this.fallbackNotification(notificationData);
            }
          }
          
          fallbackNotification(notificationData) {
            // Dispatch a custom event that can be caught by the main process
            const event = new CustomEvent('electron-notification-request', {
              detail: notificationData,
              bubbles: true,
              cancelable: true
            });
            window.dispatchEvent(event);
            
            // Also try direct IPC if available
            if (window.require) {
              try {
                const { ipcRenderer } = window.require('electron');
                ipcRenderer.invoke('show-notification', notificationData);
              } catch (e) {
                console.log('Direct IPC not available');
              }
            }
            
            // Fire onshow event optimistically
            if (this.onshow) {
              setTimeout(() => this.onshow(new Event('show')), 100);
            }
          }
          
          close() {
            if (window.electronAPI && window.electronAPI.closeNotification) {
              window.electronAPI.closeNotification(this._id);
            }
            pendingNotifications.delete(this._id);
            if (this.onclose) {
              this.onclose(new Event('close'));
            }
          }
          
          // Add event listener methods for compatibility
          addEventListener(type, listener) {
            if (type === 'click') this.onclick = listener;
            else if (type === 'close') this.onclose = listener;
            else if (type === 'error') this.onerror = listener;
            else if (type === 'show') this.onshow = listener;
          }
          
          removeEventListener(type, listener) {
            if (type === 'click' && this.onclick === listener) this.onclick = null;
            else if (type === 'close' && this.onclose === listener) this.onclose = null;
            else if (type === 'error' && this.onerror === listener) this.onerror = null;
            else if (type === 'show' && this.onshow === listener) this.onshow = null;
          }
        }
        
        // Listen for notification click events
        window.addEventListener('electron-notification-click', (event) => {
          const notification = pendingNotifications.get(event.detail.id);
          if (notification && notification.onclick) {
            notification.onclick(new Event('click'));
          }
        });
        
        // Static properties
        ElectronNotification.permission = 'granted';
        ElectronNotification.maxActions = 2;
        
        // Static methods
        ElectronNotification.requestPermission = function() {
          return Promise.resolve('granted');
        };
        
        // Replace the global Notification
        window.Notification = ElectronNotification;
        
        // Also override the permission getter
        Object.defineProperty(window.Notification, 'permission', {
          get: function() { return 'granted'; },
          configurable: true
        });
        
        console.log('✅ Electron notification bridge installed successfully');
        
      } catch (error) {
        console.error('❌ Failed to install notification bridge:', error);
      }
    })();
  `;
  
  // Execute the script
  if (mainWindow && mainWindow.webContents) {
    mainWindow.webContents.executeJavaScript(notificationScript)
      .then(() => {
        console.log('✅ Notification polyfill injected successfully');
      })
      .catch((error) => {
        console.error('❌ Failed to inject notification polyfill:', error);
      });
  }
}

// Create application menu
function createMenu() {
  const template = [
    {
      label: 'File',
      submenu: [
        {
          label: 'Settings...',
          accelerator: 'CmdOrCtrl+,',
          click: showSettings
        },
        { type: 'separator' },
        {
          label: 'Quit',
          accelerator: process.platform === 'darwin' ? 'Cmd+Q' : 'Ctrl+Q',
          click: () => {
            isQuitting = true;
            app.quit();
          }
        }
      ]
    },
    {
      label: 'Edit',
      submenu: [
        { role: 'undo' },
        { role: 'redo' },
        { type: 'separator' },
        { role: 'cut' },
        { role: 'copy' },
        { role: 'paste' },
        { role: 'selectall' }
      ]
    },
    {
      label: 'View',
      submenu: [
        {
          label: 'Reload',
          accelerator: 'CmdOrCtrl+R',
          click: () => {
            mainWindow.reload();
          }
        },
        {
          label: 'Force Reload',
          accelerator: 'CmdOrCtrl+Shift+R',
          click: () => {
            mainWindow.webContents.reloadIgnoringCache();
          }
        },
        { type: 'separator' },
        { role: 'resetzoom' },
        { role: 'zoomin' },
        { role: 'zoomout' },
        { type: 'separator' },
        { role: 'togglefullscreen' },
        { type: 'separator' },
        { role: 'toggledevtools' }
      ]
    },
    {
      label: 'Window',
      submenu: [
        { role: 'minimize' },
        { role: 'close' }
      ]
    },
    {
      label: 'Help',
      submenu: [
        {
          label: 'About Notificator',
          click: () => {
            dialog.showMessageBox(mainWindow, {
              type: 'info',
              title: 'About Notificator',
              message: 'Notificator Desktop Client',
              detail: `Version: ${app.getVersion()}\nElectron: ${process.versions.electron}\nNode: ${process.versions.node}`,
              buttons: ['OK']
            });
          }
        }
      ]
    }
  ];

  // macOS specific menu adjustments
  if (process.platform === 'darwin') {
    template.unshift({
      label: app.getName(),
      submenu: [
        { role: 'about' },
        { type: 'separator' },
        {
          label: 'Preferences...',
          accelerator: 'Cmd+,',
          click: showSettings
        },
        { type: 'separator' },
        { role: 'services', submenu: [] },
        { type: 'separator' },
        { role: 'hide' },
        { role: 'hideothers' },
        { role: 'unhide' },
        { type: 'separator' },
        { role: 'quit' }
      ]
    });

    // Window menu
    template[4].submenu = [
      { role: 'close' },
      { role: 'minimize' },
      { role: 'zoom' },
      { type: 'separator' },
      { role: 'front' }
    ];
  }

  const menu = Menu.buildFromTemplate(template);
  Menu.setApplicationMenu(menu);
}

// Store active notifications for click handling
const activeNotifications = new Map();
let notificationIdCounter = 0;

// Handle IPC messages
ipcMain.on('save-settings', (event, settings) => {
  config.url = settings.url;
  config.env = settings.env;
  saveConfig();
  
  // Update UI
  mainWindow.setTitle(`Notificator - ${config.env}`);
  updateTrayMenu();
  mainWindow.loadURL(config.url);
  
  event.reply('settings-saved');
});

// Handle OAuth login requests from renderer
ipcMain.handle('handle-oauth-login', async (event, oauthUrl) => {
  try {
    const result = await handleOAuthLogin(oauthUrl);
    mainWindow.webContents.send('oauth-success');
    return result;
  } catch (error) {
    mainWindow.webContents.send('oauth-error', error.message);
    throw error;
  }
});

// Handle notification display requests
ipcMain.handle('show-notification', async (event, options) => {
  try {
    // Check if notifications are supported on this platform
    if (!Notification.isSupported()) {
      console.warn('Notifications are not supported on this platform');
      return null;
    }
    
    // Generate unique ID for this notification
    const notificationId = `notif-${++notificationIdCounter}`;
    
    // Create notification options
    const notificationOptions = {
      title: options.title || 'Notificator Alert',
      body: options.body || '',
      silent: options.silent || false,
      urgency: options.requireInteraction ? 'critical' : 'normal'
    };
    
    // Add icon if available
    if (options.icon) {
      try {
        // If icon is a URL path, try to load it
        if (options.icon.startsWith('/')) {
          const iconPath = path.join(__dirname, 'assets', 'icon.png');
          if (fs.existsSync(iconPath)) {
            notificationOptions.icon = nativeImage.createFromPath(iconPath);
          }
        }
      } catch (error) {
        console.warn('Failed to load notification icon:', error);
      }
    }
    
    // Create the notification
    const notification = new Notification(notificationOptions);
    
    // Store notification reference
    activeNotifications.set(notificationId, {
      notification: notification,
      options: options,
      timestamp: Date.now()
    });
    
    // Handle notification click
    notification.on('click', () => {
      console.log('Notification clicked:', notificationId);
      
      // Bring window to front
      if (mainWindow) {
        if (mainWindow.isMinimized()) {
          mainWindow.restore();
        }
        mainWindow.show();
        mainWindow.focus();
        
        // Send click event to renderer
        mainWindow.webContents.send('notification-clicked', notificationId);
      }
      
      // Clean up
      activeNotifications.delete(notificationId);
    });
    
    // Handle notification close
    notification.on('close', () => {
      console.log('Notification closed:', notificationId);
      activeNotifications.delete(notificationId);
    });
    
    // Show the notification
    notification.show();
    
    // Auto-close non-critical notifications after 10 seconds
    if (!options.requireInteraction) {
      setTimeout(() => {
        if (activeNotifications.has(notificationId)) {
          notification.close();
          activeNotifications.delete(notificationId);
        }
      }, 10000);
    }
    
    return notificationId;
  } catch (error) {
    console.error('Failed to show notification:', error);
    throw error;
  }
});

// Handle notification close requests
ipcMain.on('close-notification', (event, notificationId) => {
  const notificationData = activeNotifications.get(notificationId);
  if (notificationData && notificationData.notification) {
    notificationData.notification.close();
    activeNotifications.delete(notificationId);
  }
});

// App event handlers
app.whenReady().then(() => {
  createWindow();
  createTray();
  createMenu();

  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) {
      createWindow();
    } else {
      mainWindow.show();
    }
  });
});

app.on('before-quit', () => {
  isQuitting = true;
  // Save session before quitting
  if (mainWindow) {
    saveSession();
  }
});

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') {
    app.quit();
  }
});

// Auto-updater (if you want to implement it later)
// const { autoUpdater } = require('electron-updater');
// app.on('ready', () => {
//   autoUpdater.checkForUpdatesAndNotify();
// });