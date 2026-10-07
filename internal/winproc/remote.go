package winproc

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	memCommit            = 0x1000
	memReserve           = 0x2000
	memRelease           = 0x8000
	pageReadWrite        = 0x04
	pageExecuteReadWrite = 0x40
	waitTimeout          = 0x00000102
	callTimeoutMS        = 10000
)

var (
	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procVirtualAllocEx     = kernel32.NewProc("VirtualAllocEx")
	procVirtualFreeEx      = kernel32.NewProc("VirtualFreeEx")
	procVirtualProtectEx   = kernel32.NewProc("VirtualProtectEx")
	procCreateRemoteThread = kernel32.NewProc("CreateRemoteThread")
	procGetExitCodeThread  = kernel32.NewProc("GetExitCodeThread")
)

func (p *Process) Protect(addr, size, prot uintptr) (uintptr, error) {
	var old uintptr
	r1, _, err := procVirtualProtectEx.Call(
		uintptr(p.h), addr, size, prot, uintptr(unsafe.Pointer(&old)))
	if r1 == 0 {
		return 0, fmt.Errorf("VirtualProtectEx(0x%X, %d): %v", addr, size, err)
	}
	return old, nil
}

func (p *Process) Alloc(size uintptr) (uintptr, error) {
	if size == 0 {
		return 0, fmt.Errorf("alloc size 0")
	}
	r1, _, err := procVirtualAllocEx.Call(
		uintptr(p.h), 0, size, memCommit|memReserve, pageReadWrite)
	if r1 == 0 {
		return 0, fmt.Errorf("VirtualAllocEx(%d): %v", size, err)
	}
	return r1, nil
}

func (p *Process) Free(addr uintptr) error {
	if addr == 0 {
		return nil
	}
	r1, _, err := procVirtualFreeEx.Call(uintptr(p.h), addr, 0, memRelease)
	if r1 == 0 {
		return fmt.Errorf("VirtualFreeEx: %v", err)
	}
	return nil
}

func (p *Process) Run(addr uintptr) (uint32, error) {
	r1, _, err := procCreateRemoteThread.Call(uintptr(p.h), 0, 0, addr, 0, 0, 0)
	if r1 == 0 {
		return 0, fmt.Errorf("CreateRemoteThread: %v", err)
	}
	tid := windows.Handle(r1)
	defer windows.CloseHandle(tid)

	st, err := windows.WaitForSingleObject(tid, callTimeoutMS)
	if err != nil {
		return 0, fmt.Errorf("WaitForSingleObject: %v", err)
	}
	if st == waitTimeout {
		return 0, fmt.Errorf("remote call timeout")
	}
	var code uint32
	r2, _, _ := procGetExitCodeThread.Call(uintptr(tid), uintptr(unsafe.Pointer(&code)))
	if r2 == 0 {
		return 0, fmt.Errorf("GetExitCodeThread failed")
	}
	return code, nil
}

func (p *Process) Call(entry uintptr, args ...uint32) (uint32, error) {
	a := NewAsm(0)
	a.B(0x55, 0x8B, 0xEC)
	a.B(0x8B, 0xE5)
	for i := len(args) - 1; i >= 0; i-- {
		a.B(0x68)
		a.Imm32(args[i])
	}
	a.Call(entry)
	a.B(0x8B, 0xE5, 0x5D, 0xC3)

	mem, err := p.Alloc(uintptr(len(a.Buf)))
	if err != nil {
		return 0, err
	}
	defer p.Free(mem)

	a.Base = mem
	a.Reloc()
	if err := p.Write(mem, a.Buf); err != nil {
		return 0, err
	}
	return p.Run(mem)
}
