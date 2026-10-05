package crm

import (
	"testing"

	"faangjobs/internal/model"
)

func TestGreeting(t *testing.T) {
	cases := []struct {
		job  model.Job
		want string
	}{
		{model.Job{}, "Sehr geehrte Damen und Herren,"},
		{model.Job{ContactPerson: "Mustermann", Salutation: "Herr"}, "Sehr geehrter Herr Mustermann,"},
		{model.Job{ContactPerson: "Schmidt", Salutation: "Frau"}, "Sehr geehrte Frau Schmidt,"},
	}
	for _, c := range cases {
		if got := Greeting(c.job); got != c.want {
			t.Errorf("Greeting(%+v) = %q, want %q", c.job, got, c.want)
		}
	}
}
