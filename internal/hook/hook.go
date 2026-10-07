package hook

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gotrain/internal/winproc"
	"gotrain/internal/wow"
)

var ErrNil = errors.New("global is nil or not a string")

const (
	opNone = 0
	opExec = 1
	opGet  = 2

	offCmd    = 0x00
	offArg    = 0x04
	offResult = 0x08
	offFrames = 0x0C

	scOff    = 0x10
	sc2Off   = 0x210
	scRoom   = 0x200
	codeOff  = 0x410
	codeRoom = 0x1000
	nameOff  = 0x1410
	nameRoom = 0x400

	totalSize = nameOff + nameRoom

	pageRWX = 0x40
)

type Candidate struct {
	Chain  string
	P1     uintptr
	Root   uintptr
	Dev    uintptr
	VT     uintptr
	SlotES uintptr
	OrigES uintptr
	SlotPR uintptr
	OrigPR uintptr
	F0     uintptr
	F1     uintptr
	F2     uintptr
}

func Resolve(p *winproc.Process) ([]Candidate, error) {
	var p1 uintptr
	var err error
	for i := 0; i < 3; i++ {
		if p1, err = p.ReadPtr(wow.VA(p.Base, wow.D3DPtr1)); err != nil {
			return nil, fmt.Errorf("d3d ptr1: %w", err)
		}
		if p1 != 0 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if p1 == 0 {
		return nil, errors.New("d3d ptr1 is null (клиент ещё не создал устройство?)")
	}

	var out []Candidate
	if c := readCand(p, "A:p1+0x397C", p1, p1); c != nil {
		out = append(out, *c)
	}
	if base, e := p.ReadPtr(p1); e == nil && base != 0 && base != p1 {
		if c := readCand(p, "B:*p1+0x397C", p1, base); c != nil {
			dup := false
			for _, x := range out {
				if x.VT == c.VT {
					dup = true
					break
				}
			}
			if !dup {
				out = append(out, *c)
			}
		}
	}
	if len(out) == 0 {
		return nil, errors.New("ни одна цепочка d3d не дала валидной vtable")
	}
	return out, nil
}

func readCand(p *winproc.Process, chain string, p1, root uintptr) *Candidate {
	dev, err := p.ReadPtr(root + wow.D3DStruct2)
	if err != nil || dev == 0 {
		return nil
	}
	vt, err := p.ReadPtr(dev)
	if err != nil || vt == 0 {
		return nil
	}
	es, err := p.ReadPtr(vt + wow.D3DEndSceneOff)
	if err != nil || es == 0 {
		return nil
	}
	pr, _ := p.ReadPtr(vt + wow.D3DPresentOff)
	f0, _ := p.ReadPtr(vt)
	f1, _ := p.ReadPtr(vt + 4)
	f2, _ := p.ReadPtr(vt + 8)
	return &Candidate{
		Chain: chain, P1: p1, Root: root, Dev: dev, VT: vt,
		SlotES: vt + wow.D3DEndSceneOff, OrigES: es,
		SlotPR: vt + wow.D3DPresentOff, OrigPR: pr,
		F0: f0, F1: f1, F2: f2,
	}
}

func Score(c Candidate, mods []winproc.Module) int {
	if c.OrigES == 0 || c.F0 == 0 || c.F1 == 0 {
		return 0
	}
	m := winproc.FindModule(mods, c.OrigES)
	if m == nil {
		return 0
	}
	if c.OrigPR != 0 && !m.Contains(c.OrigPR) {
		return 0
	}
	if !m.Contains(c.F0) || !m.Contains(c.F1) {
		return 0
	}
	if strings.Contains(strings.ToLower(m.BaseName()), "d3d9") {
		return 10
	}
	return 1
}

type Hook struct {
	p       *winproc.Process
	mem     uintptr
	fse     uintptr
	text    uintptr
	slots   []uintptr
	entries []uintptr
	origs   []uintptr
	mu      sync.Mutex
}

func Install(p *winproc.Process) (*Hook, error) {
	cands, err := Resolve(p)
	if err != nil {
		return nil, err
	}
	mods, modErr := p.Modules()
	var best *Candidate
	bestScore := 0
	for i := range cands {
		s := Score(cands[i], mods)
		if s > bestScore {
			best = &cands[i]
			bestScore = s
		}
	}
	if best == nil || bestScore == 0 {
		return nil, fmt.Errorf("vtable не прошла валидацию (модули: %v); candidates: %s",
			modErr, summarize(cands, mods))
	}

	mem, err := p.Alloc(totalSize)
	if err != nil {
		return nil, err
	}
	h := &Hook{
		p:    p,
		mem:  mem,
		fse:  wow.VA(p.Base, wow.FrameScriptExecute),
		text: wow.VA(p.Base, wow.GetText),
	}

	type patch struct{ slot, entry, orig uintptr }
	var plan []patch

	sc1, err := buildCave(mem, mem+scOff, h.fse, h.text, best.OrigES)
	if err != nil {
		p.Free(mem)
		return nil, err
	}
	if err := p.Write(mem+scOff, sc1); err != nil {
		p.Free(mem)
		return nil, err
	}
	plan = append(plan, patch{best.SlotES, mem + scOff, best.OrigES})

	if best.OrigPR != 0 {
		sc2, err := buildCave(mem, mem+sc2Off, h.fse, h.text, best.OrigPR)
		if err != nil {
			p.Free(mem)
			return nil, err
		}
		if err := p.Write(mem+sc2Off, sc2); err != nil {
			p.Free(mem)
			return nil, err
		}
		plan = append(plan, patch{best.SlotPR, mem + sc2Off, best.OrigPR})
	}

	rollback := func(n int) {
		for i := 0; i < n; i++ {
			h.patchSlot(plan[i].slot, plan[i].orig)
		}
	}
	for i, pt := range plan {
		if err := h.patchSlot(pt.slot, pt.entry); err != nil {
			rollback(i)
			p.Free(mem)
			return nil, err
		}
		got, err := p.ReadU32(pt.slot)
		if err != nil || uintptr(got) != pt.entry {
			rollback(i + 1)
			p.Free(mem)
			return nil, fmt.Errorf("vtable readback: got 0x%X, want 0x%X (err=%v)", got, pt.entry, err)
		}
		h.slots = append(h.slots, pt.slot)
		h.entries = append(h.entries, pt.entry)
		h.origs = append(h.origs, pt.orig)
	}
	return h, nil
}

func (h *Hook) Uninstall() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.mem == 0 {
		return nil
	}
	var err error
	for i, slot := range h.slots {
		if e := h.patchSlot(slot, h.origs[i]); e != nil && err == nil {
			err = e
		}
	}
	time.Sleep(150 * time.Millisecond)
	if e := h.p.Free(h.mem); e != nil && err == nil {
		err = e
	}
	h.mem = 0
	return err
}

