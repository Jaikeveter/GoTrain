package hook

import (
	"fmt"
	"strings"

	"gotrain/internal/winproc"
)

// RepairedCaves чинит vtable-слоты, в которых остался наш старый кейв
// от оборванной сессии бота: вычитывает оригинал из самого кейва
// (финальный Jmp32 указывает на исходный EndScene/Present в d3d9) и
// возвращает слот к исходному адресу. Возвращает описание действий.
func RepairedCaves(p *winproc.Process) string {
	mods, _ := p.Modules()
	cands, rerr := Resolve(p)
	if rerr != nil || len(cands) == 0 || mods == nil {
		return ""
	}
	var parts []string
	for _, c := range cands {
		for _, s := range []struct {
			slot, held uintptr
		}{
			{c.SlotES, c.OrigES},
			{c.SlotPR, c.OrigPR},
		} {
			if s.held == 0 {
				continue
			}
			if winproc.FindModule(mods, s.held) != nil {
				continue
			}
			orig := scanCaveOriginal(p, s.held, mods)
			if orig == 0 {
				continue
			}
			if err := patchSlotAddr(p, s.slot, orig); err != nil {
				parts = append(parts, fmt.Sprintf("slot=0x%X repair: %v", s.slot, err))
				continue
			}
			parts = append(parts, fmt.Sprintf("slot=0x%X (cave=0x%X) -> orig=0x%X", s.slot, s.held, orig))
		}
	}
	return strings.Join(parts, "; ")
}

// scanCaveOriginal читает старый кейв и ищет финальный Jmp rel32,
// цель которого лежит в d3d9 — это исходный адрес, который был в слоте
// до нашего патча.
func scanCaveOriginal(p *winproc.Process, cave uintptr, mods []winproc.Module) uintptr {
	buf := make([]byte, 0x200)
	if err := p.Read(cave, buf); err != nil {
		return 0
	}
	if buf[0] != 0x60 { // pushad — пролог нашего кейва
		return 0
	}
	for i := 0; i+4 < len(buf); i++ {
		if buf[i] != 0xE9 { // Jmp rel32
			continue
		}
		rel := int32(buf[i+1]) | int32(buf[i+2])<<8 | int32(buf[i+3])<<16 | int32(buf[i+4])<<24
		tgt := cave + uintptr(i) + 5 + uintptr(int32(rel))
		if tgt == cave {
			continue
		}
		m := winproc.FindModule(mods, tgt)
		if m == nil {
			continue
		}
		if strings.Contains(strings.ToLower(m.BaseName()), "d3d9") {
			return tgt
		}
	}
	return 0
}