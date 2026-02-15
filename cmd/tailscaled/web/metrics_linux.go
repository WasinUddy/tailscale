// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package web

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var lastCPUStats cpuStats

type cpuStats struct {
	user   uint64
	nice   uint64
	system uint64
	idle   uint64
	iowait uint64
	irq    uint64
	total  uint64
	time   time.Time
}

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
	file, err := os.Open("/proc/stat")
	if err != nil {
		return 0, err
	}
	defer file.Close()
	
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return 0, fmt.Errorf("failed to read /proc/stat")
	}
	
	line := scanner.Text()
	if !strings.HasPrefix(line, "cpu ") {
		return 0, fmt.Errorf("unexpected /proc/stat format")
	}
	
	fields := strings.Fields(line)
	if len(fields) < 8 {
		return 0, fmt.Errorf("insufficient fields in /proc/stat")
	}
	
	var stats cpuStats
	stats.user, _ = strconv.ParseUint(fields[1], 10, 64)
	stats.nice, _ = strconv.ParseUint(fields[2], 10, 64)
	stats.system, _ = strconv.ParseUint(fields[3], 10, 64)
	stats.idle, _ = strconv.ParseUint(fields[4], 10, 64)
	stats.iowait, _ = strconv.ParseUint(fields[5], 10, 64)
	stats.irq, _ = strconv.ParseUint(fields[6], 10, 64)
	stats.total = stats.user + stats.nice + stats.system + stats.idle + stats.iowait + stats.irq
	stats.time = time.Now()
	
	// Calculate percentage from last sample
	if lastCPUStats.total > 0 {
		totalDelta := stats.total - lastCPUStats.total
		idleDelta := stats.idle - lastCPUStats.idle
		
		if totalDelta > 0 {
			usage := float64(totalDelta-idleDelta) / float64(totalDelta) * 100
			lastCPUStats = stats
			return usage, nil
		}
	}
	
	lastCPUStats = stats
	return 0, nil
}

func getMemoryUsage() (used uint64, total uint64, err error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	
	var memFree, memAvailable, buffers, cached uint64
	
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		
		value, _ := strconv.ParseUint(fields[1], 10, 64)
		value *= 1024 // Convert from KB to bytes
		
		switch fields[0] {
		case "MemTotal:":
			total = value
		case "MemFree:":
			memFree = value
		case "MemAvailable:":
			memAvailable = value
		case "Buffers:":
			buffers = value
		case "Cached:":
			cached = value
		}
	}
	
	// Use MemAvailable if present, otherwise calculate
	if memAvailable > 0 {
		used = total - memAvailable
	} else {
		used = total - memFree - buffers - cached
	}
	
	return used, total, nil
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
	file, err := os.Open("/proc/net/dev")
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	
	scanner := bufio.NewScanner(file)
	// Skip header lines
	scanner.Scan()
	scanner.Scan()
	
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		
		// Skip loopback
		if strings.HasPrefix(fields[0], "lo:") {
			continue
		}
		
		// Column 1 is receive bytes, column 9 is transmit bytes
		if r, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
			recv += r
		}
		if s, err := strconv.ParseUint(fields[9], 10, 64); err == nil {
			sent += s
		}
	}
	
	return sent, recv, nil
}

func getUptime() (uint64, error) {
	file, err := os.Open("/proc/uptime")
	if err != nil {
		return 0, err
	}
	defer file.Close()
	
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return 0, fmt.Errorf("failed to read /proc/uptime")
	}
	
	fields := strings.Fields(scanner.Text())
	if len(fields) < 1 {
		return 0, fmt.Errorf("unexpected /proc/uptime format")
	}
	
	uptime, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, err
	}
	
	return uint64(uptime), nil
}