func (h *Hook) Do(code string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.idle(); err != nil {
		return err
	}
	b := append([]byte(code), 0)
	if len(b) > codeRoom {
		return fmt.Errorf("lua code too long: %d > %d", len(b), codeRoom)
	}
	if err := h.p.Write(h.mem+codeOff, b); err != nil {
		return err
	}
	if err := h.writeU32(offArg, u32(h.mem+codeOff)); err != nil {
		return err
	}
	if err := h.writeU32(offCmd, opExec); err != nil {
		return err
	}
	return h.wait("exec")
}

func (h *Hook) Get(name string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.idle(); err != nil {
		return "", err
	}
	b := append([]byte(name), 0)
	if len(b) > nameRoom {
		return "", fmt.Errorf("global name too long: %d > %d", len(b), nameRoom)
	}
	if err := h.p.Write(h.mem+nameOff, b); err != nil {
		return "", err
	}
	if err := h.writeU32(offArg, u32(h.mem+nameOff)); err != nil {
		return "", err
	}
	if err := h.writeU32(offResult, 0); err != nil {
		return "", err
	}
	if err := h.writeU32(offCmd, opGet); err != nil {
		return "", err
	}
	if err := h.wait("get"); err != nil {
		return "", err
	}
	res, err := h.readU32(offResult)
	if err != nil {
		return "", err
	}
	if res == 0 {
		return "", ErrNil
	}
	return h.p.ReadCString(uintptr(res), 4096)
}

func (h *Hook) Frames() (uint32, error) {
	return h.readU32(offFrames)
}

func (h *Hook) Slot() uintptr { return h.slots[0] }

func (h *Hook) Orig() uintptr { return h.origs[0] }

func (h *Hook) Cave() uintptr { return h.entries[0] }

func (h *Hook) idle() error {
	cmd, err := h.readU32(offCmd)
	if err != nil {
		return err
	}
	if cmd != opNone {
		return fmt.Errorf("hook busy (cmd=%d), предыдущая команда не завершилась", cmd)
	}
	return nil
}

