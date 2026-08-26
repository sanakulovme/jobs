// Command autoapply runs one auto-apply pass over the CRM's current data:
// it matches every vacancy in -data against every candidate and, in
// test mode (the default), reports what it would send without touching
// Gmail. It is meant to be chained after the daily crawler in one scheduled
// job (see scripts/daily-crawl.sh) so newly-scraped vacancies get matched
// and applied to without a human running the "Hozir boshlash" button by
// hand every day.
//
// SAFETY: -test-mode defaults to true. Sending real applications requires
// BOTH -test-mode=false AND -i-understand-this-sends-real-emails — a single
// flag flip is not enough, on purpose. Do not set both outside of an
// explicit, one-time decision confirmed with whoever owns this deployment;
// this is the only command in the whole repo that can email real employers
// on a candidate's behalf without a person in the loop.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"faangjobs/internal/crm"
	"faangjobs/internal/gmail"
	"faangjobs/internal/httpapi"
	"faangjobs/internal/store"
)

// envOr returns the environment variable's value, or def when unset/empty.
// Duplicated from cmd/server/main.go rather than shared — two three-line
// copies are cheaper than a new internal package for one helper.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// loadDotEnv mirrors cmd/server/main.go's loader so the same .env file
// (Google OAuth client id/secret) works for both binaries without
// duplicating secrets into the scheduler's own environment.
func loadDotEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if n := len(val); n >= 2 && (val[0] == '"' && val[n-1] == '"' || val[0] == '\'' && val[n-1] == '\'') {
			val = val[1 : n-1]
		}
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, val)
		}
	}
}

func main() {
	loadDotEnv(".env")

	var (
		dataDir    = flag.String("data", "./data", "directory the crawler writes vacancies into")
		crmDataDir = flag.String("crm-data", "", "directory for CRM data (candidates, applications, ...); defaults to -data")
		count      = flag.Int("count", 0, "max vacancies to consider this run (0 = the server's default cap)")

		testMode       = flag.Bool("test-mode", true, "dry run: report what would be sent without touching Gmail (SAFE, default)")
		confirmRealRun = flag.Bool("i-understand-this-sends-real-emails", false,
			"required in addition to -test-mode=false before this command will send anything through a real employer's inbox")

		googleClientID     = flag.String("google-client-id", envOr("FAANGJOBS_GOOGLE_CLIENT_ID", ""), "Google OAuth client ID (empty = Gmail integration disabled)")
		googleClientSecret = flag.String("google-client-secret", envOr("FAANGJOBS_GOOGLE_CLIENT_SECRET", ""), "Google OAuth client secret")
		googleRedirectURL  = flag.String("google-redirect-url", envOr("FAANGJOBS_GOOGLE_REDIRECT_URL", ""), "OAuth redirect URI, must match the server's")
	)
	flag.Parse()

	logger := log.New(os.Stderr, "", log.LstdFlags)

	if !*testMode && !*confirmRealRun {
		logger.Fatalf("refusing to run with -test-mode=false unless -i-understand-this-sends-real-emails is also set — " +
			"this would send real applications through candidates' Gmail accounts to real employers")
	}

	crmDir := *crmDataDir
	if crmDir == "" {
		crmDir = *dataDir
	}
	crmStore, err := crm.New(crmDir)
	if err != nil {
		logger.Fatalf("open crm store: %v", err)
	}

	st, err := store.New(*dataDir)
	if err != nil {
		logger.Fatalf("open store: %v", err)
	}
	// NewIndex performs its initial load synchronously, so the index is
	// already populated by the time it returns — this single-shot CLI has no
	// need for the server's periodic StartAutoReload.
	idx := httpapi.NewIndex(st, logger.Printf)

	gmailClient := gmail.New(gmail.Config{
		ClientID:     *googleClientID,
		ClientSecret: *googleClientSecret,
		RedirectURL:  *googleRedirectURL,
	})

	api := httpapi.NewCRMAPI(crmStore, idx, gmailClient)

	mode := "TEST MODE (no emails sent)"
	if !*testMode {
		mode = "REAL RUN — sending through candidates' Gmail"
	}
	logger.Printf("starting auto-apply pass: %s, count=%d", mode, *count)

	result, err := api.RunAutoApply(*count, *testMode)
	if err != nil {
		logger.Fatalf("auto-apply run failed: %v", err)
	}

	s := result.Stats
	fmt.Printf("run %s: considered=%d sent=%d skipped=%d failed=%d noEmail=%d\n",
		result.Run.ID, s.Considered, s.Sent, s.Skipped, s.Failed, s.NoEmail)
	for _, d := range s.Details {
		fmt.Println("  " + d)
	}
}
