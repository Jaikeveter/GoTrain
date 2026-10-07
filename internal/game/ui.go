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
  f:SetSize(280,180)
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
  f.ver=2

  local healf=CreateFrame("CheckButton",nil,f,"UICheckButtonTemplate")
  healf:SetSize(24,24)
  healf:SetPoint("TOPLEFT",10,-8)
  healf:SetScript("OnClick",function(self) GT_ON=self:GetChecked() end)
  local hl=healf:CreateFontString(nil,"OVERLAY","GameFontNormal")
  hl:SetPoint("LEFT",healf,"RIGHT",4,0)
  hl:SetText("Хил")

  local bufff=CreateFrame("CheckButton",nil,f,"UICheckButtonTemplate")
  bufff:SetSize(24,24)
  bufff:SetPoint("LEFT",healf,"RIGHT",80,0)
  bufff:SetScript("OnClick",function(self) GT_BUFF=self:GetChecked() end)
  local bl=bufff:CreateFontString(nil,"OVERLAY","GameFontNormal")
  bl:SetPoint("LEFT",bufff,"RIGHT",4,0)
  bl:SetText("Бафы")

  local t=f:CreateFontString(nil,"OVERLAY","GameFontNormal")
  t:SetPoint("TOPLEFT",f,"TOPLEFT",10,-40)
  t:SetPoint("BOTTOMRIGHT",f,"BOTTOMRIGHT",-10,6)
  t:SetJustifyH("LEFT")
  t:SetJustifyV("TOP")
  f.text=t
  GT_UI_frame=f
  GT_ON=true
  GT_BUFF=true
  GT_LASTUP=GetTime()
  healf:SetChecked(true)
  bufff:SetChecked(true)
  f:SetScript("OnUpdate",function(self)
    if GetTime()-GT_LASTUP>3 then self:Hide() end
  end)
  t:SetText("HealBot: START")
end
GT_UI_INIT()
`

// UIDestroy убирает фрейм из клиента.
func UIDestroy(h *hook.Hook) error {
	return h.Do(`if GT_UI_frame then GT_UI_frame:Hide() GT_UI_frame:SetParent(nil) GT_UI_frame=nil end`)
}

// UIInit создаёт окно с чекбоксами в игре (идемпотентно).
func UIInit(h *hook.Hook) error {
	return h.Do(uiInitLua)
}

// UIUpdate обновляет текст фрейма и отмечает факт работы бота.
func UIUpdate(h *hook.Hook, text string) error {
	return h.Do(fmt.Sprintf("GT_LASTUP=GetTime() GT_TXT=[==[%s]==] local f=GT_UI_frame if f then f.text:SetText(GT_TXT) end", text))
}

// UIState возвращает состояние чекбоксов (Хил, Бафы).
func UIState(h *hook.Hook) (heal, buff bool, err error) {
	heal, buff = true, true
	if err = h.Do(`GT_V=tostring(GT_ON==true).."#"..tostring(GT_BUFF==true)`); err != nil {
		return heal, buff, err
	}
	v, gerr := h.Get("GT_V")
	if gerr != nil {
		return heal, buff, gerr
	}
	parts := strings.Split(v, "#")
	heal = strings.TrimSpace(parts[0]) == "true"
	if len(parts) > 1 {
		buff = strings.TrimSpace(parts[1]) == "true"
	}
	return heal, buff, nil
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