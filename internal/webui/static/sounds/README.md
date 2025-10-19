# Notification Sounds

This directory contains notification sound files for different alert severities.

## Required Files

The following sound files are required:

- `critical.mp3` - Urgent notification sound (higher pitch, faster tempo)
- `warning.mp3` - Moderate notification sound (medium pitch)
- `info.mp3` - Gentle notification sound (lower pitch, softer)

## Sound Specifications

- **Format**: MP3 (best browser compatibility)
- **Duration**: 0.5-2 seconds (short and non-intrusive)
- **Volume**: Normalized to -3dB to -6dB
- **Sample Rate**: 44.1kHz
- **Bit Rate**: 128-192 kbps

## Sourcing Sounds

### Free Sound Resources

1. **FreeSound.org** (https://freesound.org/)
   - Search for "notification", "alert", "beep"
   - Filter by CC0 license (public domain)

2. **NotificationSounds.com** (https://notificationsounds.com/)
   - Browse notification categories
   - Download MP3 format

3. **Zapsplat** (https://www.zapsplat.com/)
   - Free sound effects
   - Attribution required for free tier

### Recommended Sound Characteristics

**Critical (`critical.mp3`)**:
- Sharp, attention-grabbing
- 800-1200Hz frequency range
- Quick attack, short duration
- Example search: "urgent alert", "critical beep", "emergency notification"

**Warning (`warning.mp3`)**:
- Moderate urgency
- 600-900Hz frequency range
- Balanced tone
- Example search: "warning beep", "caution alert"

**Info (`info.mp3`)**:
- Gentle, subtle
- 400-600Hz frequency range
- Soft tone, pleasant
- Example search: "notification", "subtle beep", "info ping"

## Testing Sounds

After adding sound files, test them in the browser:
1. Open Dashboard Settings → Notifications tab
2. Click "Test Notification" button
3. Verify sounds play for each severity level

## Browser Compatibility

All modern browsers support MP3 playback. The app includes fallback handling for:
- Browsers that block autoplay (user must interact with page first)
- Missing sound files (fails gracefully without errors)
