package httpapi

import (
	"testing"

	"faangjobs/internal/ai"
)

func TestSiteChromeOnJobBoard(t *testing.T) {
	listing := "Jobs in Berlin\nMFA [link: https://board.example/job/1]\nZFA [link: https://board.example/job/2]\nkontakt@board.example\nImpressum"
	jobs := []ai.PageJob{{Title: "MFA", Employer: "Praxis A"}, {Title: "ZFA", Employer: "Praxis B"}}
	c := siteChrome(listing, jobs)

	if !c.emails["kontakt@board.example"] {
		t.Error("a job board's own address should be excluded")
	}
	got := c.strip("Jobs in Berlin\nMFA bei Praxis A\nBewerbung: a@praxis-a.example\nkontakt@board.example\nImpressum")
	if got != "MFA bei Praxis A\nBewerbung: a@praxis-a.example" {
		t.Errorf("strip = %q", got)
	}
}

func TestSiteChromeKeepsSingleEmployerAddress(t *testing.T) {
	c := siteChrome("Karriere\nMFA\nbewerbung@praxis.example", []ai.PageJob{{Title: "MFA", Employer: "Praxis"}, {Title: "ZFA", Employer: "Praxis"}})
	if c.emails["bewerbung@praxis.example"] {
		t.Error("a single employer's own careers address must stay usable")
	}
}