func (h *Hook) wait(op string) error {
	f0, err := h.readU32(offFrames)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		cmd, err := h.readU32(offCmd)
		if err != nil {
			return err
		}
		if cmd == opNone {
			return nil
		}
		time.Sleep(time.Millisecond)
	}
	f1, _ := h.readU32(offFrames)
	cmd, _ := h.readU32(offCmd)
	hint := ""
	if f0 == f1 {
		hint = "; EndScene/Present не вызываются (окно свёрнуто?)"
	}
	return fmt.Errorf("%s timeout: cmd=%d, frames %d->%d%s", op, cmd, f0, f1, hint)
}

func (h *Hook) readU32(off uintptr) (uint32, error) {
	return h.p.ReadU32(h.mem + off)
}

func (h *Hook) writeU32(off uintptr, v uint32) error {
	return h.p.Write(h.mem+off, u32bytes(v))
}

func (h *Hook) patchSlot(addr, val uintptr) error {
	old, err := h.p.Protect(addr, 4, pageRWX)
	if err != nil {
		return fmt.Errorf("vtable protect: %w", err)
	}
	if err := h.p.Write(addr, u32bytes(u32(val))); err != nil {
		h.p.Protect(addr, 4, old)
		return fmt.Errorf("vtable write: %w", err)
	}
	if _, err := h.p.Protect(addr, 4, old); err != nil {
		return fmt.Errorf("vtable restore protect: %w", err)
	}
	return nil
}

func summarize(cands []Candidate, mods []winproc.Module) string {
	var parts []string
	for _, c := range cands {
		m := winproc.FindModule(mods, c.OrigES)
		name := "no-module"
		if m != nil {
			name = m.BaseName()
		}
		parts = append(parts, fmt.Sprintf("%s vt=0x%X es=0x%X (%s)",
			c.Chain, c.VT, c.OrigES, name))
	}
	return strings.Join(parts, "; ")
}

func buildCave(mem, entry, fse, getText, orig uintptr) ([]byte, error) {
	a := winproc.NewAsm(entry)

	a.B(0x60) // pushad
	a.B(0xFF, 0x05)
	a.Imm32(u32(mem + offFrames))
	a.B(0xA1)
	a.Imm32(u32(mem + offCmd))
	a.B(0x85, 0xC0)
	jzDone := a.Jz()
	a.B(0x83, 0xF8, 0x01)
	jeExec := a.Jz()
	a.B(0x83, 0xF8, 0x02)
	jeGet := a.Jz()
	jmpDone := a.Jmp()

	execPos := a.Pos()
	a.B(0x6A, 0x00)
	a.B(0xFF, 0x35)
	a.Imm32(u32(mem + offArg))
	a.B(0xFF, 0x35)
	a.Imm32(u32(mem + offArg))
	a.B(0x89, 0xE3)
	a.Call(fse)
	a.B(0x89, 0xDC)
	a.B(0x83, 0xC4, 0x0C)
	a.B(0xC7, 0x05)
	a.Imm32(u32(mem + offCmd))
	a.Imm32(0)
	jmpDone2 := a.Jmp()

	getPos := a.Pos()
	a.B(0x6A, 0x00)
	a.B(0x6A, 0xFF)
	a.B(0xFF, 0x35)
	a.Imm32(u32(mem + offArg))
	a.B(0x89, 0xE3)
	a.Call(getText)
	a.B(0x89, 0xDC)
	a.B(0x83, 0xC4, 0x0C)
	a.B(0xA3)
	a.Imm32(u32(mem + offResult))
	a.B(0xC7, 0x05)
	a.Imm32(u32(mem + offCmd))
	a.Imm32(0)

	donePos := a.Pos()
	a.Patch8(jzDone, donePos)
	a.Patch8(jmpDone, donePos)
	a.Patch8(jmpDone2, donePos)
	a.Patch8(jeExec, execPos)
	a.Patch8(jeGet, getPos)
	a.B(0x61) // popad
	a.Jmp32(orig)

	a.Reloc()
	if len(a.Buf) > scRoom {
		return nil, fmt.Errorf("cave shellcode too big: %d > %d", len(a.Buf), scRoom)
	}
	return a.Buf, nil
}

func u32(v uintptr) uint32 {
	if v > 0xFFFFFFFF {
		panic(fmt.Sprintf("address 0x%X does not fit in 32 bits", v))
	}
	return uint32(v)
}

func u32bytes(v uint32) []byte {
	return []byte{
		byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24),
	}
}
