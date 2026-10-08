package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"faangjobs/internal/ai"
	"faangjobs/internal/model"
	"faangjobs/internal/registry"
	"faangjobs/internal/source"
	"faangjobs/internal/store"
)

// "Any site" scraping: the admin pastes the URL of a careers page or job
// board listing, an LLM reads the page and lists the postings on it, and the
// postings' own pages are then visited (without the LLM) to find an
// application e-mail and a fuller description. arbeitsagentur.de keeps its
// dedicated API adapter (crm_scrape.go); this is for every other site.

// PageJobExtractor lists the job postings on a page — *ai.Groq or *ai.Claude.
type PageJobExtractor interface {
	ExtractJobs(ctx context.Context, pageURL, pageText string) ([]ai.PageJob, error)
}

const (
	// siteMaxJobs caps how many postings one page scrape keeps.
	siteMaxJobs = 40
	// siteMaxDetailPages caps how many posting pages are visited per scrape
	// to look for an application e-mail.
	siteMaxDetailPages = 15
	// siteDetailWorkers is how many posting pages are fetched at once.
	siteDetailWorkers = 3
	// siteMaxDescription bounds a description taken from a posting's page.
	siteMaxDescription = 8000
)

var siteHeaders = map[string]string{
	"Accept":          "text/html,application/xhtml+xml",
	"Accept-Language": "de-DE,de;q=0.9,en;q=0.8",
}

var errPagesNotConfigured = errors.New("AI sozlanmagan — boshqa saytlarni scrape qilish uchun serverda GROQ_API_KEY yoki ANTHROPIC_API_KEY kerak")

// scrapeSite scrapes one user-supplied page into that site's on-demand pool
// (one pool per host, deduped by Job.ID like the Bundesagentur pools) and
// returns everything found plus just the postings that are new to the pool.
func (a *CRMAPI) scrapeSite(ctx context.Context, rawURL string) (all, newOnes []model.Job, err error) {
	if a.pages == nil {
		return nil, nil, errPagesNotConfigured
	}
	pageURL, err := publicHTTPURL(ctx, rawURL)
	if err != nil {
		return nil, nil, err
	}

	body, err := a.fetcher.Do(ctx, "GET", pageURL.String(), nil, siteHeaders)
	if err != nil {
		return nil, nil, fmt.Errorf("sahifani yuklab bo'lmadi: %w", err)
	}
	text := source.PageText(string(body), pageURL)
	if len(text) < 80 {
		return nil, nil, errors.New("sahifada matn topilmadi — sayt kontentni JavaScript bilan yuklaydigan bo'lishi mumkin; e'lonlar ro'yxati to'g'ridan-to'g'ri ko'rinadigan sahifa havolasini kiriting")
	}

	pageJobs, err := a.pages.ExtractJobs(ctx, pageURL.String(), text)
	if err != nil {
		return nil, nil, err
	}
	if len(pageJobs) == 0 {
		return nil, nil, errors.New("bu sahifada ish e'lonlari topilmadi")
	}
	if len(pageJobs) > siteMaxJobs {
		pageJobs = pageJobs[:siteMaxJobs]
	}

	host := strings.TrimPrefix(strings.ToLower(pageURL.Hostname()), "www.")
	company := registry.Company{Name: host, ATS: "site", Slug: host}
	company.EnsureID()

	jobs := make([]model.Job, len(pageJobs))
	for i, pj := range pageJobs {
		jobURL := pageURL.String()
		if u, err := pageURL.Parse(pj.URL); pj.URL != "" && err == nil && (u.Scheme == "http" || u.Scheme == "https") {
			jobURL = u.String()
		}
		jobs[i] = model.Job{
			// The posting's own URL identifies it; postings without one share
			// the page URL, so title/employer/location tell them apart.
			ID:               company.ID + "~" + model.StableID(jobURL, pj.Title, pj.Employer, pj.Location),
			Title:            pj.Title,
			Company:          pj.Employer,
			Location:         pj.Location,
			URL:              jobURL,
			Description:      pj.Summary,
			ApplicationEmail: pj.ApplicationEmail,
			Website:          pageURL.Scheme + "://" + pageURL.Host,
		}
	}
	chrome := siteChrome(text, pageJobs)
	for i := range jobs {
		if chrome.emails[strings.ToLower(jobs[i].ApplicationEmail)] {
			jobs[i].ApplicationEmail = ""
		}
	}
	a.enrichFromPostingPages(ctx, pageURL, jobs, chrome)

	// Pages rarely give a machine-readable date, so a posting counts as
	// published when first seen (the merge below keeps the first sighting).
	now := time.Now().UTC()
	for i := range jobs {
		j := &jobs[i]
		j.Specialties = source.ClassifySpecialties(j.Title, j.Description)
		j.ContactPerson, j.Salutation = source.ContactPerson(j.Description)
		j.PostedAt = now
	}
	fetched := source.Finalize(company, jobs)

	existing, err := a.jobStore.ReadCompany(company.ID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	var existingJobs []model.Job
	if existing != nil {
		existingJobs = existing.Jobs
	}
	merged, newOnes := mergeJobsByID(existingJobs, fetched)

	if err := a.jobStore.WriteCompany(store.CompanyResult{
		CompanyID: company.ID,
		Company:   host + " (on-demand)",
		ATS:       "site",
		Slug:      host,
		FetchedAt: time.Now().UTC(),
		OK:        true,
		Jobs:      merged,
		JobCount:  len(merged),
	}); err != nil {
		return nil, nil, err
	}
	a.idx.Reload()

	return fetched, newOnes, nil
}

// pageChrome is what a site repeats on every page — navigation, footer,
// the site's own contact address — learned from the listing page so it can
// be told apart from a posting's own content.
type pageChrome struct {
	lines  map[string]bool
	emails map[string]bool // lower-cased; never an application address
}

// siteChrome collects the listing page's lines, and — when the listing shows
// postings from several employers, i.e. it is a job board rather than one
// employer's careers page — every e-mail on it: on a board those are the
// board's own addresses (a live test found kontakt@medi-karriere.de picked
// as the "application e-mail" for half the postings). On a single
// employer's page the listing's address may well be the right one, so it
// stays usable.
func siteChrome(listingText string, jobs []ai.PageJob) pageChrome {
	c := pageChrome{lines: map[string]bool{}, emails: map[string]bool{}}
	for _, l := range strings.Split(listingText, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			c.lines[l] = true
		}
	}
	employers := map[string]bool{}
	for _, j := range jobs {
		if j.Employer != "" {
			employers[strings.ToLower(j.Employer)] = true
		}
	}
	if len(employers) >= 2 {
		for _, e := range source.Emails(listingText) {
			c.emails[strings.ToLower(e)] = true
		}
	}
	return c
}

