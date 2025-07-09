package audio

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// AudioDevice represents an audio output device
type AudioDevice struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IsDefault   bool   `json:"is_default"`
}

// AudioDeviceManager handles audio device enumeration and selection
type AudioDeviceManager struct{}

// NewAudioDeviceManager creates a new audio device manager
func NewAudioDeviceManager() *AudioDeviceManager {
	return &AudioDeviceManager{}
}

// GetAvailableDevices returns a list of available audio output devices
func (adm *AudioDeviceManager) GetAvailableDevices() ([]AudioDevice, error) {
	switch runtime.GOOS {
	case "linux":
		return adm.getLinuxDevices()
	case "darwin":
		return adm.getMacDevices()
	case "windows":
		return adm.getWindowsDevices()
	default:
		return []AudioDevice{
			{ID: "default", Name: "System Default", Description: "Default system audio device", IsDefault: true},
		}, nil
	}
}

// getLinuxDevices gets audio devices on Linux using PulseAudio
func (adm *AudioDeviceManager) getLinuxDevices() ([]AudioDevice, error) {
	devices := []AudioDevice{
		{ID: "default", Name: "System Default", Description: "Default system audio device", IsDefault: true},
	}

	// First, get the basic device list to get device IDs
	shortCmd := exec.Command("pactl", "list", "short", "sinks")
	shortOutput, err := shortCmd.Output()
	if err != nil {
		// Fallback to ALSA devices if PulseAudio is not available
		return adm.getAlsaDevices()
	}

	// Get detailed device information for better names
	detailCmd := exec.Command("pactl", "list", "sinks")
	detailOutput, err := detailCmd.Output()
	if err != nil {
		// Fallback to simple parsing if detailed info fails
		return adm.parseSimpleDevices(shortOutput)
	}

	// Parse detailed output to extract device info
	deviceInfo := adm.parseDetailedDeviceInfo(string(detailOutput))

	// Parse short output to get device IDs and match with detailed info
	lines := strings.Split(string(shortOutput), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) >= 2 {
			id := parts[1]

			// Look up detailed info for this device
			if info, exists := deviceInfo[id]; exists {
				devices = append(devices, AudioDevice{
					ID:          id,
					Name:        info.Name,
					Description: info.Description,
					IsDefault:   false,
				})
			} else {
				// Fallback to simple name extraction
				name := adm.extractSimpleName(id)
				devices = append(devices, AudioDevice{
					ID:          id,
					Name:        name,
					Description: fmt.Sprintf("PulseAudio device: %s", id),
					IsDefault:   false,
				})
			}
		}
	}

	return devices, nil
}

// DeviceInfo holds parsed device information
type DeviceInfo struct {
	Name        string
	Description string
	ProductName string
	ProfileDesc string
}

// parseDetailedDeviceInfo parses the output of 'pactl list sinks' to extract device information
func (adm *AudioDeviceManager) parseDetailedDeviceInfo(output string) map[string]DeviceInfo {
	deviceInfo := make(map[string]DeviceInfo)

	// Split into individual sink entries
	sinks := strings.Split(output, "Sink #")

	for _, sink := range sinks {
		if strings.TrimSpace(sink) == "" {
			continue
		}

		var info DeviceInfo
		var deviceName string

		lines := strings.Split(sink, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)

			// Extract device name from the Name: line
			if strings.HasPrefix(line, "Name: ") {
				deviceName = strings.TrimPrefix(line, "Name: ")
			}

			// Extract product name
			if strings.Contains(line, "device.product.name = ") {
				productName := strings.Split(line, "device.product.name = ")[1]
				productName = strings.Trim(productName, `"`)
				info.ProductName = productName
			}

			// Extract profile description
			if strings.Contains(line, "device.profile.description = ") {
				profileDesc := strings.Split(line, "device.profile.description = ")[1]
				profileDesc = strings.Trim(profileDesc, `"`)
				info.ProfileDesc = profileDesc
			}
		}

		if deviceName != "" {
			// Create user-friendly name
			name := adm.createFriendlyName(info.ProductName, info.ProfileDesc)
			info.Name = name
			info.Description = fmt.Sprintf("PulseAudio device: %s", deviceName)
			deviceInfo[deviceName] = info
		}
	}

	return deviceInfo
}

// createFriendlyName creates a user-friendly device name from product name and profile description
func (adm *AudioDeviceManager) createFriendlyName(productName, profileDesc string) string {
	// If we have both product name and profile description
	if productName != "" && profileDesc != "" {
		// For HDMI devices, use the profile description as it's more descriptive
		if strings.Contains(strings.ToLower(profileDesc), "hdmi") ||
			strings.Contains(strings.ToLower(profileDesc), "displayport") {
			return profileDesc
		}

		// For other devices, combine product name with profile description
		if profileDesc == "Analog Stereo" {
			return productName
		}

		return fmt.Sprintf("%s (%s)", productName, profileDesc)
	}

	// Fallback to what we have
	if productName != "" {
		return productName
	}
	if profileDesc != "" {
		return profileDesc
	}

	return "Unknown Audio Device"
}

// parseSimpleDevices fallback method for simple device parsing
func (adm *AudioDeviceManager) parseSimpleDevices(output []byte) ([]AudioDevice, error) {
	devices := []AudioDevice{
		{ID: "default", Name: "System Default", Description: "Default system audio device", IsDefault: true},
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) >= 2 {
			id := parts[1]
			name := adm.extractSimpleName(id)

			devices = append(devices, AudioDevice{
				ID:          id,
				Name:        name,
				Description: fmt.Sprintf("PulseAudio device: %s", id),
				IsDefault:   false,
			})
		}
	}

	return devices, nil
}

