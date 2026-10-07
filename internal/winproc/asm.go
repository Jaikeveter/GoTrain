package winproc

import "encoding/binary"

type Asm struct {
	Base  uintptr
	Buf   []byte
	rel32 []relPatch
}

type relPatch struct {
	relPos int
	target uintptr
}

func NewAsm(base uintptr) *Asm {
	return &Asm{Base: base}
}

func (a *Asm) B(bs ...byte) *Asm {
	a.Buf = append(a.Buf, bs...)
	return a
}

func (a *Asm) Imm32(v uint32) *Asm {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	a.Buf = append(a.Buf, b[:]...)
	return a
}

func (a *Asm) Call(target uintptr) *Asm {
	a.Buf = append(a.Buf, 0xE8, 0, 0, 0, 0)
	a.rel32 = append(a.rel32, relPatch{relPos: len(a.Buf) - 4, target: target})
	return a
}

func (a *Asm) Jmp32(target uintptr) *Asm {
	a.Buf = append(a.Buf, 0xE9, 0, 0, 0, 0)
	a.rel32 = append(a.rel32, relPatch{relPos: len(a.Buf) - 4, target: target})
	return a
}

func (a *Asm) Jz() int {
	a.Buf = append(a.Buf, 0x74, 0)
	return len(a.Buf) - 1
}

func (a *Asm) Jnz() int {
	a.Buf = append(a.Buf, 0x75, 0)
	return len(a.Buf) - 1
}

func (a *Asm) Jmp() int {
	a.Buf = append(a.Buf, 0xEB, 0)
	return len(a.Buf) - 1
}

func (a *Asm) Patch8(dispPos, targetPos int) {
	a.Buf[dispPos] = byte(int8(targetPos - (dispPos + 1)))
}

func (a *Asm) Pos() int {
	return len(a.Buf)
}

func (a *Asm) Reloc() {
	for _, c := range a.rel32 {
		from := a.Base + uintptr(c.relPos) + 4
		binary.LittleEndian.PutUint32(a.Buf[c.relPos:c.relPos+4], uint32(int64(c.target)-int64(from)))
	}
}
