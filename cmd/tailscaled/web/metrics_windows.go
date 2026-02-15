// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package web

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procGetSystemTimes      = kernel32.NewProc("GetSystemTimes")
	procGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
	procGetDiskFreeSpaceExW = kernel32.NewProc("GetDiskFreeSpaceExW")
	procGetTickCount64      = kernel32.NewProc("GetTickCount64")
)

type memoryStatusEx struct {
	dwLength                uint32
	dwMemoryLoad            uint32
	ullTotalPhys            uint64
	ullAvailPhys            uint64
	ullTotalPageFile        uint64
	ullAvailPageFile        uint64
	ullTotalVirtual         uint64
	ullAvailVirtual         uint64
	ullAvailExtendedVirtual uint64
}

var lastCPUTimes cpuTimes

type cpuTimes struct {
	idle   uint64
	kernel uint64
	user   uint64
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
	
	// Get network stats (Windows implementation would need more work)
	// For now, we'll set to 0
	metrics.NetworkBytesSent = 0
	metrics.NetworkBytesRecv = 0
	
	// Get uptime
	uptime, err := getUptime()
	if err == nil {
		metrics.UptimeSeconds = uptime
	}
	
	return metrics, nil
}

func getCPUUsage() (float64, error) {
	var idleTime, kernelTime, userTime syscall.Filetime
	
	ret, _, err := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idleTime)),
		uintptr(unsafe.Pointer(&kernelTime)),
		uintptr(unsafe.Pointer(&userTime)),
	)
	
	if ret == 0 {
		return 0, err
	}
	
	idle := fileTimeToUint64(idleTime)
	kernel := fileTimeToUint64(kernelTime)
	user := fileTimeToUint64(userTime)
	
	now := time.Now()
	
	if lastCPUTimes.time.IsZero() {
		lastCPUTimes = cpuTimes{idle, kernel, user, now}
		return 0, nil
	}
	
	idleDelta := idle - lastCPUTimes.idle
	kernelDelta := kernel - lastCPUTimes.kernel
	userDelta := user - lastCPUTimes.user
	
	totalDelta := kernelDelta + userDelta
	
	var usage float64
	if totalDelta > 0 {
		usage = float64(totalDelta-idleDelta) / float64(totalDelta) * 100
	}
	
	lastCPUTimes = cpuTimes{idle, kernel, user, now}
	
	return usage, nil
}

func fileTimeToUint64(ft syscall.Filetime) uint64 {
	return (uint64(ft.HighDateTime) << 32) | uint64(ft.LowDateTime)
}

func getMemoryUsage() (used uint64, total uint64, err error) {
	var memStatus memoryStatusEx
	memStatus.dwLength = uint32(unsafe.Sizeof(memStatus))
	
	ret, _, err := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&memStatus)))
	if ret == 0 {
		return 0, 0, err
	}
	
	total = memStatus.ullTotalPhys
	used = total - memStatus.ullAvailPhys
	
	return used, total, nil
}

func getDiskUsage() (used uint64, total uint64, err error) {
	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	
	// Get C:\ drive
	drive, err := syscall.UTF16PtrFromString("C:\\")
	if err != nil {
		return 0, 0, err
	}
	
	ret, _, err := procGetDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(drive)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFreeBytes)),
	)
	
	if ret == 0 {
		return 0, 0, err
	}
	
	total = totalBytes
	used = total - totalFreeBytes
	
	return used, total, nil
}

func getUptime() (uint64, error) {
	ret, _, _ := procGetTickCount64.Call()
	if ret == 0 {
		return 0, fmt.Errorf("failed to get tick count")
	}
	
	// Convert milliseconds to seconds
	return uint64(ret) / 1000, nil
}