// extractSimpleName extracts a simple name from device ID (fallback method)
func (adm *AudioDeviceManager) extractSimpleName(id string) string {
	name := id

	// Extract meaningful name from PipeWire/PulseAudio device IDs
	if strings.Contains(id, "usb-") {
		// For USB devices, extract the device name
		if usbParts := strings.Split(id, "usb-"); len(usbParts) > 1 {
			usbName := strings.Split(usbParts[1], "-00.")[0]
			usbName = strings.ReplaceAll(usbName, "_", " ")
			name = usbName
		}
	} else if strings.Contains(id, "pci-") {
		// For built-in devices, use a generic name
		if strings.Contains(id, "platform-skl_hda_dsp_generic") {
			name = "Built-in Audio"
		} else {
			name = "Built-in Audio Device"
		}
	}

	// If we still have the raw ID as name, try to make it more readable
	if name == id {
		// Remove common prefixes and make more readable
		name = strings.ReplaceAll(name, "alsa_output.", "")
		name = strings.ReplaceAll(name, "_", " ")
		if len(name) > 50 {
			name = name[:47] + "..."
		}
	}

	return name
}

// getAlsaDevices gets ALSA audio devices as fallback for Linux
func (adm *AudioDeviceManager) getAlsaDevices() ([]AudioDevice, error) {
	devices := []AudioDevice{
		{ID: "default", Name: "System Default", Description: "Default system audio device", IsDefault: true},
	}

	cmd := exec.Command("aplay", "-l")
	output, err := cmd.Output()
	if err != nil {
		return devices, nil // Return just default if ALSA is not available
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "card ") {
			// Parse ALSA card information
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				cardInfo := strings.TrimSpace(parts[0])
				deviceInfo := strings.TrimSpace(parts[1])

				// Extract card number
				cardParts := strings.Fields(cardInfo)
				if len(cardParts) >= 2 {
					cardNum := cardParts[1]
					id := fmt.Sprintf("hw:%s,0", cardNum)
					name := deviceInfo

					devices = append(devices, AudioDevice{
						ID:          id,
						Name:        name,
						Description: fmt.Sprintf("ALSA device: %s", name),
						IsDefault:   false,
					})
				}
			}
		}
	}

	return devices, nil
}

// getMacDevices gets audio devices on macOS
func (adm *AudioDeviceManager) getMacDevices() ([]AudioDevice, error) {
	devices := []AudioDevice{
		{ID: "default", Name: "System Default", Description: "Default system audio device", IsDefault: true},
	}

	// Use system_profiler to get audio devices
	cmd := exec.Command("system_profiler", "SPAudioDataType", "-json")
	_, err := cmd.Output()
	if err != nil {
		// Fallback: try to get basic device info using osascript
		return adm.getMacDevicesOsascript()
	}

	// For now, just return default + a few common device types
	// Full JSON parsing would require more complex logic
	devices = append(devices, []AudioDevice{
		{ID: "builtin", Name: "Built-in Output", Description: "Built-in speakers/headphones", IsDefault: false},
		{ID: "usb", Name: "USB Audio", Description: "USB audio devices", IsDefault: false},
		{ID: "bluetooth", Name: "Bluetooth Audio", Description: "Bluetooth audio devices", IsDefault: false},
	}...)

	return devices, nil
}

// getMacDevicesOsascript gets Mac devices using osascript as fallback
func (adm *AudioDeviceManager) getMacDevicesOsascript() ([]AudioDevice, error) {
	devices := []AudioDevice{
		{ID: "default", Name: "System Default", Description: "Default system audio device", IsDefault: true},
		{ID: "builtin", Name: "Built-in Output", Description: "Built-in speakers/headphones", IsDefault: false},
	}

	return devices, nil
}

// getWindowsDevices gets audio devices on Windows
func (adm *AudioDeviceManager) getWindowsDevices() ([]AudioDevice, error) {
	devices := []AudioDevice{
		{ID: "default", Name: "System Default", Description: "Default system audio device", IsDefault: true},
	}

	// Use PowerShell to get audio devices
	cmd := exec.Command("powershell", "-Command",
		"Get-WmiObject -Class Win32_SoundDevice | Select-Object Name, Description | ConvertTo-Json")
	output, err := cmd.Output()
	if err != nil {
		// Fallback to common device names
		devices = append(devices, []AudioDevice{
			{ID: "speakers", Name: "Speakers", Description: "Default speakers", IsDefault: false},
			{ID: "headphones", Name: "Headphones", Description: "Default headphones", IsDefault: false},
		}...)
		return devices, nil
	}

	// For now, return basic devices. Full JSON parsing would require more complex logic
	lines := strings.Split(string(output), "\n")
	deviceCount := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Name") && deviceCount < 5 { // Limit to 5 additional devices
			deviceCount++
			devices = append(devices, AudioDevice{
				ID:          fmt.Sprintf("device_%d", deviceCount),
				Name:        fmt.Sprintf("Audio Device %d", deviceCount),
				Description: "Windows audio device",
				IsDefault:   false,
			})
		}
	}

	return devices, nil
}

// ValidateDevice checks if a device ID is valid
func (adm *AudioDeviceManager) ValidateDevice(deviceID string) bool {
	if deviceID == "default" {
		return true
	}

	devices, err := adm.GetAvailableDevices()
	if err != nil {
		return false
	}

	for _, device := range devices {
		if device.ID == deviceID {
			return true
		}
	}

	return false
}

// GetDeviceByID returns a device by its ID
func (adm *AudioDeviceManager) GetDeviceByID(deviceID string) (*AudioDevice, error) {
	devices, err := adm.GetAvailableDevices()
	if err != nil {
		return nil, err
	}

	for _, device := range devices {
		if device.ID == deviceID {
			return &device, nil
		}
	}

	return nil, fmt.Errorf("device not found: %s", deviceID)
}
