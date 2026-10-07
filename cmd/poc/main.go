package main

import (
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"gotrain/internal/hook"
	"gotrain/internal/winproc"
	"gotrain/internal/wow"
)

var targets = map[string]uintptr{
	"FrameScript_Execute": wow.FrameScriptExecute,
	"GetText":             wow.GetText,
	"lua_gettop":          wow.LuaGetTop,
	"lua_settop":          wow.LuaSetTop,
	"lua_pushstring":      wow.LuaPushString,
	"lua_tolstring":       wow.LuaToLString,
	"getfield_by_key":     wow.GetFieldByStackKey,
}

func main() {
	luaCode := flag.String("lua", "", "execute lua code in client")
	getName := flag.String("get", "", "read lua global string by name")
	dump := flag.Bool("dump", false, "dump function prologues and d3d chains only")
	probe := flag.Bool("probe", false, "read player info and test Rejuvenation cast")
	flag.Parse()

	pid, err := winproc.Find("Wow.exe")
	if err != nil {
		fail("%v", err)
	}
	p, err := winproc.Open(pid)
	if err != nil {
		fail("%v", err)
	}
	defer p.Close()

	fmt.Printf("pid=%d base=0x%X\n", p.Pid, p.Base)

	if err := fingerprint(p); err != nil {
		fail("fingerprint: %v", err)
	}
	fmt.Println("fingerprint: ok (MZ/PE32)")

	L, err := p.ReadPtr(wow.VA(p.Base, wow.LuaStatePtr))
	if err != nil {
		fail("lua_State: %v", err)
	}
	fmt.Printf("lua_State*=0x%X\n", L)

	for name, off := range targets {
		addr := wow.VA(p.Base, off)
		pro := make([]byte, 16)
		if err := p.Read(addr, pro); err != nil {
			fail("read %s at 0x%X: %v", name, addr, err)
		}
		fmt.Printf("%-22s 0x%08X  % X\n", name, addr, pro)
	}

	mods, modErr := p.Modules()
	if modErr != nil {
		fmt.Printf("modules: err %v\n", modErr)
	} else {
		fmt.Printf("modules: %d loaded\n", len(mods))
	}

	cands, err := hook.Resolve(p)
	if err != nil {
		fail("d3d resolve: %v", err)
	}
	fmt.Println("d3d candidates:")
	for _, c := range cands {
		fmt.Printf("  chain %s:\n", c.Chain)
		fmt.Printf("    p1=0x%X root=0x%X dev=0x%X vt=0x%X\n", c.P1, c.Root, c.Dev, c.VT)
		fmt.Printf("    QI=0x%X (%s) AddRef=0x%X (%s)\n",
			c.F0, modName(mods, c.F0), c.F1, modName(mods, c.F1))
		fmt.Printf("    EndScene slot=0x%X orig=0x%X (%s)\n", c.SlotES, c.OrigES, modName(mods, c.OrigES))
		fmt.Printf("    Present  slot=0x%X orig=0x%X (%s)\n", c.SlotPR, c.OrigPR, modName(mods, c.OrigPR))
		fmt.Printf("    score=%d\n", hook.Score(c, mods))
	}

	if *dump {
		return
	}

	h, err := hook.Install(p)
	if err != nil {
		fail("hook install: %v", err)
	}
	defer h.Uninstall()

	fmt.Printf("hook: slot=0x%X EndScene=0x%X cave=0x%X\n", h.Slot(), h.Orig(), h.Cave())

	cave := make([]byte, 24)
	if err := p.Read(h.Cave(), cave); err != nil {
		fail("read cave: %v", err)
	}
	fmt.Printf("cave[0..24]: % X\n", cave)

	fmt.Print("waiting for frame... ")
	if f, ok := waitFrame(h, 2*time.Second); ok {
		fmt.Printf("frames=%d\n", f)
	} else {
		fmt.Printf("NONE (frames=%d)\n", f)
		return
	}

	if *probe {
		runProbe(h)
		return
	}

	if *luaCode != "" {
		if err := h.Do(*luaCode); err != nil {
			fail("exec: %v", err)
		}
		fmt.Printf("exec ok: %s\n", *luaCode)
	}

	if *getName != "" {
		v, err := h.Get(*getName)
		if err != nil {
			fail("get %s: %v", *getName, err)
		}
		fmt.Printf("%s = %q\n", *getName, v)
		return
	}

	if *luaCode != "" {
		return
	}

	fmt.Print("roundtrip... ")
	if err := h.Do(`GT_POC="hello from gotrain42"`); err != nil {
		fail("exec: %v", err)
	}
	v, err := h.Get("GT_POC")
	if err != nil {
		fail("get: %v", err)
	}
	const want = "hello from gotrain42"
	if v != want {
		fail("got %q, want %q", v, want)
	}
	fmt.Printf("ok (%q)\n", v)
	fmt.Println("POC PASSED")
}

func eval(h *hook.Hook, expr string) (string, error) {
	v, err := evalOK(h, expr)
	if err != "" {
		return v, errors.New(err)
	}
	return v, nil
}

