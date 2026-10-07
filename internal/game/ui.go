package game

import (
	"fmt"
	"strings"

	"gotrain/internal/hook"
)

const uiInitLua = `
GT_UI_INIT = function()
  if GT_UI_frame then
    GT_UI_frame:Hide()
    GT_UI_frame:SetParent(nil)
    GT_UI_frame=nil
  end
  local f=CreateFrame("Frame",nil,UIParent)
  f:SetSize(230,44)
  f:SetPoint("TOPLEFT",UIParent,"TOPLEFT",12,-12)
  f:SetMovable(true)
  f:EnableMouse(true)
  f:RegisterForDrag("LeftButton")
  f:SetScript("OnDragStart",function(self) self:StartMoving() end)
  f:SetScript("OnDragStop",function(self) self:StopMovingOrSizing() end)
  f:SetFrameStrata("DIALOG")
  f:SetBackdrop({bgFile="Interface\\Buttons\\WHITE8X8",edgeFile="Interface\\Buttons\\WHITE8X8",tile=true,tileSize=8,edgeSize=1,insets={left=2,right=2,top=2,bottom=2}})
  f:SetBackdropColor(0,0,0,0.72)
  f:SetBackdropBorderColor(0.6,0.5,0,0.8)

  local healf=CreateFrame("CheckButton",nil,f,"UICheckButtonTemplate")
  healf:SetSize(24,24)
  healf:SetPoint("TOPLEFT",8,-8)
  healf:SetHitRectInsets(0,-40,0,0)
  healf:SetScript("OnClick",function(self)
    GT_ON=not GT_ON
    self:SetChecked(GT_ON==true)
  end)
  local hl=healf:CreateFontString(nil,"OVERLAY","GameFontNormal")
  hl:SetPoint("LEFT",healf,"RIGHT",4,0)
  hl:SetText("Хил")

  local bufff=CreateFrame("CheckButton",nil,f,"UICheckButtonTemplate")
  bufff:SetSize(24,24)
  bufff:SetPoint("LEFT",hl,"RIGHT",10,0)
  bufff:SetHitRectInsets(0,-40,0,0)
  bufff:SetScript("OnClick",function(self)
    GT_BUFF=not GT_BUFF
    self:SetChecked(GT_BUFF==true)
  end)
  local bl=bufff:CreateFontString(nil,"OVERLAY","GameFontNormal")
  bl:SetPoint("LEFT",bufff,"RIGHT",4,0)
  bl:SetText("Бафы")

  local formf=CreateFrame("CheckButton",nil,f,"UICheckButtonTemplate")
  formf:SetSize(24,24)
  formf:SetPoint("LEFT",bl,"RIGHT",10,0)
  formf:SetHitRectInsets(0,-40,0,0)
  formf:SetScript("OnClick",function(self)
    GT_FORM=not GT_FORM
    self:SetChecked(GT_FORM==true)
  end)
  local fl=formf:CreateFontString(nil,"OVERLAY","GameFontNormal")
  fl:SetPoint("LEFT",formf,"RIGHT",4,0)
  fl:SetText("Облик")

  GT_UI_frame=f
  GT_heal=healf
  GT_buff=bufff
  GT_formchk=formf
  GT_ON=true
  GT_BUFF=true
  GT_FORM=true
  GT_LASTUP=GetTime()
  healf:SetChecked(true)
  bufff:SetChecked(true)
  formf:SetChecked(true)
  f:SetScript("OnUpdate",function(self)
    if GetTime()-GT_LASTUP>3 then self:Hide() end
  end)
end
GT_UI_INIT()
`

// UIDestroy убирает фрейм и чекбоксы из клиента.
func UIDestroy(h *hook.Hook) error {
	return h.Do(`if GT_UI_frame then GT_UI_frame:Hide() GT_UI_frame:SetParent(nil) GT_UI_frame=nil end GT_heal=nil GT_buff=nil GT_formchk=nil`)
}

// UIInit создаёт окно с чекбоксами в игре (идемпотентно).
func UIInit(h *hook.Hook) error {
	return h.Do(uiInitLua)
}

// UIUpdate синхронизирует чекбоксы с переменными (без текстовой панели).
func UIUpdate(h *hook.Hook) error {
	return h.Do(`GT_LASTUP=GetTime() if GT_heal then GT_heal:SetChecked(GT_ON==true) end if GT_buff then GT_buff:SetChecked(GT_BUFF==true) end if GT_formchk then GT_formchk:SetChecked(GT_FORM==true) end`)
}

// UnitHasBuff проверяет наличие бафа по иконке спелла на юните.
func UnitHasBuff(h *hook.Hook, unit string, spellID int) (bool, error) {
	code := fmt.Sprintf(`GT_V=(function(spellID)
  local _,_,icon=GetSpellInfo(spellID)
  if not icon then return "true" end
  local ok,v=pcall(function()
    for i=1,32 do
      local _,_,tex=UnitBuff(%q,i)
      if tex and tex==icon then return true end
    end
    return false
  end)
  if not ok then return "true" end
  return tostring(v)
end)(%d)`, unit, spellID)
	if err := h.Do(code); err != nil {
		return false, err
	}
	v, err := h.Get("GT_V")
	if err != nil {
		return false, err
	}
	return v == "true", nil
}

// UIState возвращает состояние чекбоксов (Хил, Бафы, Облик).
func UIState(h *hook.Hook) (heal, buff, form bool, err error) {
	heal, buff, form = true, true, true
	if err = h.Do(`GT_V=tostring(GT_ON==true).."#"..tostring(GT_BUFF==true).."#"..tostring(GT_FORM==true)`); err != nil {
		return heal, buff, form, err
	}
	v, gerr := h.Get("GT_V")
	if gerr != nil {
		return heal, buff, form, gerr
	}
	parts := strings.Split(v, "#")
	if len(parts) > 0 {
		heal = strings.TrimSpace(parts[0]) == "true"
	}
	if len(parts) > 1 {
		buff = strings.TrimSpace(parts[1]) == "true"
	}
	if len(parts) > 2 {
		form = strings.TrimSpace(parts[2]) == "true"
	}
	return heal, buff, form, nil
}

// BuffMissing возвращает юнитов без бафа, соответствующего иконке спелла.
// В бою и при отсутствии целевых юнитов возвращает пустой список.
func BuffMissing(h *hook.Hook, spellID int) ([]string, error) {
	code := fmt.Sprintf(`GT_V=(function(spellID)
  local _,_,icon=GetSpellInfo(spellID)
  if UnitAffectingCombat("player") then return "" end
  local function has(unit)
    if not icon then return true end
    for i=1,32 do
      local _,_,tex=UnitBuff(unit,i)
      if tex and tex==icon then return true end
    end
    return false
  end
  local ok,v=pcall(function()
    local out={}
    local function add(u) if UnitName(u) and not has(u) then out[#out+1]=u end end
    add("player")
    for i=1,4 do add("party"..i) end
    return table.concat(out,",")
  end)
  if not ok then return "" end
  return v
end)(%d)`, spellID)
	if err := h.Do(code); err != nil {
		return nil, err
	}
	v, err := h.Get("GT_V")
	if err != nil {
		return nil, err
	}
	if v == "" {
		return nil, nil
	}
	var out []string
	for _, u := range strings.Split(v, ",") {
		if u != "" {
			out = append(out, u)
		}
	}
	return out, nil
}