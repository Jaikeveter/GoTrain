package winproc

import (
	"encoding/binary"
	"testing"
)

func TestCallReloc(t *testing.T) {
	a := NewAsm(0x1000)
	a.B(0x55)
	a.Call(0x1050)
	a.Reloc()

	if a.Buf[0] != 0x55 || a.Buf[1] != 0xE8 {
		t.Fatalf("bad encoding: % X", a.Buf[:6])
	}
	from := 0x1000 + 6
	got := binary.LittleEndian.Uint32(a.Buf[2:6])
	if int64(0x1050)-int64(from) != int64(got) {
		t.Fatalf("rel32 = 0x%X, want 0x%X", got, 0x1050-from)
	}
}

func TestCallRelocHighAddress(t *testing.T) {
	a := NewAsm(0x401000)
	a.Call(0x819210)
	a.Reloc()
	got := binary.LittleEndian.Uint32(a.Buf[1:5])
	want := uint32(0x819210 - (0x401000 + 5))
	if got != want {
		t.Fatalf("rel32 = 0x%X, want 0x%X", got, want)
	}
}

func TestJmp32Reloc(t *testing.T) {
	target := uintptr(0x12345678)
	base := uintptr(0x7FF00000)
	a := NewAsm(base)
	a.Jmp32(target)
	a.Reloc()
	if a.Buf[0] != 0xE9 {
		t.Fatalf("want E9 opcode, got 0x%X", a.Buf[0])
	}
	got := binary.LittleEndian.Uint32(a.Buf[1:5])
	want := uint32(target - (base + 5))
	if got != want {
		t.Fatalf("rel32 = 0x%X, want 0x%X", got, want)
	}
}

func TestJumpPatch(t *testing.T) {
	a := NewAsm(0)
	jz := a.Jz()
	a.B(0x90, 0x90)
	target := a.Pos()
	a.B(0xC3)
	a.Patch8(jz, target)

	if a.Buf[0] != 0x74 {
		t.Fatalf("want jz opcode, got 0x%X", a.Buf[0])
	}
	if int8(a.Buf[1]) != 2 {
		t.Fatalf("disp = %d, want 2", int8(a.Buf[1]))
	}

	b := NewAsm(0)
	b.B(0x90)
	jmp := b.Jmp()
	b.Patch8(jmp, 0)
	if int8(b.Buf[2]) != -3 {
		t.Fatalf("backward disp = %d, want -3", int8(b.Buf[2]))
	}
}

func TestPushImm32(t *testing.T) {
	a := NewAsm(0)
	a.B(0x68)
	a.Imm32(0xFFFFD8EE)
	if len(a.Buf) != 5 || a.Buf[1] != 0xEE || a.Buf[4] != 0xFF {
		t.Fatalf("bad imm32 encoding: % X", a.Buf)
	}
}
