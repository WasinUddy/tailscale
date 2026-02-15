// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package web

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func getSystemMetrics() (*SystemMetrics, error) {
	metrics := &SystemMetrics{}
	
	// Get CPU usage
	cpu, err := getCPUUsage()
	if err == nil {
		metrics.CPUPercent = cpu
	}
	
	// Get memory usage
	memUsed, memTotal, err := getMemoryUsage()
	if err == nil {
		metrics.MemoryUsed = memUsed
		metrics.MemoryTotal = memTotal
		if memTotal > 0 {
			metrics.MemoryPercent = float64(memUsed) / float64(memTotal) * 100
		}
	}
	
	// Get disk usage
	diskUsed, diskTotal, err := getDiskUsage()
	if err == nil {
		metrics.DiskUsed = diskUsed
		metrics.DiskTotal = diskTotal
		if diskTotal > 0 {
			metrics.DiskPercent = float64(diskUsed) / float64(diskTotal) * 100
		}
	}
	
	// Get network stats
	sent, recv, err := getNetworkStats()
	if err == nil {
		metrics.NetworkBytesSent = sent
		metrics.NetworkBytesRecv = recv
	}
	
	// Get uptime
	uptime, err := getUptime()
	if err == nil {
		metrics.UptimeSeconds = uptime
	}
	
	return metrics, nil
}

func getCPUUsage() (float64, error) {
	// Use top command to get CPU usage
	cmd := exec.Command("top", "-l", "2", "-n", "0", "-stats", "cpu")
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "CPU usage") {
			// Parse line like: "CPU usage: 5.40% user, 3.85% sys, 90.73% idle"
			parts := strings.Split(line, ",")
			if len(parts) > 2 {
				idlePart := strings.TrimSpace(parts[2])
				idleStr := strings.TrimSuffix(strings.TrimPrefix(idlePart, " "), "% idle")
				idleStr = strings.TrimSpace(idleStr)
				if idle, err := strconv.ParseFloat(idleStr, 64); err == nil {
					return 100 - idle, nil
				}
			}
		}
	}
	return 0, fmt.Errorf("could not parse CPU usage")
}

func getMemoryUsage() (used uint64, total uint64, err error) {
	// Get total memory
	cmd := exec.Command("sysctl", "-n", "hw.memsize")
	output, err := cmd.Output()
	if err != nil {
		return 0, 0, err
	}
	total, err = strconv.ParseUint(strings.TrimSpace(string(output)), 10, 64)
	if err != nil {
		return 0, 0, err
	}
	
	// Get memory stats using vm_stat
	cmd = exec.Command("vm_stat")
	output, err = cmd.Output()
	if err != nil {
		return 0, 0, err
	}
	
	var pageSize uint64 = 4096 // Default page size
	var active, inactive, speculative, wired uint64
	
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "page size of") {
			parts := strings.Fields(line)
			if len(parts) >= 8 {
				if ps, err := strconv.ParseUint(parts[7], 10, 64); err == nil {
					pageSize = ps
				}
			}
		} else if strings.HasPrefix(line, "Pages active:") {
			active = parseVMStatValue(line)
		} else if strings.HasPrefix(line, "Pages inactive:") {
			inactive = parseVMStatValue(line)
		} else if strings.HasPrefix(line, "Pages speculative:") {
			speculative = parseVMStatValue(line)
		} else if strings.HasPrefix(line, "Pages wired down:") {
			wired = parseVMStatValue(line)
		}
	}
	
	// Calculate used memory (active + wired + inactive - speculative)
	used = (active + wired + inactive - speculative) * pageSize
	
	return used, total, nil
}

func parseVMStatValue(line string) uint64 {
	parts := strings.Fields(line)
	if len(parts) >= 2 {
		valStr := strings.TrimSuffix(parts[len(parts)-1], ".")
		if val, err := strconv.ParseUint(valStr, 10, 64); err == nil {
			return val
		}
	}
	return 0
}

func getDiskUsage() (used uint64, total uint64, err error) {
	var stat syscall.Statfs_t
	err = syscall.Statfs("/", &stat)
	if err != nil {
		return 0, 0, err
	}
	
	total = stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	used = total - free
	
	return used, total, nil
}

func getNetworkStats() (sent uint64, recv uint64, err error) {
	// Use netstat to get network stats
	cmd := exec.Command("netstat", "-ibn")
	output, err := cmd.Output()
	if err != nil {
		return 0, 0, err
	}
	
	lines := strings.Split(string(output), "\n")
	for i, line := range lines {
		if i == 0 {
			continue // Skip header
		}
		fields := strings.Fields(line)
		if len(fields) >= 10 {
			// Skip loopback
			if len(fields) > 0 && strings.HasPrefix(fields[0], "lo") {
				continue
			}
			// Column 7 is Ibytes (received), column 10 is Obytes (sent)
			if r, err := strconv.ParseUint(fields[6], 10, 64); err == nil {
				recv += r
			}
			if s, err := strconv.ParseUint(fields[9], 10, 64); err == nil {
				sent += s
			}
		}
	}
	
	return sent, recv, nil
}

func getUptime() (uint64, error) {
	// Use sysctl to get boot time
	cmd := exec.Command("sysctl", "-n", "kern.boottime")
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	
	// Parse output like: { sec = 1707948123, usec = 0 } Thu Feb 15 12:15:23 2026
	line := string(output)
	if strings.Contains(line, "sec = ") {
		start := strings.Index(line, "sec = ") + 6
		end := strings.Index(line[start:], ",")
		if end > 0 {
			bootTimeStr := line[start : start+end]
			if bootTime, err := strconv.ParseInt(bootTimeStr, 10, 64); err == nil {
				uptime := time.Now().Unix() - bootTime
				return uint64(uptime), nil
			}
		}
	}
	
	return 0, fmt.Errorf("could not parse uptime")
}
