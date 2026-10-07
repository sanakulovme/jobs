package source

import (
	"net/url"
	"strings"
	"testing"
)

func TestPageText(t *testing.T) {
	base, _ := url.Parse("https://praxis.example/karriere/")
	src := `<html><head><title>x</title><script>var a = "<b>";</script></head><body>
<nav><a href="/">Start</a></nav>
<h2>Offene Stellen</h2>
<ul>
 <li><a href="mfa-teilzeit.html">MFA (m/w/d) &amp; Empfang</a> – Berlin</li>
 <li><a href='https://jobs.example/42'>Azubi&nbsp;ZFA</a></li>
</ul>
<p>Bewerbung an <a href="mailto:bewerbung@praxis.example?subject=Job">hier</a></p>
<a href="javascript:void(0)">Cookies</a><style>.x{}</style>
</body></html>`
	got := PageText(src, base)

	for _, want := range []string{
		"Offene Stellen",
		"MFA (m/w/d) & Empfang [link: https://praxis.example/karriere/mfa-teilzeit.html] – Berlin",
		"Azubi ZFA [link: https://jobs.example/42]",
		"bewerbung@praxis.example",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"var a", "<", ".x{}", "javascript", "subject=Job"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("should not contain %q:\n%s", unwanted, got)
		}
	}
}

func TestPickApplicationEmail(t *testing.T) {
	cases := map[string]string{
		"Datenschutz: datenschutz@x.de. Fragen: info@x.de, Bewerbung: karriere@x.de": "karriere@x.de",
		"noreply@x.de oder info@praxis-mueller.de.":                                  "info@praxis-mueller.de",
		"nur datenschutz@x.de": "",
		"keine Adresse":        "",
	}
	for text, want := range cases {
		if got := PickApplicationEmail(text, nil); got != want {
			t.Errorf("PickApplicationEmail(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestPickApplicationEmailExcludes(t *testing.T) {
	text := "Kontakt: kontakt@board.example – Bewerbung an mueller@praxis.example"
	if got := PickApplicationEmail(text, map[string]bool{"kontakt@board.example": true}); got != "mueller@praxis.example" {
		t.Errorf("got %q", got)
	}
}
