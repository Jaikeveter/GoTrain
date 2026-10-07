package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"gotrain/internal/game"
	"gotrain/internal/hook"
	"gotrain/internal/winproc"
)

const (
	rejuvID    = 774
	regrowthID = 8936
	htID       = 5187
	innervate  = 29166
	swiftmend  = 18562
	motwID     = 1126
	thornsID   = 467
	barkID     = 22812
	treeID     = 33891

	rejuvDur = 12 * time.Second

	treeForm = 6
)

var spellNames = map[int]string{
	rejuvID:    "Rejuvenation",
	regrowthID: "Regrowth",
	htID:       "Healing Touch",
	innervate:  "Innervate",
	swiftmend:  "Swiftmend",
	motwID:     "Mark of the Wild",
	thornsID:   "Thorns",
	barkID:     "Barkskin",
	treeID:     "Tree of Life",
}

var buffIDs = []int{motwID, thornsID}

var hookStop = make(chan struct{})
var hookStopOnce sync.Once

func ctrlHandler(ctrlType uintptr) uintptr {
	switch ctrlType {
	case 0, 1, 2, 5, 6: // CTRL_C, CTRL_BREAK, CTRL_CLOSE, CTRL_LOGOFF, CTRL_SHUTDOWN
		hookStopOnce.Do(func() { close(hookStop) })
		return 1
	}
	return 0
}

func setupConsoleClose() {
	k32 := syscall.NewLazyDLL("kernel32.dll")
	k32.NewProc("SetConsoleCtrlHandler").Call(syscall.NewCallback(ctrlHandler), 1)
}

func printCastStats(casts map[int]int) {
	fmt.Println("\nbye")
	for id, n := range casts {
		if n > 0 {
			fmt.Printf("кастов %s: %d\n", spellNames[id], n)
		}
	}
}

