// Command mfaexport reads a crawled company's jobs from the FaangJobs data
// store and writes one CSV row per job matching a specific external MySQL
// table schema (source, external_id, title, employer, city, ...). All the
// heavy lifting (fetching, per-job detail calls, field extraction) already
// happened in the "bundesagentur" crawler adapter (internal/source); this
// tool just projects model.Job onto the target column set.
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"log"
	"os"

	"faangjobs/internal/model"
	"faangjobs/internal/store"
)

var csvHeader = []string{
	"source", "external_id", "title", "employer", "city", "medical_specialty",
	"application_email", "reference_number", "contact_person", "salutation",
	"keywords", "required_german_level", "requires_drivers_license",
	"requires_own_car", "requires_german_mfa_training", "required_qualifications",
	"source_url", "website", "application_portal", "description", "main_duties",
	"mandatory_requirements", "preferred_requirements",
}

func main() {
	data := flag.String("data", "./data", "data directory to read")
	companyID := flag.String("company", "bundesagentur-ba-mfa-de", "company id to export")
	out := flag.String("out", "./data/mfa_jobs_export.csv", "output CSV path")
	flag.Parse()

	st, err := store.New(*data)
	if err != nil {
		log.Fatalf("open data dir: %v", err)
	}
	res, err := st.ReadCompany(*companyID)
	if err != nil {
		log.Fatalf("read company %q: %v (run the crawler with -jobs all first)", *companyID, err)
	}

	if err := writeCSV(*out, res.Jobs); err != nil {
		log.Fatalf("write csv: %v", err)
	}
	log.Printf("wrote %d rows to %s", len(res.Jobs), *out)
}

func toRecord(j model.Job) []string {
	return []string{
		"arbeitsagentur.de", j.ReferenceNumber, j.Title, j.Company, j.Location, j.MedicalSpecialty,
		j.ApplicationEmail, j.ReferenceNumber, j.ContactPerson, j.Salutation,
		keywords(j), j.RequiredGermanLevel, boolStr(j.RequiresDriversLicense),
		boolStr(j.RequiresOwnCar), boolStr(j.RequiresGermanMFATraining), qualificationsJSON(j.RequiredQualifications),
		j.URL, j.Website, j.ApplicationPortal, j.Description, j.MainDuties,
		j.MandatoryRequirements, j.PreferredRequirements,
	}
}

func keywords(j model.Job) string {
	parts := []string{"MFA"}
	if j.MedicalSpecialty != "" {
		parts = append(parts, j.MedicalSpecialty)
	}
	if j.Location != "" {
		parts = append(parts, j.Location)
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += ", " + p
	}
	return out
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func qualificationsJSON(items []string) string {
	if len(items) == 0 {
		return ""
	}
	b, _ := json.Marshal(items)
	return string(b)
}

func writeCSV(path string, jobs []model.Job) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write(csvHeader); err != nil {
		return err
	}
	for _, j := range jobs {
		if j.Title == "" {
			continue
		}
		if err := w.Write(toRecord(j)); err != nil {
			return err
		}
	}
	return w.Error()
}
