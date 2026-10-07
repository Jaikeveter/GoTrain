package wow

import "testing"

func TestVARebase(t *testing.T) {
	if got := VA(0x400000, StaticClientConnection); got != StaticClientConnection {
		t.Fatalf("base 0x400000: got 0x%X", got)
	}
	if got := VA(0x500000, StaticClientConnection); got != 0x500000+StaticClientConnection-0x400000 {
		t.Fatalf("rebased: got 0x%X", got)
	}
}

func TestLuaGlobalsIndex(t *testing.T) {
	u := uint32(LuaGlobalsIndex)
	if int32(u) != -10002 {
		t.Fatalf("LUA_GLOBALSINDEX = %d, want -10002", int32(u))
	}
}
