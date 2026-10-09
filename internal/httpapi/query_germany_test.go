package httpapi

import (
	"testing"

	"faangjobs/internal/model"
	"faangjobs/internal/store"
)

func germanSnapshot() *snapshot {
	results := []store.CompanyResult{
		{CompanyID: "bundesagentur-mfa-ondemand", OK: true, Jobs: []model.Job{
			{ID: "ba~1", Title: "MFA", Location: "Berlin, Berlin, Germany", Source: "bundesagentur", URL: "u1"},
			{ID: "ba~2", Title: "MFA", Location: "Potsdam, Brandenburg, Germany", Source: "bundesagentur", URL: "u2"},
			{ID: "ba~3", Title: "MFA", Location: "Zossen bei Berlin, Brandenburg, Germany", Source: "bundesagentur", URL: "u3"},
		}},
		{CompanyID: "site-medi-karriere-de", OK: true, Jobs: []model.Job{
			{ID: "site~1", Title: "MFA", Location: "München, Bayern, Germany", Source: "site", Website: "https://www.medi-karriere.de", URL: "u4"},
			{ID: "site~2", Title: "MFA", Location: "Berlin", Source: "site", Website: "https://www.medi-karriere.de", URL: "u5"},
		}},
	}
	return buildSnapshot(results, nil)
}

func TestQueryGermanStateAndCity(t *testing.T) {
	s := germanSnapshot()
	if r := s.Run(Query{Country: "Germany"}); r.Total != 5 {
		t.Errorf("all of Germany = %d, want 5", r.Total)
	}
	if r := s.Run(Query{State: "Brandenburg"}); r.Total != 2 {
		t.Errorf("Brandenburg = %d, want 2", r.Total)
	}
	if r := s.Run(Query{City: "Berlin"}); r.Total != 2 {
		t.Errorf("city Berlin = %d, want 2 (Zossen bei Berlin is its own town)", r.Total)
	}
	if r := s.Run(Query{City: "München"}); r.Total != 1 {
		t.Errorf("city München = %d, want 1", r.Total)
	}

	parents := map[string]string{}
	for _, f := range s.cityFacets {
		parents[f.Value] = f.Parent
	}
	if parents["Zossen"] != "Brandenburg" || parents["Berlin"] != "Berlin" || parents["München"] != "Bayern" {
		t.Errorf("city parents = %v", parents)
	}
}

func TestQuerySite(t *testing.T) {
	s := germanSnapshot()
	if r := s.Run(Query{Site: "arbeitsagentur.de"}); r.Total != 3 {
		t.Errorf("arbeitsagentur.de = %d, want 3", r.Total)
	}
	r := s.Run(Query{Site: "medi-karriere.de", City: "Berlin"})
	if r.Total != 1 {
		t.Errorf("medi-karriere.de in Berlin = %d, want 1", r.Total)
	}
	sites := map[string]int{}
	for _, f := range s.Run(Query{}).Facets.Sites {
		sites[f.Value] = f.Count
	}
	if sites["arbeitsagentur.de"] != 3 || sites["medi-karriere.de"] != 2 {
		t.Errorf("site facets = %v", sites)
	}
}
