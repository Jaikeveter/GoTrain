package game

import (
	"fmt"
	"strconv"
	"strings"

	"gotrain/internal/hook"
)

type Unit struct {
	ID  string
	HP  int
	Max int
}

func (u Unit) Pct() float64 {
	if u.Max <= 0 {
		return 100
	}
	return float64(u.HP) * 100 / float64(u.Max)
}

type State struct {
	Units   []Unit
	Mana    int
	ManaMax int
	Form    int
	Casting string
}

func (s *State) ManaPct() float64 {
	if s.ManaMax <= 0 {
		return 100
	}
	return float64(s.Mana) * 100 / float64(s.ManaMax)
}

const stateLua = `(function()
  local parts = {}
  local function addUnit(u)
    local n = UnitName(u)
    if not n then return false end
    parts[#parts+1] = tostring(UnitHealth(u)) .. "/" .. tostring(UnitHealthMax(u))
    return true
  end
  local ok = addUnit("player")
  if not ok then return "" end
  for i = 1, 4 do
    addUnit("party" .. i)
  end
  parts[#parts+1] = tostring(UnitPower("player"))
  parts[#parts+1] = tostring(UnitPowerMax("player"))
  parts[#parts+1] = tostring(GetShapeshiftForm(false))
  parts[#parts+1] = (UnitCastingInfo("player") or "")
  return table.concat(parts, ";")
end)()`

func Read(h *hook.Hook) (*State, error) {
	if err := h.Do("GT_V=" + stateLua); err != nil {
		return nil, err
	}
	s, err := h.Get("GT_V")
	if err != nil {
		return nil, err
	}
	if s == "" {
		return nil, fmt.Errorf("player not in game")
	}
	return parse(s)
}

func parse(s string) (*State, error) {
	parts := strings.Split(s, ";")
	unitCount := len(parts) - 4
	if unitCount < 1 {
		return nil, fmt.Errorf("state malformed: %q", s)
	}
	st := &State{Units: make([]Unit, 0, unitCount)}
	for i := 0; i < unitCount; i++ {
		id := "player"
		if i > 0 {
			id = "party" + strconv.Itoa(i)
		}
		hp, mx, err := parsePair(parts[i])
		if err != nil {
			return nil, fmt.Errorf("unit %s: %w", id, err)
		}
		st.Units = append(st.Units, Unit{ID: id, HP: hp, Max: mx})
	}
	var err error
	if st.Mana, err = strconv.Atoi(parts[unitCount]); err != nil {
		return nil, fmt.Errorf("mana: %w", err)
	}
	if st.ManaMax, err = strconv.Atoi(parts[unitCount+1]); err != nil {
		return nil, fmt.Errorf("manaMax: %w", err)
	}
	st.Form, _ = strconv.Atoi(parts[unitCount+2])
	if unitCount+3 < len(parts) {
		st.Casting = parts[unitCount+3]
	}
	return st, nil
}

func parsePair(s string) (int, int, error) {
	i := strings.IndexByte(s, '/')
	if i < 0 {
		return 0, 0, fmt.Errorf("bad pair %q", s)
	}
	hp, err := strconv.Atoi(s[:i])
	if err != nil {
		return 0, 0, err
	}
	mx, err := strconv.Atoi(s[i+1:])
	if err != nil {
		return 0, 0, err
	}
	return hp, mx, nil
}

func (s *State) String() string {
	var b strings.Builder
	b.WriteString("hp[")
	sep := ""
	for _, u := range s.Units {
		fmt.Fprintf(&b, "%s%s:%d/%d", sep, u.ID, u.HP, u.Max)
		sep = " "
	}
	fmt.Fprintf(&b, "] mana=%d/%d form=%d", s.Mana, s.ManaMax, s.Form)
	return b.String()
}

func BuffList(h *hook.Hook, unit string) ([]string, error) {
	code := `(function()
  local t = {}
  for i = 1, 40 do
    local n = UnitBuff("` + unit + `", i)
    if not n then break end
    t[#t+1] = n
  end
  return table.concat(t, "|")
end)()`
	if err := h.Do("GT_V=" + code); err != nil {
		return nil, err
	}
	s, err := h.Get("GT_V")
	if err != nil {
		return nil, err
	}
	if s == "" {
		return nil, nil
	}
	return strings.Split(s, "|"), nil
}