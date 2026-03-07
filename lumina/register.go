// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

// Package lumina provides integration with the Lumina server for
// MAC address registration and Wake-on-LAN functionality.
package lumina

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"tailscale.com/envknob"
	"tailscale.com/types/logger"
)

// Config holds configuration for Lumina server integration
type Config struct {
	// ServerURL is the Lumina server endpoint (default: http://lumina-server:80)
	ServerURL string

	// Hostname is the device hostname to register (auto-detected if empty)
	Hostname string

	// Interface is the network interface to get MAC from (auto-detected if empty)
	Interface string

	// Enabled controls whether registration should happen
	Enabled bool

	// NodeIDFunc is an optional function that returns the Tailscale stable node ID
	// (e.g. "nodeXXXXXXX"). When provided, it is used as tailscale_id in the
	// registration request so the server can correlate the record with the device
	// returned by the Tailscale API. If the function returns an empty string the
	// registration loop retries until it becomes available.
	NodeIDFunc func() string
}

// LoadConfig loads Lumina configuration from environment variables
func LoadConfig() *Config {
	serverURL := envknob.String("TS_LUMINA_SERVER")
	if serverURL == "" {
		serverURL = "http://lumina-server:80"
	}

	enabled := envknob.Bool("TS_LUMINA_ENABLED")
	if !enabled {
		// Check if server URL is set, if so, enable by default
		if envknob.String("TS_LUMINA_SERVER") != "" {
			enabled = true
		}
	}

	return &Config{
		ServerURL: serverURL,
		Hostname:  envknob.String("TS_LUMINA_HOSTNAME"),
		Interface: envknob.String("TS_LUMINA_INTERFACE"),
		Enabled:   enabled,
	}
}

// RegisterMACAddress registers this device's MAC address with the Lumina server
// It runs asynchronously and logs errors but doesn't block daemon startup
func RegisterMACAddress(logf logger.Logf, cfg *Config) {
	if cfg == nil {
		cfg = LoadConfig()
	}

	if !cfg.Enabled {
		logf("lumina: registration disabled (set TS_LUMINA_ENABLED=true or TS_LUMINA_SERVER to enable)")
		return
	}

	// Run registration in background goroutine to not block daemon startup
	go func() {
		if err := registerWithRetry(logf, cfg); err != nil {
			logf("lumina: failed to register MAC address: %v", err)
		} else {
			logf("lumina: successfully registered with server at %s", cfg.ServerURL)
		}
	}()
}

// registerWithRetry attempts to register with exponential backoff
func registerWithRetry(logf logger.Logf, cfg *Config) error {
	maxAttempts := 5
	baseDelay := 2 * time.Second

	// Get hostname (used as fallback identifier in logs)
	hostname := cfg.Hostname
	if hostname == "" {
		var err error
		hostname, err = os.Hostname()
		if err != nil {
			return fmt.Errorf("failed to get hostname: %w", err)
		}
	}

	// Get MAC address
	macAddr, iface, err := getMACAddress(cfg.Interface)
	if err != nil {
		return fmt.Errorf("failed to get MAC address: %w", err)
	}

	// Resolve the Tailscale stable node ID.
	// If NodeIDFunc is provided we poll until it returns a non-empty value
	// (the daemon may not be fully connected yet at the time of registration).
	tailscaleID := hostname // default fallback
	if cfg.NodeIDFunc != nil {
		nodeIDMaxWait := 60 * time.Second
		nodeIDPoll := 2 * time.Second
		waited := time.Duration(0)
		for {
			if id := cfg.NodeIDFunc(); id != "" {
				tailscaleID = id
				break
			}
			if waited >= nodeIDMaxWait {
				logf("lumina: timed out waiting for Tailscale node ID, falling back to hostname %s", hostname)
				break
			}
			logf("lumina: waiting for Tailscale node ID (waited %v)...", waited)
			time.Sleep(nodeIDPoll)
			waited += nodeIDPoll
		}
	}

	logf("lumina: registering tailscale_id=%s mac=%s interface=%s", tailscaleID, macAddr, iface)

	// Retry with exponential backoff
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := sendRegistration(ctx, cfg.ServerURL, tailscaleID, macAddr)
		cancel()

		if err == nil {
			return nil
		}

		lastErr = err
		if attempt < maxAttempts {
			delay := baseDelay * time.Duration(1<<uint(attempt-1)) // exponential backoff
			logf("lumina: registration attempt %d/%d failed: %v (retrying in %v)", attempt, maxAttempts, err, delay)
			time.Sleep(delay)
		}
	}

	return fmt.Errorf("failed after %d attempts: %w", maxAttempts, lastErr)
}

// getMACAddress retrieves the MAC address of the specified interface or auto-detects one
func getMACAddress(interfaceName string) (macAddr, ifaceName string, err error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", "", fmt.Errorf("failed to get network interfaces: %w", err)
	}

	// If interface name is specified, find it
	if interfaceName != "" {
		for _, iface := range interfaces {
			if iface.Name == interfaceName {
				if len(iface.HardwareAddr) == 0 {
					return "", "", fmt.Errorf("interface %s has no MAC address", interfaceName)
				}
				return iface.HardwareAddr.String(), iface.Name, nil
			}
		}
		return "", "", fmt.Errorf("interface %s not found", interfaceName)
	}

	// Auto-detect: find first suitable physical interface
	// Note: We don't require an IP address because the interface might be UP
	// but not yet have an IP assigned (e.g., during boot before DHCP completes)
	for _, iface := range interfaces {
		// Skip loopback and interfaces that are down
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		// Must have a hardware address
		if len(iface.HardwareAddr) == 0 {
			continue
		}

		// Skip virtual/container interfaces
		if isVirtualInterface(iface.HardwareAddr) {
			continue
		}

		return iface.HardwareAddr.String(), iface.Name, nil
	}

	return "", "", fmt.Errorf("no suitable network interface found")
}

// isVirtualInterface checks if a MAC address belongs to a virtual/container interface
func isVirtualInterface(mac net.HardwareAddr) bool {
	if len(mac) != 6 {
		return false
	}

	// Docker containers (02:42:xx:xx:xx:xx)
	if mac[0] == 0x02 && mac[1] == 0x42 {
		return true
	}

	// Check known virtual OUIs
	oui := [3]byte{mac[0], mac[1], mac[2]}
	virtualOUIs := map[[3]byte]bool{
		{0x00, 0x15, 0x5d}: true, // Hyper-V
		{0x00, 0x50, 0x56}: true, // VMware
		{0x00, 0x1c, 0x14}: true, // VMware
		{0x00, 0x05, 0x69}: true, // VMware
		{0x00, 0x0c, 0x29}: true, // VMware
		{0x52, 0x54, 0x00}: true, // QEMU/KVM
		{0x08, 0x00, 0x27}: true, // VirtualBox
	}

	return virtualOUIs[oui]
}

// sendRegistration sends the MAC address registration to the Lumina server
func sendRegistration(ctx context.Context, serverURL, tailscaleID, macAddr string) error {
	url := fmt.Sprintf("%s/api/devices/associate", serverURL)

	reqBody := map[string]interface{}{
		"tailscale_id": tailscaleID,
		"mac_address":  macAddr,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "tailscaled-lumina/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		var errResp map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		return fmt.Errorf("server returned status %d: %v", resp.StatusCode, errResp)
	}

	return nil
}
