package hook

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestBuildCave(t *testing.T) {
	const (
		mem  = 0x10000000
		fse  = 0x00819210
		text = 0x00819D40
		orig = 0x6F00A000
	)
	entry := uintptr(mem + scOff)
	sc, err := buildCave(mem, entry, fse, text, orig)
	if err != nil {
		t.Fatal(err)
	}
	if len(sc) == 0 || len(sc) > scRoom {
		t.Fatalf("shellcode size %d, room %d", len(sc), scRoom)
	}
	if sc[0] != 0x60 {
		t.Fatalf("prologue: %X, want 60 (pushad)", sc[0])
	}
	if sc[len(sc)-6] != 0x61 {
		t.Fatalf("missing popad before tail jmp: % X", sc[len(sc)-6:])
	}
	if sc[len(sc)-5] != 0xE9 {
		t.Fatalf("tail opcode %X, want E9", sc[len(sc)-5])
	}
	rel := int32(binary.LittleEndian.Uint32(sc[len(sc)-4:]))
	tail := uintptr(int64(entry+uintptr(len(sc))) + int64(rel))
	if tail != orig {
		t.Fatalf("tail jmp -> 0x%X, want EndScene 0x%X", tail, orig)
	}

	foundFSE, foundText := false, false
	for i := 0; i+5 <= len(sc); i++ {
		if sc[i] != 0xE8 {
			continue
		}
		r := int32(binary.LittleEndian.Uint32(sc[i+1 : i+5]))
		tgt := uintptr(int64(entry+uintptr(i+5)) + int64(r))
		if tgt == fse {
			foundFSE = true
		}
		if tgt == text {
			foundText = true
		}
	}
	if !foundFSE {
		t.Fatal("no call to FrameScript_Execute")
	}
	if !foundText {
		t.Fatal("no call to GetText")
	}

	for _, addr := range []uintptr{
		mem + offCmd, mem + offArg, mem + offResult, mem + offFrames,
	} {
		if !bytes.Contains(sc, u32bytes(u32(addr))) {
			t.Fatalf("missing abs addr 0x%X in shellcode", addr)
		}
	}
}
