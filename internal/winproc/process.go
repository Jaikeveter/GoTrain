package winproc

import (
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

type Process struct {
	Pid  uint32
	Base uintptr
	h    windows.Handle
}

func Find(name string) (uint32, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(snap)

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err := windows.Process32First(snap, &pe); err != nil {
		return 0, err
	}
	for {
		exe := windows.UTF16ToString(pe.ExeFile[:])
		if strings.EqualFold(exe, name) {
			return pe.ProcessID, nil
		}
		if err := windows.Process32Next(snap, &pe); err != nil {
			return 0, fmt.Errorf("process %s not found", name)
		}
	}
}

func Open(pid uint32) (*Process, error) {
	_ = enableDebugPrivilege()
	_ = enableDebugPrivilege()
	const access = windows.PROCESS_QUERY_INFORMATION |
		windows.PROCESS_VM_READ | windows.PROCESS_VM_WRITE | windows.PROCESS_VM_OPERATION
	h, err := windows.OpenProcess(access, false, pid)
	if err != nil {
		return nil, fmt.Errorf("open process %d: %w (запусти бота ОТ ИМЕНИ АДМИНИСТРАТОРА, если игра идёт под админом)", pid, err)
	}
	p := &Process{Pid: pid, h: h}
	base, err := p.moduleBase()
	if err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	p.Base = base
	return p, nil
}

func (p *Process) Close() error {
	return windows.CloseHandle(p.h)
}

func (p *Process) Handle() windows.Handle {
	return p.h
}

var (
	psapi                  = windows.NewLazySystemDLL("psapi.dll")
	procEnumProcessModules = psapi.NewProc("EnumProcessModules")
)

var (
	advapi32                    = windows.NewLazySystemDLL("advapi32.dll")
	procOpenProcessToken        = advapi32.NewProc("OpenProcessToken")
	procLookupPrivilegeValueW   = advapi32.NewProc("LookupPrivilegeValueW")
	procAdjustTokenPrivileges   = advapi32.NewProc("AdjustTokenPrivileges")
)

// enableDebugPrivilege поднимает SeDebugPrivilege в текущем токене, чтобы
// OpenProcess/ReadProcessMemory работали против процесса игры, запущенной админом.
func enableDebugPrivilege() error {
	var tok windows.Token
	const adjAndQuery = 0x0020 | 0x0008
	hCur, _ := windows.GetCurrentProcess()
	r1, _, _ := procOpenProcessToken.Call(
		uintptr(hCur), adjAndQuery, uintptr(unsafe.Pointer(&tok)))
	if r1 == 0 {
		return fmt.Errorf("OpenProcessToken: %v", windows.GetLastError())
	}
	defer tok.Close()

	var nameC [64]uint16
	copy(nameC[:], windows.StringToUTF16("SeDebugPrivilege"))
	var luid windows.LUID
	r2, _, _ := procLookupPrivilegeValueW.Call(0, uintptr(unsafe.Pointer(&nameC[0])), uintptr(unsafe.Pointer(&luid)))
	if r2 == 0 {
		return fmt.Errorf("LookupPrivilegeValueW: %v", windows.GetLastError())
	}

	type luidAndAttributes struct {
		Luid       windows.LUID
		Attributes uint32
	}
	type tokenPrivileges struct {
		PrivilegeCount uint32
		Privileges     [1]luidAndAttributes
	}
	var tp tokenPrivileges
	tp.PrivilegeCount = 1
	tp.Privileges[0].Luid = luid
	tp.Privileges[0].Attributes = 0x00000002 // SE_PRIVILEGE_ENABLED
	r3, _, _ := procAdjustTokenPrivileges.Call(
		uintptr(tok), 0, uintptr(unsafe.Pointer(&tp)), 0, 0, 0)
	if r3 == 0 {
		return fmt.Errorf("AdjustTokenPrivileges: %v", windows.GetLastError())
	}
	return nil
}

func (p *Process) moduleBase() (uintptr, error) {
	var needed uint32
	var mod [1]windows.Handle
	r1, _, err := procEnumProcessModules.Call(
		uintptr(p.h),
		uintptr(unsafe.Pointer(&mod[0])),
		uintptr(len(mod)*int(unsafe.Sizeof(mod[0]))),
		uintptr(unsafe.Pointer(&needed)),
	)
	if r1 == 0 {
		return 0, fmt.Errorf("EnumProcessModules: %v", err)
	}
	if needed == 0 {
		return 0, fmt.Errorf("no modules in target process")
	}
	return uintptr(mod[0]), nil
}

func (p *Process) Read(addr uintptr, buf []byte) error {
	if len(buf) == 0 {
		return nil
	}
	var n uintptr
	err := windows.ReadProcessMemory(p.h, addr, &buf[0], uintptr(len(buf)), &n)
	if err != nil {
		return fmt.Errorf("read 0x%X: %w", addr, err)
	}
	return nil
}

func (p *Process) Write(addr uintptr, buf []byte) error {
	if len(buf) == 0 {
		return nil
	}
	var n uintptr
	err := windows.WriteProcessMemory(p.h, addr, &buf[0], uintptr(len(buf)), &n)
	if err != nil {
		return fmt.Errorf("write 0x%X: %w", addr, err)
	}
	return nil
}

func (p *Process) ReadU32(addr uintptr) (uint32, error) {
	var b [4]byte
	if err := p.Read(addr, b[:]); err != nil {
		return 0, err
	}
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24, nil
}

func (p *Process) ReadPtr(addr uintptr) (uintptr, error) {
	v, err := p.ReadU32(addr)
	return uintptr(v), err
}

func (p *Process) ReadCString(addr uintptr, max int) (string, error) {
	buf := make([]byte, max)
	if err := p.Read(addr, buf); err != nil {
		return "", err
	}
	for i, c := range buf {
		if c == 0 {
			return string(buf[:i]), nil
		}
	}
	return string(buf), nil
}
