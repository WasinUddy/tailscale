// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package web

import (
	"os/exec"
	"time"
)

func shutdownSystem(force bool) error {
	// Give a small delay to allow HTTP response to be sent
	time.Sleep(100 * time.Millisecond)

	if force {
		// Force immediate shutdown with systemd
		cmd := exec.Command("systemctl", "poweroff", "-i", "--force")
		if err := cmd.Run(); err != nil {
			// Fallback to traditional forced shutdown
			cmd = exec.Command("shutdown", "-h", "now")
			return cmd.Run()
		}
		return nil
	}

	// Graceful shutdown with 1 minute delay
	cmd := exec.Command("shutdown", "-h", "+1")
	if err := cmd.Run(); err != nil {
		// Try systemctl as fallback
		cmd = exec.Command("systemctl", "poweroff")
		return cmd.Run()
	}
	return nil
}