func main() {
	tick := flag.Duration("tick", 100*time.Millisecond, "decision tick")
	hotPct := flag.Float64("hot", 75, "cast Rejuvenation below this hp%")
	flashPct := flag.Float64("flash", 45, "cast Healing Touch below this hp%")
	regrowPct := flag.Float64("regrow", 50, "cast Regrowth below this hp% while HoT is up")
	swiftPct := flag.Float64("swift", 45, "cast Swiftmend below this hp% (consumes HoT)")
	hotCount := flag.Int("hotCount", 3, "max simultaneous Rejuvenation HoTs")
	manaLow := flag.Float64("manaLow", 12, "cast Innervate below this mana%")
	barkPct := flag.Float64("bark", 30, "cast Barkskin on self below this hp%")
	trinketOn := flag.Bool("trinket", false, "auto-use trinkets (slots 13/14) during hard heals")
	treeOn := flag.Bool("tree", false, "enter Tree of Life form when everyone is healthy")
	stealthOn := flag.Bool("stealth", false, "hide console window and suppress status spam")
	gcdF := flag.Duration("gcd", 1600*time.Millisecond, "global cast lock (client GCD is 1500ms)")
	selftest := flag.Bool("selftest", false, "cast Rejuv/HT/Innervate once and report")
	casttest := flag.Bool("casttest", false, "probe hardcast start: HT and Regrowth on self, trace casting state")
	pidF := flag.Int("pid", 0, "WoW process id (default: first Wow.exe found)")
	flag.Parse()

	pid, err := winproc.Find("Wow.exe")
	if err != nil {
		fail("%v", err)
	}
	if *pidF > 0 {
		pid = uint32(*pidF)
	}
	fmt.Printf("healbot: Wow pid=%d (если клиентов несколько — запусти с -pid)\n", pid)
	p, err := winproc.Open(pid)
	if err != nil {
		fail("%v", err)
	}
	defer p.Close()

	if err := hook.RestoreStale(p); err != nil {
		fmt.Printf("healbot: восстановление vtable: %v\n", err)
	}
	if msg := hook.RepairedCaves(p); msg != "" {
		fmt.Println("healbot: восстановлен vtable:", msg)
	}

	h, err := hook.Install(p)
	if err != nil {
		fail("hook: %v", err)
	}
	slots, origs, ents := h.Snapshot()
	_ = hook.SaveState(pid, slots, origs, ents)
	defer func() {
		hook.ClearState(pid)
		_ = h.Uninstall()
	}()
	defer game.UIDestroy(h)
	fmt.Printf("healbot: hook ok (EndScene=0x%X)\n", h.Orig())
	fmt.Printf("healbot: пороги hot<%.0f%% regrow<%.0f%% swift<%.0f%% flash<%.0f%% innervate<%.0f%% маны, лимит HoT=%d, лок каста %v\n",
		*hotPct, *regrowPct, *swiftPct, *flashPct, *manaLow, *hotCount, *gcdF)
	if *stealthOn {
		hideConsole()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	setupConsoleClose()

	f0, _ := h.Frames()
	lastCast := time.Now()
	hotUntil := map[string]time.Time{}
	skippedUntil := map[int]time.Time{}
	lastStatus := time.Time{}
	lastUI := time.Time{}
	lastWarn := time.Time{}
	lastErr := error(nil)
	uiInited := false
	uiInitAt := time.Now()
	uiOn, uiBuff, uiForm := true, true, true
	lastBuffCheck := time.Now()
	casts := map[int]int{}
	lastTrinket := time.Time{}
	trinketSlot := 13

	if *selftest {
		runSelfTest(h, &lastCast)
	}
	if *casttest {
		runCastTest(h)
		return
	}

	for {
		select {
		case <-ctx.Done():
			printCastStats(casts)
			return
		case <-hookStop:
			printCastStats(casts)
			return
		default:
		}

		f, _ := h.Frames()
		if lastErr != nil && f == f0 {
			if time.Since(lastWarn) > 10*time.Second {
				fmt.Printf("%s жду рендер (фокусируй/не сворачивай окно): %v\n", ts(), lastErr)
				lastWarn = time.Now()
			}
			time.Sleep(*tick)
			continue
		}
		f0 = f

		st, err := game.Read(h)
		if err != nil {
			lastErr = err
			time.Sleep(*tick)
			continue
		}
		lastErr = nil
		if time.Since(lastStatus) > 2*time.Second {
			if !*stealthOn {
				fmt.Println(st.String())
			}
			lastStatus = time.Now()
		}

		now := time.Now()

		if !uiInited && time.Since(uiInitAt) > 100*time.Millisecond {
			if err := game.UIInit(h); err == nil {
				uiInited = true
			} else {
				uiInitAt = now
			}
		} else if uiInited && time.Since(lastUI) > 500*time.Millisecond {
			if on, buff, form, err := game.UIState(h); err == nil {
				uiOn, uiBuff, uiForm = on, buff, form
			}
			lastUI = now
			_ = game.UIUpdate(h)
		}

		if !uiOn && !uiBuff {
			time.Sleep(*tick)
			continue
		}

		if st.Casting != "" {
			time.Sleep(*tick)
			continue
		}
		if st.Form != 0 {
			treeOK := *treeOn && st.Form == treeForm
			if treeOK {
				// в tree-форме можно продолжать хилиться
			} else if uiForm {
				_ = h.Do(`CancelShapeshiftForm()`)
				fmt.Printf("%s выхожу из формы %d (uiForm=%v)\n", ts(), st.Form, uiForm)
				time.Sleep(*tick)
				continue
			} else {
				// чекбокс «Облик» снят — форму не трогаем, хилы пропускаем
				fmt.Printf("%s форма %d сохранена (uiForm=%v)\n", ts(), st.Form, uiForm)
				time.Sleep(*tick)
				continue
			}
		}

		if uiOn && st.ManaPct() < *manaLow && now.Sub(lastCast) > *gcdF {
			if v, ok := skippedUntil[innervate]; ok && now.Before(v) {
				time.Sleep(*tick)
				continue
			}
			if cast(h, "player", innervate, &lastCast, 0, false) {
				casts[innervate]++
			} else {
				skippedUntil[innervate] = now.Add(10 * time.Minute)
			}
			time.Sleep(*tick)
			continue
		}

		didCast := false
		if uiOn {
			best := lowest(st.Units)
			if best != nil && now.Sub(lastCast) > *gcdF {
				pct := best.Pct()
				hot := now.Before(hotUntil[best.ID])
				spell := 0
				switch {
				case pct < *flashPct:
					spell = htID
				case hot && pct < *swiftPct:
					spell = swiftmend
				case hot && pct < *regrowPct:
					spell = regrowthID
				case pct < *hotPct && now.After(hotUntil[best.ID]) && hotBudgetOK(hotUntil, *hotCount):
					spell = rejuvID
				}
				if spell != 0 {
					if v, ok := skippedUntil[spell]; ok && now.Before(v) {
						time.Sleep(*tick)
						continue
					}
					hard := spell == htID || spell == regrowthID
					if hard && *trinketOn && time.Since(lastTrinket) > 60*time.Second {
						if err := h.Do(fmt.Sprintf("UseInventoryItem(%d)", trinketSlot)); err == nil {
							lastTrinket = now
							trinketSlot = 15 - trinketSlot
						}
					}
					if cast(h, best.ID, spell, &lastCast, int(pct), hard) {
						if spell == rejuvID {
							hotUntil[best.ID] = now.Add(rejuvDur)
						}
						if spell == swiftmend {
							delete(hotUntil, best.ID)
						}
						casts[spell]++
						didCast = true
					} else {
						skippedUntil[spell] = now.Add(10 * time.Minute)
					}
				}
			}

			if !didCast {
				self := unitByID(st.Units, "player")
				if self != nil && self.Pct() < *barkPct && now.Sub(lastCast) > *gcdF {
					if v, ok := skippedUntil[barkID]; ok && now.Before(v) {
						didCast = false
					} else if castBuff(h, "player", barkID, &lastCast) {
						casts[barkID]++
						didCast = true
					} else {
						skippedUntil[barkID] = now.Add(10 * time.Minute)
					}
				}
			}
		}

		if !didCast && *treeOn && st.Form == 0 && allHealthy(st.Units, *hotPct) && now.Sub(lastCast) > *gcdF {
			if v, ok := skippedUntil[treeID]; ok && now.Before(v) {
				// пропуск
			} else if cast(h, "player", treeID, &lastCast, -1, false) {
				casts[treeID]++
				didCast = true
			} else {
				skippedUntil[treeID] = now.Add(10 * time.Minute)
			}
		}

		if !didCast && uiBuff && st.ManaPct() > 20 && now.Sub(lastCast) > *gcdF && time.Since(lastBuffCheck) > 500*time.Millisecond {
			lastBuffCheck = now
			for _, id := range buffIDs {
				if v, ok := skippedUntil[id]; ok && now.Before(v) {
					continue
				}
				units, err := game.BuffMissing(h, id)
				if err != nil || len(units) == 0 {
					continue
				}
				if cast(h, units[0], id, &lastCast, -1, false) {
					casts[id]++
				} else {
					skippedUntil[id] = now.Add(10 * time.Minute)
				}
				break
			}
		}
		time.Sleep(*tick)
	}
}

func allHealthy(units []game.Unit, hotPct float64) bool {
	for _, u := range units {
		if u.Pct() < hotPct {
			return false
		}
	}
	return true
}

func unitByID(units []game.Unit, id string) *game.Unit {
	for i := range units {
		if units[i].ID == id {
			return &units[i]
		}
	}
	return nil
}

func hotBudgetOK(m map[string]time.Time, cap int) bool {
	n := 0
	for _, t := range m {
		if t.After(time.Now()) {
			n++
		}
	}
	return n < cap
}

func lowest(units []game.Unit) *game.Unit {
	if len(units) == 0 {
		return nil
	}
	best := &units[0]
	pct := best.Pct()
	for i := 1; i < len(units); i++ {
		if units[i].Pct() < pct {
			best = &units[i]
			pct = best.Pct()
		}
	}
	return best
}

func runSelfTest(h *hook.Hook, lastCast *time.Time) {
	fmt.Printf("%s self-test: жду первый кадр...\n", ts())
	for i := 0; i < 200; i++ {
		if f, _ := h.Frames(); f > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	st, err := game.Read(h)
	if err != nil {
		fmt.Printf("%s self-test: нет игры: %v\n", ts(), err)
		return
	}
	m0 := st.Mana
	b0, _ := game.BuffList(h, "player")

	cast(h, "player", rejuvID, lastCast, -1, false)
	time.Sleep(700 * time.Millisecond)
	b1, _ := game.BuffList(h, "player")
	fmt.Printf("%s Rejuvenation: хот=%v мана=%d(было %d)\n", ts(), len(b1) > len(b0), manaNow(h), m0)
	time.Sleep(1100 * time.Millisecond)

	m1 := manaNow(h)
	cast(h, "player", htID, lastCast, -1, true)
	fmt.Printf("%s Healing Touch: мана=%d(было %d)\n", ts(), manaNow(h), m1)
	time.Sleep(1600 * time.Millisecond)

	m2 := manaNow(h)
	cast(h, "player", innervate, lastCast, -1, false)
	time.Sleep(800 * time.Millisecond)
	fmt.Printf("%s Innervate: мана=%d(было %d)\n", ts(), manaNow(h), m2)
	time.Sleep(1100 * time.Millisecond)

	cast(h, "player", rejuvID, lastCast, -1, false)
	time.Sleep(700 * time.Millisecond)
	b2, _ := game.BuffList(h, "player")
	fmt.Printf("%s Rejuvenation повторно: хот=%v\n", ts(), len(b2) > len(b0))
}

func manaNow(h *hook.Hook) int {
	st, err := game.Read(h)
	if err != nil {
		return -1
	}
	return st.Mana
}

func runCastTest(h *hook.Hook) {
	fmt.Printf("%s cast-test: 3 спелла x {по ID, по имени} на себя (НЕ трогай клиент руками)\n", ts())
	for _, id := range []int{htID, regrowthID, rejuvID} {
		for _, mode := range []string{"by_id", "by_name"} {
			m0 := manaNow(h)
			if err := h.Do(`TargetUnit("player")`); err != nil {
				fmt.Printf("%s -- %s/%s: target: %v\n", ts(), spellNames[id], mode, err)
				continue
			}
			if err := h.Do(`SpellStopCasting()`); err != nil {
				fmt.Printf("%s -- %s/%s: stop: %v\n", ts(), spellNames[id], mode, err)
				continue
			}
			var castErr error
			if mode == "by_id" {
				castErr = h.Do(castCode(id))
			} else {
				castErr = h.Do(`GT_V=(function() local n=GetSpellInfo(` + strconv.Itoa(id) + `) if n then CastSpellByName(n) end return tostring(n) end)()`)
			}
			if castErr != nil {
				fmt.Printf("%s -- %s/%s: cast: %v\n", ts(), spellNames[id], mode, castErr)
				continue
			}
			nameUsed := ""
			if mode == "by_name" {
				nameUsed, _ = h.Get("GT_V")
			}
			fmt.Printf("%s -- %s/%s: ждём каст...\n", ts(), spellNames[id], mode)
			deadline := time.Now().Add(3 * time.Second)
			seen := ""
			var mid int
			for time.Now().Before(deadline) {
				st, err := game.Read(h)
				if err == nil {
					if st.Casting != "" {
						seen = st.Casting
					}
					mid = st.Mana
				}
				time.Sleep(200 * time.Millisecond)
			}
			fmt.Printf("%s -- %s/%s: имя(для каста)=%q кастинг=%q старт=%v мана=%d->%d\n",
				ts(), spellNames[id], mode, nameUsed, seen, seen != "", m0, mid)
		}
	}
	fmt.Printf("%s cast-test готово\n", ts())
}

func waitCastStart(h *hook.Hook, max time.Duration) string {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		if st, err := game.Read(h); err == nil && st.Casting != "" {
			return st.Casting
		}
		time.Sleep(100 * time.Millisecond)
	}
	return ""
}

func waitCastDone(h *hook.Hook, max time.Duration) bool {
	deadline := time.Now().Add(max)
	started := false
	for time.Now().Before(deadline) {
		st, err := game.Read(h)
		if err == nil {
			if st.Casting != "" {
				started = true
			} else if started {
				return true
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return started
}

func cast(h *hook.Hook, unit string, spellID int, lastCast *time.Time, pct int, hard bool) bool {
	if err := h.Do(`TargetUnit("` + unit + `")`); err != nil {
		fmt.Printf("%s target %s: %v\n", ts(), unit, err)
		return false
	}
	name := spellNames[spellID]
	doCast := func() error { return h.Do(castCode(spellID)) }
	if hard {
		_ = h.Do(`SpellStopCasting()`)
		if err := doCast(); err != nil {
			fmt.Printf("%s cast %s: %v\n", ts(), name, err)
			return false
		}
		castName := waitCastStart(h, 1800*time.Millisecond)
		if castName == "" {
			_ = h.Do(`SpellStopCasting()`)
			if err := doCast(); err != nil {
				fmt.Printf("%s cast %s: %v\n", ts(), name, err)
				return false
			}
			castName = waitCastStart(h, 1200*time.Millisecond)
		}
		if castName == "" {
			fmt.Printf("%s %s -> %s (hp=%d%%): НЕ ВЫУЧЕН/НЕ СТАРТОВАЛ\n", ts(), name, unit, pct)
			return false
		}
		waitCastDone(h, 6000*time.Millisecond)
		*lastCast = time.Now()
		fmt.Printf("%s %s -> %s (hp=%d%%, каст=%s)\n", ts(), name, unit, pct, castName)
		return true
	}
	mBefore := manaNow(h)
	if err := doCast(); err != nil {
		fmt.Printf("%s cast %s: %v\n", ts(), name, err)
		return false
	}
	time.Sleep(600 * time.Millisecond)
	mAfter := manaNow(h)
	*lastCast = time.Now()
	if mBefore > 0 && mAfter >= 0 && mAfter >= mBefore {
		fmt.Printf("%s %s -> %s (hp=%d%%): НЕ ВЫУЧЕН (мана без изменений)\n", ts(), name, unit, pct)
		return false
	}
	if pct >= 0 {
		fmt.Printf("%s %s -> %s (hp=%d%%)\n", ts(), name, unit, pct)
	} else {
		fmt.Printf("%s %s -> %s\n", ts(), name, unit)
	}
	return true
}

func castCode(id int) string {
	return fmt.Sprintf(`local __n = GetSpellInfo(%d) if __n then CastSpellByName(__n) end`, id)
}

// castBuff кастует мгновенный бафф-дефенсив и проверяет появление по иконке.
func castBuff(h *hook.Hook, unit string, spellID int, lastCast *time.Time) bool {
	name := spellNames[spellID]
	if err := h.Do(`TargetUnit("` + unit + `")`); err != nil {
		fmt.Printf("%s target %s: %v\n", ts(), unit, err)
		return false
	}
	if err := h.Do(castCode(spellID)); err != nil {
		fmt.Printf("%s cast %s: %v\n", ts(), name, err)
		return false
	}
	time.Sleep(700 * time.Millisecond)
	*lastCast = time.Now()
	ok, _ := game.UnitHasBuff(h, unit, spellID)
	if !ok {
		fmt.Printf("%s %s -> %s: НЕ ВЫУЧЕН (баф не появился)\n", ts(), name, unit)
		return false
	}
	fmt.Printf("%s %s -> %s\n", ts(), name, unit)
	return true
}

func hideConsole() {
	kc := syscall.NewLazyDLL("kernel32.dll")
	uc := syscall.NewLazyDLL("user32.dll")
	hwnd, _, _ := kc.NewProc("GetConsoleWindow").Call()
	if hwnd == 0 {
		return
	}
	uc.NewProc("ShowWindow").Call(hwnd, 0)
}

func ts() string {
	return time.Now().Format("15:04:05.000")
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FAIL: "+format+"\n", args...)
	os.Exit(1)
}