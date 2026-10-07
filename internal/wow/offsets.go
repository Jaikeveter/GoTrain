package wow

const (
	ImageBase = 0x400000

	FrameScriptExecute = 0x00819210
	LuaStatePtr        = 0x00D3F78C
	LuaGetTop          = 0x0084DBD0
	LuaSetTop          = 0x0084DBF0
	LuaPushString      = 0x0084E350
	LuaToLString       = 0x0084E0E0
	GetFieldByStackKey = 0x0084F3B0

	LuaGlobalsIndex = 0xFFFFD8EE

	StaticClientConnection = 0x00C79CE0
	ObjectManagerOffset    = 0x2ED0
	FirstObjectOffset      = 0xAC
	NextObjectOffset       = 0x3C
	ObjectGuidOffset       = 0x30
	ObjectTypeOffset       = 0x14
	ObjectFieldsOffset     = 0x8

	UnitFieldHealth    = 0x18 * 4
	UnitFieldMaxHealth = 0x20 * 4
	UnitFieldLevel     = 0x36 * 4
	UnitFieldMana      = 0x19 * 4
	UnitFieldMaxMana   = 0x21 * 4
	UnitFieldBytes0    = 0x5C
	UnitFieldFlags     = 0xEC

	AuraTable1     = 0xC50
	AuraCount1     = 0xDD0
	AuraStructSize = 0x18
	AuraSpellID    = 0x8

	CastingSpellID = 0xC08
	ChannelSpellID = 0xC20

	D3DPtr1        = 0x00C5DF88
	D3DStruct2     = 0x397C
	D3DEndSceneOff = 0xA8
	D3DPresentOff  = 0x44

	GetText = 0x00819D40
)

func VA(base, off uintptr) uintptr {
	return base + off - ImageBase
}
