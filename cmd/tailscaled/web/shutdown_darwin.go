// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package web

import (
	"os/exec"
	"time"
)

func shutdownSystem(force bool) error {
	// Give a small delay to allow HTTP response to be sent
	time.Sleep(100 * time.Millisecond)

	if force {
		// Force immediate shutdown
		cmd := exec.Command("sudo", "shutdown", "-h", "now")
		return cmd.Run()
	}

	// Graceful shutdown with 1 minute delay
	cmd := exec.Command("sudo", "shutdown", "-h", "+1")
	return cmd.Run()
}
