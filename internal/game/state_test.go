package game

import "testing"

func TestParseSimple(t *testing.T) {
	st, err := parse("261/261;320;320;0;")
	if err != nil {
		t.Fatal(err)
	}
	if st.Casting != "" {
		t.Fatalf("casting should be empty, got %q", st.Casting)
	}
	if len(st.Units) != 1 || st.Units[0].ID != "player" || st.Units[0].HP != 261 {
		t.Fatalf("units: %+v", st.Units)
	}
	if st.Mana != 320 || st.ManaMax != 320 || st.Form != 0 {
		t.Fatalf("mana=%d/%d form=%d", st.Mana, st.ManaMax, st.Form)
	}
	if st.ManaPct() != 100 {
		t.Fatalf("manapct %v", st.ManaPct())
	}
	st2, err := parse("261/261;320;320;0;Healing Touch")
	if err != nil {
		t.Fatal(err)
	}
	if st2.Casting != "Healing Touch" {
		t.Fatalf("casting should be 'Healing Touch', got %q", st2.Casting)
	}
}

func TestParseParty(t *testing.T) {
	st, err := parse("241/241;180/200;150/300;290;320;0;")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Units) != 3 {
		t.Fatalf("units=%d", len(st.Units))
	}
	if st.Units[2].ID != "party2" || st.Units[2].HP != 150 || st.Units[2].Max != 300 {
		t.Fatalf("party2: %+v", st.Units[2])
	}
	if got := st.Units[2].Pct(); got != 50 {
		t.Fatalf("pct=%v", got)
	}
}

func TestParseBad(t *testing.T) {
	for _, s := range []string{"", "261/261;x;y;0;", "261/261;290;320;0", "abc", "261/261;290;320"} {
		if _, err := parse(s); err == nil {
			t.Fatalf("want error for %q", s)
		}
	}
}