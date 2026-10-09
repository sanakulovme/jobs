package model

import "strings"

// Germany is tracked one level deeper than other European countries: the
// CRM places candidates into German jobs, so the board drills down
// country → Bundesland → city. Bundesagentur postings carry the Bundesland
// explicitly ("Potsdam, Brandenburg, Germany"); for other sources the city
// table below fills it in.

// deStateNames maps every spelling a location may use (German, English,
// Bundesagentur's upper-case form after normPhrase) to the Bundesland's
// German name.
var deStateNames = map[string]string{}

var deStates = map[string][]string{
	"Baden-Württemberg":      {"baden württemberg", "baden wuerttemberg", "baden wurttemberg"},
	"Bayern":                 {"bayern", "bavaria", "freistaat bayern"},
	"Berlin":                 {"berlin"},
	"Brandenburg":            {"brandenburg"},
	"Bremen":                 {"bremen", "freie hansestadt bremen"},
	"Hamburg":                {"hamburg", "freie und hansestadt hamburg"},
	"Hessen":                 {"hessen", "hesse"},
	"Mecklenburg-Vorpommern": {"mecklenburg vorpommern", "mecklenburg western pomerania"},
	"Niedersachsen":          {"niedersachsen", "lower saxony"},
	"Nordrhein-Westfalen":    {"nordrhein westfalen", "north rhine westphalia", "nrw"},
	"Rheinland-Pfalz":        {"rheinland pfalz", "rhineland palatinate"},
	"Saarland":               {"saarland"},
	"Sachsen":                {"sachsen", "saxony", "freistaat sachsen"},
	"Sachsen-Anhalt":         {"sachsen anhalt", "saxony anhalt"},
	"Schleswig-Holstein":     {"schleswig holstein"},
	"Thüringen":              {"thüringen", "thueringen", "thuringen", "thuringia"},
}

// deCityStates maps German cities to their Bundesland: the larger cities
// plus the commuter towns that show up in Berlin/Brandenburg postings.
var deCityStates = map[string]string{
	// City-states: the city and the Bundesland share a name.
	"berlin": "Berlin", "hamburg": "Hamburg", "bremen": "Bremen", "bremerhaven": "Bremen",
	// Baden-Württemberg
	"stuttgart": "Baden-Württemberg", "karlsruhe": "Baden-Württemberg", "mannheim": "Baden-Württemberg",
	"freiburg": "Baden-Württemberg", "freiburg im breisgau": "Baden-Württemberg", "heidelberg": "Baden-Württemberg",
	"ulm": "Baden-Württemberg", "heilbronn": "Baden-Württemberg", "pforzheim": "Baden-Württemberg",
	"reutlingen": "Baden-Württemberg", "tübingen": "Baden-Württemberg", "esslingen": "Baden-Württemberg",
	"ludwigsburg": "Baden-Württemberg", "konstanz": "Baden-Württemberg", "walldorf": "Baden-Württemberg",
	// Bayern
	"münchen": "Bayern", "muenchen": "Bayern", "munich": "Bayern", "nürnberg": "Bayern", "nuernberg": "Bayern",
	"nuremberg": "Bayern", "augsburg": "Bayern", "regensburg": "Bayern", "ingolstadt": "Bayern",
	"würzburg": "Bayern", "wuerzburg": "Bayern", "fürth": "Bayern", "erlangen": "Bayern",
	"bamberg": "Bayern", "bayreuth": "Bayern", "landshut": "Bayern", "rosenheim": "Bayern",
	"passau": "Bayern", "ismaning": "Bayern",
	// Brandenburg
	"potsdam": "Brandenburg", "cottbus": "Brandenburg", "frankfurt oder": "Brandenburg",
	"oranienburg": "Brandenburg", "falkensee": "Brandenburg", "bernau": "Brandenburg",
	"königs wusterhausen": "Brandenburg", "teltow": "Brandenburg", "zossen": "Brandenburg",
	"strausberg": "Brandenburg", "nauen": "Brandenburg", "eberswalde": "Brandenburg",
	"ludwigsfelde": "Brandenburg", "kleinmachnow": "Brandenburg", "hennigsdorf": "Brandenburg",
	"wildau": "Brandenburg", "ahrensfelde": "Brandenburg", "hoppegarten": "Brandenburg",
	"beelitz": "Brandenburg", "eggersdorf": "Brandenburg", "werder": "Brandenburg",
	"blankenfelde mahlow": "Brandenburg", "schönefeld": "Brandenburg",
	// Hessen
	"frankfurt": "Hessen", "frankfurt am main": "Hessen", "wiesbaden": "Hessen", "kassel": "Hessen",
	"darmstadt": "Hessen", "offenbach": "Hessen", "hanau": "Hessen", "gießen": "Hessen",
	"giessen": "Hessen", "marburg": "Hessen", "fulda": "Hessen", "eschborn": "Hessen",
	// Mecklenburg-Vorpommern
	"rostock": "Mecklenburg-Vorpommern", "schwerin": "Mecklenburg-Vorpommern",
	"neubrandenburg": "Mecklenburg-Vorpommern", "greifswald": "Mecklenburg-Vorpommern",
	"stralsund": "Mecklenburg-Vorpommern", "wismar": "Mecklenburg-Vorpommern",
	// Niedersachsen
	"hannover": "Niedersachsen", "hanover": "Niedersachsen", "braunschweig": "Niedersachsen",
	"oldenburg": "Niedersachsen", "osnabrück": "Niedersachsen", "osnabrueck": "Niedersachsen",
	"wolfsburg": "Niedersachsen", "göttingen": "Niedersachsen", "goettingen": "Niedersachsen",
	"hildesheim": "Niedersachsen", "salzgitter": "Niedersachsen", "lüneburg": "Niedersachsen",
	// Nordrhein-Westfalen
	"köln": "Nordrhein-Westfalen", "koeln": "Nordrhein-Westfalen", "cologne": "Nordrhein-Westfalen",
	"düsseldorf": "Nordrhein-Westfalen", "duesseldorf": "Nordrhein-Westfalen", "dusseldorf": "Nordrhein-Westfalen",
	"dortmund": "Nordrhein-Westfalen", "essen": "Nordrhein-Westfalen", "duisburg": "Nordrhein-Westfalen",
	"bochum": "Nordrhein-Westfalen", "wuppertal": "Nordrhein-Westfalen", "bielefeld": "Nordrhein-Westfalen",
	"bonn": "Nordrhein-Westfalen", "münster": "Nordrhein-Westfalen", "muenster": "Nordrhein-Westfalen",
	"aachen": "Nordrhein-Westfalen", "gelsenkirchen": "Nordrhein-Westfalen", "mönchengladbach": "Nordrhein-Westfalen",
	"krefeld": "Nordrhein-Westfalen", "oberhausen": "Nordrhein-Westfalen", "hagen": "Nordrhein-Westfalen",
	"hamm": "Nordrhein-Westfalen", "leverkusen": "Nordrhein-Westfalen", "paderborn": "Nordrhein-Westfalen",
	"siegen": "Nordrhein-Westfalen", "neuss": "Nordrhein-Westfalen", "solingen": "Nordrhein-Westfalen",
	// Rheinland-Pfalz
	"mainz": "Rheinland-Pfalz", "ludwigshafen": "Rheinland-Pfalz", "koblenz": "Rheinland-Pfalz",
	"trier": "Rheinland-Pfalz", "kaiserslautern": "Rheinland-Pfalz", "worms": "Rheinland-Pfalz",
	// Saarland
	"saarbrücken": "Saarland", "saarbruecken": "Saarland", "neunkirchen": "Saarland", "homburg": "Saarland",
	// Sachsen
	"dresden": "Sachsen", "leipzig": "Sachsen", "chemnitz": "Sachsen", "zwickau": "Sachsen",
	"plauen": "Sachsen", "görlitz": "Sachsen", "bautzen": "Sachsen",
	// Sachsen-Anhalt
	"magdeburg": "Sachsen-Anhalt", "halle": "Sachsen-Anhalt", "halle saale": "Sachsen-Anhalt",
	"dessau": "Sachsen-Anhalt", "dessau roßlau": "Sachsen-Anhalt", "wittenberg": "Sachsen-Anhalt",
	// Schleswig-Holstein
	"kiel": "Schleswig-Holstein", "lübeck": "Schleswig-Holstein", "luebeck": "Schleswig-Holstein",
	"flensburg": "Schleswig-Holstein", "neumünster": "Schleswig-Holstein", "norderstedt": "Schleswig-Holstein",
	// Thüringen
	"erfurt": "Thüringen", "jena": "Thüringen", "gera": "Thüringen", "weimar": "Thüringen",
	"gotha": "Thüringen", "eisenach": "Thüringen",
}

