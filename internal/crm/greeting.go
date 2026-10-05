package crm

import (
	"fmt"
	"strings"

	"faangjobs/internal/model"
)

// Greeting is the German salutation an application to job opens with,
// derived the same way the bundesagentur adapter extracts a contact person:
// "Sehr geehrte{r} {Salutation} {Name}," when the posting named one, else the
// generic "Sehr geehrte Damen und Herren,".
func Greeting(job model.Job) string {
	if job.ContactPerson == "" {
		return "Sehr geehrte Damen und Herren,"
	}
	suffix := ""
	if strings.EqualFold(job.Salutation, "Herr") {
		suffix = "r"
	}
	return fmt.Sprintf("Sehr geehrte%s %s %s,", suffix, job.Salutation, job.ContactPerson)
}
