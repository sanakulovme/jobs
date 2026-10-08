package source

import (
	"slices"
	"testing"
)

func TestAusbildungTagComesFromTitleOnly(t *testing.T) {
	// A qualified-staff job whose description requires a finished Ausbildung
	// is not an apprenticeship.
	got := ClassifySpecialties("Medizinische Fachangestellte (m/w/d)",
		"Sie haben eine abgeschlossene Ausbildung als MFA und Erfahrung in der Kardiologie.")
	if slices.Contains(got, "ausbildung") {
		t.Errorf("job requiring a finished Ausbildung tagged as one: %v", got)
	}
	if !slices.Contains(got, "mfa") || !slices.Contains(got, "kardiologie") {
		t.Errorf("description-based tags should still apply: %v", got)
	}

	for _, title := range []string{"Ausbildung zur MFA 2027", "Azubi Zahnmedizinische Fachangestellte", "Auszubildende Pflegefachfrau"} {
		if got := ClassifySpecialties(title, ""); !slices.Contains(got, "ausbildung") {
			t.Errorf("%q should be tagged ausbildung, got %v", title, got)
		}
	}
}
