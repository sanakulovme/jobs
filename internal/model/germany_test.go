package model

import "testing"

func TestGermanStates(t *testing.T) {
	cases := []struct{ loc, country, state string }{
		{"Potsdam, Brandenburg, Germany", "Germany", "Brandenburg"},
		{"Zossen bei Berlin, BRANDENBURG, Germany", "Germany", "Brandenburg"},
		{"Köln, NORDRHEIN-WESTFALEN, Germany", "Germany", "Nordrhein-Westfalen"},
		{"Berlin", "Germany", "Berlin"},
		{"Munich, Germany", "Germany", "Bayern"},
		{"Teltow", "Germany", "Brandenburg"},
		{"Frankfurt (Oder)", "Germany", "Brandenburg"},
		{"Frankfurt am Main, Germany", "Germany", "Hessen"},
		{"Stuttgart, Baden-Württemberg, Deutschland", "Germany", "Baden-Württemberg"},
		// Still US/Canada as before.
		{"Austin, TX", "United States", "Texas"},
	}
	for _, c := range cases {
		country, _, state := ResolveLocations(c.loc)
		if country != c.country || state != c.state {
			t.Errorf("ResolveLocations(%q) = %q/%q, want %q/%q", c.loc, country, state, c.country, c.state)
		}
	}
}

func TestGermanCity(t *testing.T) {
	cases := map[string]string{
		"Zossen bei Berlin, Brandenburg, Germany": "Zossen",
		"Hoppegarten (Mark), Brandenburg":         "Hoppegarten",
		"Munich, Germany":                         "München",
		"Berlin":                                  "Berlin",
		"Germany":                                 "",
		"Bayern, Germany":                         "",
		"Nauen, Havelland":                        "Nauen",
	}
	for loc, want := range cases {
		if got := GermanCity(loc); got != want {
			t.Errorf("GermanCity(%q) = %q, want %q", loc, got, want)
		}
	}
}

func TestGermanStateName(t *testing.T) {
	if got := GermanStateName("MECKLENBURG-VORPOMMERN"); got != "Mecklenburg-Vorpommern" {
		t.Errorf("got %q", got)
	}
	if got := GermanStateName("Ausland"); got != "" {
		t.Errorf("got %q", got)
	}
}
