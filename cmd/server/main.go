// Command server serves the FaangJobs web application: the embedded React
// frontend plus a JSON API backed by the crawled data folder. It is one of the
// two standalone binaries of FaangJobs.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"faangjobs/internal/ailetter"
	"faangjobs/internal/crm"
	"faangjobs/internal/dataset"
	"faangjobs/internal/gmail"
	"faangjobs/internal/httpapi"
	"faangjobs/internal/source"
	"faangjobs/internal/store"
)

// envOr returns the environment variable's value, or def when unset/empty.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// loadDotEnv reads simple KEY=VALUE lines from path (if present) into the
// process environment, so secrets like the Google OAuth client id/secret can
// live in a git-ignored local file instead of being retyped on every launch.
// A real environment variable set by the caller always wins over the file —
// this only fills in what's still unset. Blank lines and lines starting with
// # are ignored; values may be wrapped in matching single or double quotes.
func loadDotEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return // no .env file — not an error, envOr's own defaults still apply
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

// diskHasData reports whether dir/companies contains any company JSON files,
// without creating anything on disk.
func diskHasData(dir string) bool {
	entries, err := os.ReadDir(filepath.Join(dir, "companies"))
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			return true
		}
	}
	return false
}

func main() {
	loadDotEnv(".env")

	var (
		dataDir  = flag.String("data", "./data", "directory to read crawled data from")
		addr     = flag.String("addr", ":8080", "listen address")
		webDir   = flag.String("web-dir", "", "serve frontend from this directory instead of the embedded build")
		reload   = flag.Duration("reload", 30*time.Second, "how often to check the data folder for changes")
		embedded = flag.Bool("embedded", false, "serve the data snapshot embedded in the binary (ignore -data)")

		// 42.uz authentication. Auth is enabled iff a JWT secret is provided
		// (flag or FAANGJOBS_JWT_SECRET env); without one the board is open
		// (local dev / self-hosting).
		jwtSecret = flag.String("jwt-secret", envOr("FAANGJOBS_JWT_SECRET", ""), "HS256 secret for 42.uz access tokens (empty = auth disabled)")
		authAPI   = flag.String("auth-api", envOr("FAANGJOBS_AUTH_API", "https://api.42.uz"), "base URL of the 42.uz auth API")
		loginURL  = flag.String("login-url", envOr("FAANGJOBS_LOGIN_URL", "https://42.uz/login"), "where unauthenticated visitors are redirected")
		enrollURL = flag.String("enroll-url", envOr("FAANGJOBS_ENROLL_URL", "https://42.uz/course/devops"), "where authenticated non-enrollees are redirected")

		// Candidate/vacancy CRM (auto-apply). Always on — it lives alongside the
		// job data under -data/crm and is independent of -embedded, since CRM
		// records are never part of the embedded snapshot.
		crmDataDir = flag.String("crm-data", "", "directory for CRM data (candidates, applications, ...); defaults to -data")

		// Gmail OAuth (candidate auto-apply sending). Enabled iff both the
		// client id and secret are set; get them from Google Cloud Console ->
		// APIs & Services -> Credentials. The redirect URL must match exactly
		// what's registered there.
		googleClientID     = flag.String("google-client-id", envOr("FAANGJOBS_GOOGLE_CLIENT_ID", ""), "Google OAuth client ID (empty = Gmail integration disabled)")
		googleClientSecret = flag.String("google-client-secret", envOr("FAANGJOBS_GOOGLE_CLIENT_SECRET", ""), "Google OAuth client secret")
		googleRedirectURL  = flag.String("google-redirect-url", envOr("FAANGJOBS_GOOGLE_REDIRECT_URL", ""), "OAuth redirect URI; must match Google Cloud Console exactly (e.g. http://localhost:8080/api/crm/gmail/callback)")

		// Claude API key for writing application letters (internal/ailetter).
		// Without it auto-apply can't run, not even in test mode.
		anthropicKey = flag.String("anthropic-api-key", envOr("ANTHROPIC_API_KEY", ""), "Claude API key used to write application letters (empty = auto-apply disabled)")
	)
	flag.Parse()

	logger := log.New(os.Stderr, "", log.LstdFlags)

	crmDir := *crmDataDir
	if crmDir == "" {
		crmDir = *dataDir
	}
	crmStore, err := crm.New(crmDir)
	if err != nil {
		logger.Fatalf("open crm store: %v", err)
	}

	// Data source selection: an explicit -embedded flag wins; otherwise prefer
	// a live ./data folder (fresher + hot-reloadable) and fall back to the
	// snapshot baked into the binary at build time.
	useEmbedded := *embedded
	if !useEmbedded && !diskHasData(*dataDir) && dataset.HasData() {
		useEmbedded = true
	}

	var st *store.Store
	if useEmbedded {
		fsys, err := dataset.FS()
		if err != nil {
			logger.Fatalf("embedded dataset: %v", err)
		}
		st = store.OpenFS(fsys)
		logger.Printf("serving the embedded data snapshot (baked in at build time)")
	} else {
		var err error
		st, err = store.New(*dataDir)
		if err != nil {
			logger.Fatalf("open store: %v", err)
		}
		logger.Printf("serving data from %s", *dataDir)
	}

	idx := httpapi.NewIndex(st, logger.Printf)

	// Shared HTTP fetcher for on-demand, candidate-scoped scrapes triggered
	// from the CRM (see crm_scrape.go) — separate from the standalone
	// cmd/crawler binary's own fetcher, but built the same way. Writing new
	// company data only works against a live -data dir; against -embedded
	// (fsys-backed, read-only) store.WriteCompany already fails cleanly with
	// its own read-only error, so no extra guard is needed here.
	fetcher := source.NewFetcher(source.FetcherOptions{Log: func(f string, a ...any) { logger.Printf("http: "+f, a...) }})

	// Hot reload only makes sense for the mutable on-disk folder.
	stopReload := make(chan struct{})
	if !useEmbedded {
		idx.StartAutoReload(*reload, stopReload)
	}

	var letters httpapi.LetterWriter
	if *anthropicKey != "" {
		letters = ailetter.New(*anthropicKey)
		logger.Printf("AI letter writer enabled (model %s)", ailetter.Model)
	} else {
		logger.Printf("AI letter writer disabled (no ANTHROPIC_API_KEY) — auto-apply will refuse to run")
	}

	handler, err := httpapi.Handler(idx, httpapi.Config{
		WebDir:    *webDir,
		JWTSecret: *jwtSecret,
		AuthAPI:   *authAPI,
		LoginURL:  *loginURL,
		EnrollURL: *enrollURL,
		CRM:       crmStore,
		JobStore:  st,
		Fetcher:   fetcher,
		Letters:   letters,
		Gmail: gmail.Config{
			ClientID:     *googleClientID,
			ClientSecret: *googleClientSecret,
			RedirectURL:  *googleRedirectURL,
		},
		Log: logger.Printf,
	})
	if err != nil {
		logger.Fatalf("build handler: %v", err)
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// Generous enough for a 20 MiB CV upload over a slow connection.
		ReadTimeout: 2 * time.Minute,
		// A candidate scrape fetches Bundesagentur (with retries) and then
		// runs auto-apply synchronously in the same request, which can take
		// well over a minute — a short WriteTimeout would cut it off mid-run.
		WriteTimeout: 10 * time.Minute,
		IdleTimeout:  120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		source := *dataDir
		if useEmbedded {
			source = "embedded snapshot"
		}
		logger.Printf("FaangJobs server listening on %s (data: %s)", *addr, source)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	logger.Printf("shutting down…")
	close(stopReload)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Printf("graceful shutdown failed: %v", err)
	}
	logger.Printf("bye")
}
