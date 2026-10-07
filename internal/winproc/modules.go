package winproc

import (
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

type Module struct {
	Base uintptr
	Size uint32
	Name string
}

type moduleInfo struct {
	lpBaseOfDll uintptr
	SizeOfImage uint32
	EntryPoint  uintptr
}

var (
	procGetModuleInformation = psapi.NewProc("GetModuleInformation")
	procGetModuleFileNameExW = psapi.NewProc("GetModuleFileNameExW")
)

func (p *Process) Modules() ([]Module, error) {
	var needed uint32
	buf := make([]windows.Handle, 256)
	r1, _, err := procEnumProcessModules.Call(
		uintptr(p.h),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)*int(unsafe.Sizeof(buf[0]))),
		uintptr(unsafe.Pointer(&needed)),
	)
	if r1 == 0 {
		return nil, fmt.Errorf("EnumProcessModules: %v", err)
	}
	count := int(needed) / int(unsafe.Sizeof(buf[0]))
	if count > len(buf) {
		count = len(buf)
	}
	out := make([]Module, 0, count)
	for i := 0; i < count; i++ {
		var mi moduleInfo
		procGetModuleInformation.Call(
			uintptr(p.h), uintptr(buf[i]),
			uintptr(unsafe.Pointer(&mi)), unsafe.Sizeof(mi))
		nameBuf := make([]uint16, 260)
		ln, _, _ := procGetModuleFileNameExW.Call(
			uintptr(p.h), uintptr(buf[i]),
			uintptr(unsafe.Pointer(&nameBuf[0])), uintptr(len(nameBuf)))
		name := ""
		if ln > 0 {
			name = windows.UTF16ToString(nameBuf[:int(ln)])
		}
		out = append(out, Module{Base: mi.lpBaseOfDll, Size: mi.SizeOfImage, Name: name})
	}
	return out, nil
}

func (m Module) Contains(addr uintptr) bool {
	return addr >= m.Base && addr < m.Base+uintptr(m.Size)
}

func (m Module) BaseName() string {
	if i := strings.LastIndexByte(m.Name, '\\'); i >= 0 {
		return m.Name[i+1:]
	}
	return m.Name
}

func FindModule(mods []Module, addr uintptr) *Module {
	for i := range mods {
		if mods[i].Contains(addr) {
			return &mods[i]
		}
	}
	return nil
}
