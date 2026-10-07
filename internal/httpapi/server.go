package httpapi

import (
	"compress/gzip"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"faangjobs/internal/crm"
	"faangjobs/internal/gmail"
	"faangjobs/internal/source"
	"faangjobs/internal/store"
	"faangjobs/internal/webui"
)

// Config configures the HTTP server handler.
type Config struct {
	// WebDir, if set, serves the frontend from this directory instead of the
	// embedded build (useful for frontend development).
	WebDir string
	// 42.uz authentication. Auth is enabled iff JWTSecret is non-empty; see
	// auth.go for the flow.
	JWTSecret string
	AuthAPI   string // base URL of the auth API (e.g. https://42.uz)
	LoginURL  string // where unauthenticated visitors are sent
	EnrollURL string // where authenticated non-enrollees are sent
	// CRM is the candidate/vacancy CRM store (see internal/crm). Nil disables
	// every /api/crm/* route.
	CRM *crm.Store
	// JobStore and Fetcher back the on-demand, candidate-scoped scrape
	// endpoint (POST /api/crm/candidates/{id}/scrape, crm_scrape.go). Nil
	// JobStore disables that route with a clear error rather than a panic —
	// it's optional so tests/tools that only need the read-side Index don't
	// have to construct a whole store.Store.
	JobStore *store.Store
	Fetcher  *source.Fetcher
	// Gmail configures the candidate-Gmail-connect OAuth flow (see
	// internal/gmail and crm_gmail.go). A zero value disables it — every
	// Gmail route responds 503 rather than being unreachable, so the
	// frontend can show a clear "not configured" state instead of a 404.
	Gmail gmail.Config
	// Letters writes each application e-mail (Groq or Claude, see internal/ai).
	// Nil leaves auto-apply unable to run, with a clear error.
	Letters LetterWriter
	// Pages reads arbitrary job pages for "any site" scrapes (same AI as
	// Letters). Nil disables that scrape source with a clear error.
	Pages PageJobExtractor
	Log   func(format string, args ...any)
}

// Handler builds the complete HTTP handler: JSON API + embedded SPA, wrapped
// with gzip, permissive CORS, and access logging.
func Handler(idx *Index, cfg Config) (http.Handler, error) {
	log := cfg.Log
	if log == nil {
		log = func(string, ...any) {}
	}

	mux := http.NewServeMux()
	NewAPI(idx).Register(mux)
	NewCRMAPI(cfg.CRM, idx, gmail.New(cfg.Gmail), cfg.JobStore, cfg.Fetcher, cfg.Letters, cfg.Pages).Register(mux)
	if cfg.Gmail.Enabled() {
		log("Gmail integration enabled (redirect: %s)", cfg.Gmail.RedirectURL)
	} else {
		log("Gmail integration disabled (no Google client ID/secret configured)")
	}

	// 42.uz authentication (nil/no-op unless a JWT secret is configured).
	auth := NewAuth(cfg, log)
	mux.HandleFunc("GET /api/me", auth.handleMe)
	if auth.Enabled() {
		log("42.uz authentication enabled (auth API: %s)", cfg.AuthAPI)
	} else {
		log("authentication disabled (no JWT secret configured) — signing visitors in as the test user")
	}

	// Frontend.
	var fsys fs.FS
	var err error
	if cfg.WebDir != "" {
		fsys, err = diskFS(cfg.WebDir)
		if err != nil {
			return nil, fmt.Errorf("web dir %q: %w", cfg.WebDir, err)
		}
		log("serving frontend from disk: %s", cfg.WebDir)
	} else {
		fsys, err = webui.FS()
		if err != nil {
			return nil, fmt.Errorf("embedded frontend: %w", err)
		}
	}
	static, err := newStaticHandler(fsys, log)
	if err != nil {
		return nil, fmt.Errorf("static handler: %w", err)
	}
	mux.Handle("/", static)

	return logMiddleware(log)(corsMiddleware(gzipMiddleware(auth.Middleware(mux)))), nil
}

// --- middleware ---

type middleware func(http.Handler) http.Handler

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

var gzipPool = sync.Pool{New: func() any { return gzip.NewWriter(nil) }}

type gzipResponseWriter struct {
	http.ResponseWriter
	gz *gzip.Writer
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) { return g.gz.Write(b) }

func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		// Don't gzip already-compressed asset types.
		if hasCompressedExt(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		gz := gzipPool.Get().(*gzip.Writer)
		gz.Reset(w)
		defer func() {
			_ = gz.Close()
			gzipPool.Put(gz)
		}()
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		w.Header().Del("Content-Length")
		next.ServeHTTP(&gzipResponseWriter{ResponseWriter: w, gz: gz}, r)
	})
}

func hasCompressedExt(p string) bool {
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".webp", ".woff2", ".gz", ".ico"} {
		if strings.HasSuffix(p, ext) {
			return true
		}
	}
	return false
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wrote {
		s.status = http.StatusOK
		s.wrote = true
	}
	return s.ResponseWriter.Write(b)
}

func logMiddleware(log func(string, ...any)) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			// Only log API and error responses to keep noise down.
			if strings.HasPrefix(r.URL.Path, "/api/") || rec.status >= 400 {
				log("%s %s -> %d (%s)", r.Method, r.URL.RequestURI(), rec.status, time.Since(start).Round(time.Millisecond))
			}
		})
	}
}
