const { contextBridge, ipcRenderer } = require('electron');

// Expose protected methods that allow the renderer process to use
// the ipcRenderer without exposing the entire object
contextBridge.exposeInMainWorld('electronAPI', {
  // Platform information
  platform: process.platform,
  
  // Version information
  getVersion: () => ipcRenderer.invoke('get-app-version'),
  
  // Settings
  openSettings: () => ipcRenderer.send('open-settings'),
  
  // Window controls
  minimizeToTray: () => ipcRenderer.send('minimize-to-tray'),
  
  // OAuth authentication
  handleOAuthLogin: (url) => ipcRenderer.invoke('handle-oauth-login', url),
  
  // Notification APIs
  showNotification: (options) => ipcRenderer.invoke('show-notification', options),
  closeNotification: (id) => ipcRenderer.send('close-notification', id),
  onNotificationClick: (callback) => {
    ipcRenderer.on('notification-clicked', (event, id) => callback(id));
  },
  
  // Events from main process
  onAlertReceived: (callback) => {
    ipcRenderer.on('alert-received', (event, alert) => callback(alert));
  },
  
  onOAuthSuccess: (callback) => {
    ipcRenderer.on('oauth-success', () => callback());
  },
  
  onOAuthError: (callback) => {
    ipcRenderer.on('oauth-error', (event, error) => callback(error));
  },
  
  // Remove listeners
  removeAllListeners: (channel) => {
    ipcRenderer.removeAllListeners(channel);
  }
});

// Log that preload script is loaded
console.log('Notificator Electron preload script loaded');