// strip drops the lines a posting page shares with the listing page, and
// the runs of blank lines that leaves behind.
func (c pageChrome) strip(text string) string {
	var kept []string
	for _, l := range strings.Split(text, "\n") {
		t := strings.TrimSpace(l)
		if t == "" {
			if len(kept) > 0 && kept[len(kept)-1] != "" {
				kept = append(kept, "")
			}
			continue
		}
		if !c.lines[t] {
			kept = append(kept, t)
		}
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// enrichFromPostingPages visits each posting's own page (when it has one
// other than the listing) to fill in the application e-mail and a fuller
// description, ignoring what the page shares with the listing (site chrome).
// Best-effort: a page that fails to load just leaves the job as the listing
// described it.
func (a *CRMAPI) enrichFromPostingPages(ctx context.Context, listing *url.URL, jobs []model.Job, chrome pageChrome) {
	var todo []int
	for i, j := range jobs {
		if j.URL != listing.String() && len(todo) < siteMaxDetailPages {
			todo = append(todo, i)
		}
	}

	work := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < siteDetailWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				u, err := publicHTTPURL(ctx, jobs[i].URL)
				if err != nil {
					continue
				}
				body, err := a.fetcher.Do(ctx, "GET", u.String(), nil, siteHeaders)
				if err != nil {
					continue
				}
				text := chrome.strip(source.PageText(string(body), u))
				if jobs[i].ApplicationEmail == "" {
					jobs[i].ApplicationEmail = source.PickApplicationEmail(text, chrome.emails)
				}
				if len(text) > len(jobs[i].Description) {
					if len(text) > siteMaxDescription {
						text = text[:siteMaxDescription]
					}
					jobs[i].Description = text
				}
			}
		}()
	}
	for _, i := range todo {
		work <- i
	}
	close(work)
	wg.Wait()
}

// publicHTTPURL parses a user-supplied URL and refuses anything but http(s)
// to a public address, so the scraper can't be pointed at this server's own
// ports or the VPS's private network.
func publicHTTPURL(ctx context.Context, raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw != "" && !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, fmt.Errorf("noto'g'ri sayt manzili: %q", raw)
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname())
	if err != nil || len(addrs) == 0 {
		return nil, fmt.Errorf("sayt topilmadi: %s", u.Hostname())
	}
	for _, a := range addrs {
		ip := a.IP
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
			return nil, fmt.Errorf("bu manzilga ruxsat yo'q: %s", u.Hostname())
		}
	}
	return u, nil
}
