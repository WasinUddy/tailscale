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

	// Get hostname
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

	logf("lumina: registering hostname=%s mac=%s interface=%s", hostname, macAddr, iface)

	// Retry with exponential backoff
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := sendRegistration(ctx, cfg.ServerURL, hostname, macAddr)
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

	// Auto-detect: prefer Tailscale interface first
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 || len(iface.HardwareAddr) == 0 {
			continue
		}

		// Prefer tailscale0 or similar interfaces
		if iface.Name == "tailscale0" || iface.Name == "utun" {
			return iface.HardwareAddr.String(), iface.Name, nil
		}
	}

	// Fall back to first suitable interface with IP address
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 || len(iface.HardwareAddr) == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil || len(addrs) == 0 {
			continue
		}

		return iface.HardwareAddr.String(), iface.Name, nil
	}

	return "", "", fmt.Errorf("no suitable network interface found")
}

// sendRegistration sends the MAC address registration to the Lumina server
func sendRegistration(ctx context.Context, serverURL, hostname, macAddr string) error {
	url := fmt.Sprintf("%s/api/devices/associate", serverURL)

	reqBody := map[string]interface{}{
		"tailscale_id": hostname,
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