func evalOK(h *hook.Hook, expr string) (string, string) {
	code := fmt.Sprintf(`GT_OK,GT_V=pcall(function() return (%s) end)`, expr)
	if err := h.Do(code); err != nil {
		return "", "Do:" + err.Error()
	}
	v, errV := h.Get("GT_V")
	if errV != nil {
		return "", "getV:" + errV.Error()
	}
	ok, errO := h.Get("GT_OK")
	if errO != nil {
		return v, "getOK:" + errO.Error()
	}
	if ok == "false" {
		return v, v
	}
	return v, ""
}

func errS(e string) string {
	if e == "" {
		return ""
	}
	return "  [ERR: " + e + "]"
}

func runProbe(h *hook.Hook) {
	if h == nil {
		return
	}
	name, _ := eval(h, `UnitName("player")`)
	cls, _ := eval(h, `UnitClass("player")`)
	lvl, _ := eval(h, `UnitLevel("player")`)
	hp, _ := eval(h, `UnitHealth("player")`)
	maxhp, _ := eval(h, `UnitHealthMax("player")`)
	mana, _ := eval(h, `UnitMana("player")`)
	maxmana, _ := eval(h, `UnitManaMax("player")`)
	fmt.Printf("probe: name=%q class=%q level=%q hp=%q/%q mana=%q/%q\n",
		name, cls, lvl, hp, maxhp, mana, maxmana)
	if name == "" || name == "nil" {
		fmt.Println("probe: игрок не в игре, каст пропущен")
		return
	}
	before := buffsList(h)
	fmt.Printf("buffs до каста: [%s]\n", before)

	form, e1 := evalOK(h, `tostring(GetShapeshiftForm(false))`)
	cd, e2 := evalOK(h, `tostring(GetSpellCooldown("Rejuvenation"))`)
	info, e3 := evalOK(h, `tostring(GetSpellInfo(774))`)
	book, e4 := evalOK(h, `(function()
  local found = false
  for tab = 1, GetNumSpellTabs() do
    local _, _, offset, num, _, _ = GetSpellTabInfo(tab)
    for i = 1, num do
      local slotType, id = GetSpellBookItemInfo(offset + i, "player")
      if slotType == "SPELL" and id then
        local name = GetSpellInfo(id)
        if name and string.find(name, "Rejuvenation", 1, true) then found = true end
      end
    end
  end
  return found and "yes" or "no"
end)()`)
	fmt.Printf("форма=%s%s\n", form, errS(e1))
	fmt.Printf("GetSpellCooldown(Rejuvenation)=%s%s\n", cd, errS(e2))
	fmt.Printf("GetSpellInfo(774)=%s%s\n", info, errS(e3))
	fmt.Printf("Rejuvenation в книжке: %s%s\n", book, errS(e4))

	if err := h.Do(`TargetUnit("player")`); err != nil {
		fail("target: %v", err)
	}
	if err := h.Do(`CastSpellByName("Rejuvenation")`); err != nil {
		fail("cast: %v", err)
	}
	if err := h.Do(`CastSpellByID(774)`); err != nil {
		fail("cast2: %v", err)
	}
	fmt.Print("cast Rejuvenation... ")
	deadline := time.Now().Add(3 * time.Second)
	after := before
	for time.Now().Before(deadline) {
		after = buffsList(h)
		if after != before {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if after != before {
		fmt.Printf("ok: баффы изменились: [%s]\n", after)
	} else {
		fmt.Printf("баффы не изменились (каст не прошёл?): [%s]\n", before)
	}
	mana2, _ := eval(h, `UnitMana("player")`)
	fmt.Printf("mana: %s -> %s\n", mana, mana2)
}

func buffsList(h *hook.Hook) string {
	s, err := eval(h, `(function()
  local t = {}
  for i = 1, 40 do
    local n = UnitBuff("player", i)
    if not n then break end
    t[#t+1] = n
  end
  return table.concat(t, "|")
end)()`)
	if err != nil || s == "nil" {
		return ""
	}
	return s
}

func waitFrame(h *hook.Hook, d time.Duration) (uint32, bool) {
	f0, _ := h.Frames()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		f, _ := h.Frames()
		if f > f0 {
			return f, true
		}
		time.Sleep(50 * time.Millisecond)
	}
	f, _ := h.Frames()
	return f, f > f0
}

func modName(mods []winproc.Module, addr uintptr) string {
	m := winproc.FindModule(mods, addr)
	if m == nil {
		return "no-module"
	}
	return m.BaseName()
}

func fingerprint(p *winproc.Process) error {
	mz := make([]byte, 2)
	if err := p.Read(p.Base, mz); err != nil {
		return err
	}
	if mz[0] != 'M' || mz[1] != 'Z' {
		return fmt.Errorf("no MZ at base")
	}
	var lfBuf [4]byte
	if err := p.Read(p.Base+0x3C, lfBuf[:]); err != nil {
		return err
	}
	lfanew := binary.LittleEndian.Uint32(lfBuf[:])
	pe := make([]byte, 6)
	if err := p.Read(p.Base+uintptr(lfanew), pe); err != nil {
		return err
	}
	if pe[0] != 'P' || pe[1] != 'E' || pe[2] != 0 || pe[3] != 0 {
		return fmt.Errorf("no PE signature")
	}
	machine := binary.LittleEndian.Uint16(pe[4:6])
	if machine != 0x14C {
		return fmt.Errorf("machine 0x%X, want i386 (0x14C)", machine)
	}
	return nil
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FAIL: "+format+"\n", args...)
	os.Exit(1)
}