// deCityAliases unifies spellings of the same city for the city facet.
var deCityAliases = map[string]string{
	"munich": "München", "muenchen": "München", "nuremberg": "Nürnberg", "nuernberg": "Nürnberg",
	"cologne": "Köln", "koeln": "Köln", "duesseldorf": "Düsseldorf", "dusseldorf": "Düsseldorf",
	"frankfurt": "Frankfurt am Main", "hanover": "Hannover", "muenster": "Münster",
	"wuerzburg": "Würzburg", "goettingen": "Göttingen", "osnabrueck": "Osnabrück",
	"saarbruecken": "Saarbrücken", "luebeck": "Lübeck", "giessen": "Gießen",
}

var deStateCodes = map[string]string{} // no two-letter codes: "by"/"be" are too ambiguous in free text

func init() {
	for name, spellings := range deStates {
		deStateNames[normPhrase(name)] = name
		for _, s := range spellings {
			deStateNames[normPhrase(s)] = name
		}
	}
	// So "Teltow" or "Potsdam" alone still resolves to Germany.
	for city := range deCityStates {
		addFirst(cityCountry, city, "Germany")
	}
}

// GermanCity returns the city a German job location names — its first
// segment, without the "bei <bigger town>" suffix or a parenthesised note,
// spelled consistently ("Munich" → "München") — or "" if loc names none.
func GermanCity(loc string) string {
	segs := splitLoc(loc) // also splits off "(Mark)"-style notes
	if len(segs) == 0 {
		return ""
	}
	city := segs[0]
	if i := strings.Index(strings.ToLower(city), " bei "); i > 0 {
		city = city[:i]
	}
	city = strings.TrimSpace(city)
	key := normPhrase(city)
	if key == "" || key == "germany" || key == "deutschland" || deStateNames[key] != "" && deCityStates[key] == "" {
		return "" // a bare country or Bundesland, not a city
	}
	if alias, ok := deCityAliases[key]; ok {
		return alias
	}
	return city
}

// GermanStateName returns the Bundesland for a spelling such as
// Bundesagentur's "NORDRHEIN-WESTFALEN", or "" if s names none.
func GermanStateName(s string) string { return deStateNames[normPhrase(s)] }
