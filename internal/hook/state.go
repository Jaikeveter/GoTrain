package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gotrain/internal/winproc"
)

type slotState struct {
	Slot uintptr
	Orig uintptr
	Cave uintptr
}

func statePath(pid uint32) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("gotrain-hook-%d.json", pid))
}

// SaveState пишет текущие vtable-патчи, чтобы оборванная сессия
// могла быть починена следующим запуском.
func SaveState(pid uint32, slots, origs, ents []uintptr) error {
	ss := make([]slotState, 0, len(slots))
	for i := range slots {
		ss = append(ss, slotState{Slot: slots[i], Orig: origs[i], Cave: ents[i]})
	}
	b, err := json.Marshal(ss)
	if err != nil {
		return err
	}
	return os.WriteFile(statePath(pid), b, 0600)
}

// ClearState удаляет сохранённое состояние после штатного выхода.
func ClearState(pid uint32) {
	_ = os.Remove(statePath(pid))
}

// RestoreStale чинит vtable-слоты, оставшиеся от оборванной сессии бота:
// если слот до сих пор указывает на нашу прошлую пещеру, возвращаем оригинал.
// Позволяет не перезапускать клиент после падения бота.
func RestoreStale(p *winproc.Process) error {
	b, err := os.ReadFile(statePath(p.Pid))
	if err != nil {
		return nil
	}
	var ss []slotState
	if json.Unmarshal(b, &ss) != nil {
		return nil
	}
	var firstErr error
	for _, s := range ss {
		v, rerr := p.ReadU32(s.Slot)
		if rerr != nil {
			continue
		}
		if uintptr(v) == s.Cave {
			if err := patchSlotAddr(p, s.Slot, s.Orig); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}