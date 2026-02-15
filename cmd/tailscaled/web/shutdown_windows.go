// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package web

import (
	"syscall"
	"time"
	"unsafe"
)

var (
	advapi32                  = syscall.NewLazyDLL("advapi32.dll")
	user32                    = syscall.NewLazyDLL("user32.dll")
	procOpenProcessToken      = advapi32.NewProc("OpenProcessToken")
	procLookupPrivilegeValue  = advapi32.NewProc("LookupPrivilegeValueW")
	procAdjustTokenPrivileges = advapi32.NewProc("AdjustTokenPrivileges")
	procExitWindowsEx         = user32.NewProc("ExitWindowsEx")
)

const (
	TOKEN_ADJUST_PRIVILEGES = 0x0020
	TOKEN_QUERY             = 0x0008
	SE_PRIVILEGE_ENABLED    = 0x00000002
	EWX_POWEROFF            = 0x00000008
	EWX_FORCE               = 0x00000004
)

type LUID struct {
	LowPart  uint32
	HighPart int32
}

type LUID_AND_ATTRIBUTES struct {
	Luid       LUID
	Attributes uint32
}

type TOKEN_PRIVILEGES struct {
	PrivilegeCount uint32
	Privileges     [1]LUID_AND_ATTRIBUTES
}

func shutdownSystem(force bool) error {
	// Give a small delay to allow HTTP response to be sent
	time.Sleep(100 * time.Millisecond)

	// Get current process token
	var token syscall.Token
	proc := syscall.CurrentProcess()

	ret, _, err := procOpenProcessToken.Call(
		uintptr(proc),
		TOKEN_ADJUST_PRIVILEGES|TOKEN_QUERY,
		uintptr(unsafe.Pointer(&token)),
	)
	if ret == 0 {
		return err
	}
	defer syscall.CloseHandle(syscall.Handle(token))

	// Lookup shutdown privilege
	var luid LUID
	name, err := syscall.UTF16PtrFromString("SeShutdownPrivilege")
	if err != nil {
		return err
	}

	ret, _, err = procLookupPrivilegeValue.Call(
		0,
		uintptr(unsafe.Pointer(name)),
		uintptr(unsafe.Pointer(&luid)),
	)
	if ret == 0 {
		return err
	}

	// Enable shutdown privilege
	tp := TOKEN_PRIVILEGES{
		PrivilegeCount: 1,
		Privileges: [1]LUID_AND_ATTRIBUTES{
			{
				Luid:       luid,
				Attributes: SE_PRIVILEGE_ENABLED,
			},
		},
	}

	ret, _, err = procAdjustTokenPrivileges.Call(
		uintptr(token),
		0,
		uintptr(unsafe.Pointer(&tp)),
		0,
		0,
		0,
	)
	if ret == 0 {
		return err
	}

	// Shutdown system
	flags := EWX_POWEROFF
	if force {
		flags |= EWX_FORCE // Force close all apps without prompting
	}

	ret, _, err = procExitWindowsEx.Call(
		uintptr(flags),
		0,
	)
	if ret == 0 {
		return err
	}

	return nil
}
