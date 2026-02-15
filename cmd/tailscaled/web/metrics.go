// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

package web

// SystemMetrics represents system metrics
type SystemMetrics struct {
	CPUPercent        float64
	MemoryUsed        uint64
	MemoryTotal       uint64
	MemoryPercent     float64
	DiskUsed          uint64
	DiskTotal         uint64
	DiskPercent       float64
	NetworkBytesSent  uint64
	NetworkBytesRecv  uint64
	UptimeSeconds     uint64
}

// GetSystemMetrics returns current system metrics
// Platform-specific implementations in metrics_*.go files
func GetSystemMetrics() (*SystemMetrics, error) {
	return getSystemMetrics()
}
