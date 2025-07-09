package audio

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// DeviceSoundPlayer implements SoundPlayer with audio device selection support
type DeviceSoundPlayer struct {
	deviceManager  *AudioDeviceManager
	selectedDevice string
}

// NewDeviceSoundPlayer creates a new device-aware sound player
func NewDeviceSoundPlayer(deviceID string) *DeviceSoundPlayer {
	return &DeviceSoundPlayer{
		deviceManager:  NewAudioDeviceManager(),
		selectedDevice: deviceID,
	}
}

// SetDevice sets the audio output device
func (dsp *DeviceSoundPlayer) SetDevice(deviceID string) {
	dsp.selectedDevice = deviceID
}

// GetDevice returns the current audio output device
func (dsp *DeviceSoundPlayer) GetDevice() string {
	return dsp.selectedDevice
}

// PlaySound plays a custom sound file on the selected device
func (dsp *DeviceSoundPlayer) PlaySound(soundPath string) error {
	if !fileExists(soundPath) {
		return fmt.Errorf("sound file not found: %s", soundPath)
	}

	switch runtime.GOOS {
	case "linux":
		return dsp.playLinuxSoundFile(soundPath)
	case "darwin":
		return dsp.playMacSoundFile(soundPath)
	case "windows":
		return dsp.playWindowsSoundFile(soundPath)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// PlayDefaultSound plays system default sound based on severity on the selected device
func (dsp *DeviceSoundPlayer) PlayDefaultSound(severity string) error {
	switch runtime.GOOS {
	case "linux":
		return dsp.playLinuxDefaultSound(severity)
	case "darwin":
		return dsp.playMacDefaultSound(severity)
	case "windows":
		return dsp.playWindowsDefaultSound(severity)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// Linux-specific implementations
func (dsp *DeviceSoundPlayer) playLinuxSoundFile(soundPath string) error {
	if dsp.selectedDevice == "default" || dsp.selectedDevice == "" {
		// Use default device
		return exec.Command("paplay", soundPath).Run()
	}

	// Try to play on specific PulseAudio device
	if strings.Contains(dsp.selectedDevice, "alsa_output") || strings.Contains(dsp.selectedDevice, "pulse") {
		cmd := exec.Command("paplay", "--device", dsp.selectedDevice, soundPath)
		if err := cmd.Run(); err == nil {
			return nil
		}
		log.Printf("Failed to play on device %s, falling back to default", dsp.selectedDevice)
	}

	// Try ALSA device
	if strings.HasPrefix(dsp.selectedDevice, "hw:") {
		cmd := exec.Command("aplay", "-D", dsp.selectedDevice, soundPath)
		if err := cmd.Run(); err == nil {
			return nil
		}
		log.Printf("Failed to play on ALSA device %s, falling back to default", dsp.selectedDevice)
	}

	// Fallback to default
	return exec.Command("paplay", soundPath).Run()
}

func (dsp *DeviceSoundPlayer) playLinuxDefaultSound(severity string) error {
	soundName := "suspend-error"
	switch severity {
	case "critical":
		soundName = "suspend-error"
	case "warning":
		soundName = "dialog-warning"
	}

	soundPath := "/usr/share/sounds/freedesktop/stereo/" + soundName + ".oga"

	if dsp.selectedDevice == "default" || dsp.selectedDevice == "" {
		// Try multiple Linux sound systems with default device
		commands := [][]string{
			{"paplay", soundPath},
			{"aplay", "/usr/share/sounds/alsa/Front_Left.wav"},
			{"speaker-test", "-t", "sine", "-f", "1000", "-l", "1"},
		}

		for _, cmd := range commands {
			if err := exec.Command(cmd[0], cmd[1:]...).Run(); err == nil {
				return nil
			}
		}
	} else {
		// Try to play on specific device
		if fileExists(soundPath) {
			return dsp.playLinuxSoundFile(soundPath)
		}
	}

	// Fallback: terminal bell
	fmt.Print("\a")
	return nil
}

// macOS-specific implementations
func (dsp *DeviceSoundPlayer) playMacSoundFile(soundPath string) error {
	if dsp.selectedDevice == "default" || dsp.selectedDevice == "" {
		return exec.Command("afplay", soundPath).Run()
	}

	// For macOS, we can try to use specific audio devices with afplay
	// This is a simplified implementation - full device selection would require AudioUnit APIs
	switch dsp.selectedDevice {
	case "builtin":
		// Force built-in output (this is a simplified approach)
		return exec.Command("afplay", soundPath).Run()
	case "usb", "bluetooth":
		// For USB/Bluetooth, we still use afplay but log the device preference
		log.Printf("Playing sound with preference for %s device", dsp.selectedDevice)
		return exec.Command("afplay", soundPath).Run()
	default:
		return exec.Command("afplay", soundPath).Run()
	}
}

func (dsp *DeviceSoundPlayer) playMacDefaultSound(severity string) error {
	// Use different beep patterns for different severities
	switch severity {
	case "critical":
		return exec.Command("osascript", "-e", "beep 3").Run()
	case "warning":
		return exec.Command("osascript", "-e", "beep 2").Run()
	default:
		return exec.Command("osascript", "-e", "beep 1").Run()
	}
}

// Windows-specific implementations
func (dsp *DeviceSoundPlayer) playWindowsSoundFile(soundPath string) error {
	if dsp.selectedDevice == "default" || dsp.selectedDevice == "" {
		return exec.Command("powershell", "-c",
			fmt.Sprintf("(New-Object Media.SoundPlayer '%s').PlaySync();", soundPath)).Run()
	}

	// For Windows, device selection with PowerShell is complex
	// This is a simplified implementation
	log.Printf("Playing sound with preference for device: %s", dsp.selectedDevice)
	return exec.Command("powershell", "-c",
		fmt.Sprintf("(New-Object Media.SoundPlayer '%s').PlaySync();", soundPath)).Run()
}

func (dsp *DeviceSoundPlayer) playWindowsDefaultSound(severity string) error {
	// Use different beep frequencies for different severities
	switch severity {
	case "critical":
		return exec.Command("powershell", "-c", "[console]::beep(1000,500)").Run()
	case "warning":
		return exec.Command("powershell", "-c", "[console]::beep(800,300)").Run()
	default:
		return exec.Command("powershell", "-c", "[console]::beep(600,200)").Run()
	}
}

// Utility function
func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return !os.IsNotExist(err)
}
