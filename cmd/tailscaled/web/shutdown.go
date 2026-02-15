// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

package web

// ShutdownSystem shuts down the system
// force=true: force immediate shutdown
// force=false: graceful shutdown
// Platform-specific implementations in shutdown_*.go files
func ShutdownSystem(force bool) error {
	return shutdownSystem(force)
}
