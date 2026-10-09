import { useEffect, useMemo, useRef, useState } from 'react'
import { api } from './api'
import type { Facet, Job, Me, Stats } from './api'
import { numberFmt, relativeDate } from './format'
import { JobRow } from './JobRow'
import { Check, Menu, Moon, SearchIcon, Sun, XIcon } from './icons'

const PAGE_SIZE = 25
// How many countries the sidebar lists: a short global top-N, but every country
// of a region once one is selected (regions have at most a few dozen).
const COUNTRY_LIMIT = 16
const COUNTRY_LIMIT_IN_REGION = 40
const STATE_LIMIT = 60
const CITY_LIMIT = 40
// Germany gets a shortcut under Region and a Bundesland → city drill-down:
// the CRM places candidates into German jobs.
const GERMANY = 'Germany'

const SORTS: { value: string; label: string }[] = [
  { value: 'recent', label: 'Newest first' },
  { value: 'oldest', label: 'Oldest first' },
  { value: 'title', label: 'Title A–Z' },
  { value: 'company', label: 'Company A–Z' },
]

export function App() {
  // ── theme ──────────────────────────────────────────────────────────
  const [theme, setTheme] = useState<'light' | 'dark'>(
    () => (document.documentElement.dataset.theme as 'light' | 'dark') || 'light',
  )
  useEffect(() => {
    document.documentElement.dataset.theme = theme
    try {
      localStorage.setItem('fb.theme', theme)
    } catch {
      /* ignore */
    }
  }, [theme])

  // ── page data ──────────────────────────────────────────────────────
  const [stats, setStats] = useState<Stats | null>(null)
  const [allCategories, setAllCategories] = useState<Facet[]>([])
  const [allCountries, setAllCountries] = useState<Facet[]>([])
  const [allRegions, setAllRegions] = useState<Facet[]>([])
  const [allStates, setAllStates] = useState<Facet[]>([])
  const [allCities, setAllCities] = useState<Facet[]>([])
  const [allSites, setAllSites] = useState<Facet[]>([])
  const [me, setMe] = useState<Me | null>(null)
  useEffect(() => {
    const ctrl = new AbortController()
    api.stats(ctrl.signal).then(setStats).catch(() => {})
    api.me(ctrl.signal).then(setMe).catch(() => {})
    api
      .filters(ctrl.signal)
      .then((f) => {
        setAllCategories(f.categories || [])
        setAllCountries(f.countries || [])
        setAllRegions(f.regions || [])
        setAllStates(f.states || [])
        setAllCities(f.cities || [])
        setAllSites(f.sites || [])
      })
      .catch(() => {})
    return () => ctrl.abort()
  }, [])

  // ── view state ─────────────────────────────────────────────────────
  const [qInput, setQInput] = useState('')
  const [q, setQ] = useState('')
  const [category, setCategory] = useState('')
  const [country, setCountry] = useState('')
  const [region, setRegion] = useState('')
  const [state, setState] = useState('')
  const [city, setCity] = useState('')
  const [site, setSite] = useState('')
  const [remote, setRemote] = useState(false)
  const [relocation, setRelocation] = useState(false)
  const [sort, setSort] = useState('recent')
  const [page, setPage] = useState(1)
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [sidebarOpen, setSidebarOpen] = useState(false)

  useEffect(() => {
    const t = setTimeout(() => {
      setQ(qInput.trim())
      setPage(1)
    }, 250)
    return () => clearTimeout(t)
  }, [qInput])

  useEffect(() => {
    if (!sidebarOpen) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setSidebarOpen(false)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [sidebarOpen])

  // ── results (single full fetch, no pagination) ─────────────────────
  const [jobs, setJobs] = useState<Job[]>([])
  const [total, setTotal] = useState(0)
  const [liveCats, setLiveCats] = useState<Map<string, number>>(new Map())
  const [liveCountries, setLiveCountries] = useState<Map<string, number>>(new Map())
  const [liveRegions, setLiveRegions] = useState<Map<string, number>>(new Map())
  const [liveStates, setLiveStates] = useState<Map<string, number>>(new Map())
  const [liveCities, setLiveCities] = useState<Map<string, number>>(new Map())
  const [liveSites, setLiveSites] = useState<Map<string, number>>(new Map())
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [retryNonce, setRetryNonce] = useState(0)
  const reqId = useRef(0)

  useEffect(() => {
    const id = ++reqId.current
    const ctrl = new AbortController()
    setLoading(true)
    setError(null)
    api
      .jobs(
        { q, category, country, region, state, city, site, remote, relocation, sort, page, pageSize: PAGE_SIZE },
        ctrl.signal,
      )
      .then((res) => {
        if (id !== reqId.current) return
        setTotal(res.total)
        setJobs((prev) => (page === 1 ? res.jobs : [...prev, ...res.jobs]))
        setLiveCats(new Map((res.facets?.categories || []).map((f) => [f.value, f.count])))
        setLiveCountries(new Map((res.facets?.countries || []).map((f) => [f.value, f.count])))
        setLiveRegions(new Map((res.facets?.regions || []).map((f) => [f.value, f.count])))
        setLiveStates(new Map((res.facets?.states || []).map((f) => [f.value, f.count])))
        setLiveCities(new Map((res.facets?.cities || []).map((f) => [f.value, f.count])))
        setLiveSites(new Map((res.facets?.sites || []).map((f) => [f.value, f.count])))
        setLoading(false)
      })
      .catch((e: unknown) => {
        if ((e as Error).name === 'AbortError' || id !== reqId.current) return
        setError(String((e as Error).message || e))
        setLoading(false)
      })
    return () => ctrl.abort()
  }, [q, category, country, region, state, city, site, remote, relocation, sort, page, retryNonce])

  const hasFilters =
    q !== '' ||
    category !== '' ||
    country !== '' ||
    region !== '' ||
    state !== '' ||
    city !== '' ||
    site !== '' ||
    remote ||
    relocation
  const hasMore = !error && jobs.length < total

  function clearFilters() {
    setQInput('')
    setQ('')
    setCategory('')
    setCountry('')
    setRegion('')
    setState('')
    setCity('')
    setSite('')
    setRemote(false)
    setRelocation(false)
    setPage(1)
  }

  // Live counts overlay for sidebar facet lists.
  const categoryItems = useMemo(
    () =>
      allCategories.map((c) => ({
        value: c.value,
        count: liveCats.has(c.value) ? liveCats.get(c.value)! : hasFilters ? 0 : c.count,
      })),
    [allCategories, liveCats, hasFilters],
  )
  const regionItems = useMemo(
    () =>
      allRegions.map((r) => ({
        value: r.value,
        count: liveRegions.has(r.value) ? liveRegions.get(r.value)! : hasFilters ? 0 : r.count,
      })),
    [allRegions, liveRegions, hasFilters],
  )
  // With a region selected the country list is scoped to that region — picking
  // a country then narrows within it rather than replacing the region.
  const countryItems = useMemo(() => {
    // Parent-less facets (the Remote / Other buckets) belong to every region.
    const inRegion = region
      ? allCountries.filter((c) => !c.parent || c.parent === region)
      : allCountries
    return inRegion
      .slice(0, region ? COUNTRY_LIMIT_IN_REGION : COUNTRY_LIMIT)
      .map((c) => ({
        value: c.value,
        count: liveCountries.has(c.value) ? liveCountries.get(c.value)! : hasFilters ? 0 : c.count,
      }))
  }, [allCountries, liveCountries, hasFilters, region])

  // States/provinces exist only for the US and Canada, so this list appears
  // once the scope is one of them — which is what makes North America (two
  // countries, fifty-odd states) browsable.
  const countryParents = useMemo(
    () => new Map(allCountries.map((c) => [c.value, c.parent])),
    [allCountries],
  )
  const stateItems = useMemo(() => {
    const scoped = allStates.filter((s) =>
      country ? s.parent === country : region ? countryParents.get(s.parent ?? '') === region : false,
    )
    return scoped.slice(0, STATE_LIMIT).map((s) => ({
      value: s.value,
      count: liveStates.has(s.value) ? liveStates.get(s.value)! : hasFilters ? 0 : s.count,
    }))
  }, [allStates, countryParents, country, region, liveStates, hasFilters])

  // Cities exist only for Germany: listed once Germany (all of it, or one
  // Bundesland) is in scope.
  const cityItems = useMemo(() => {
    if (country !== GERMANY) return []
    const scoped = state ? allCities.filter((c) => c.parent === state) : allCities
    return scoped.slice(0, CITY_LIMIT).map((c) => ({
      value: c.value,
      count: liveCities.has(c.value) ? liveCities.get(c.value)! : hasFilters ? 0 : c.count,
    }))
  }, [allCities, country, state, liveCities, hasFilters])

  const siteItems = useMemo(
    () =>
      allSites.map((s) => ({
        value: s.value,
        count: liveSites.has(s.value) ? liveSites.get(s.value)! : hasFilters ? 0 : s.count,
      })),
    [allSites, liveSites, hasFilters],
  )
  const germanyCount = useMemo(() => {
    const g = allCountries.find((c) => c.value === GERMANY)
    if (!g) return null
    return liveCountries.has(GERMANY) ? liveCountries.get(GERMANY)! : hasFilters ? 0 : g.count
  }, [allCountries, liveCountries, hasFilters])

  // Any filter change starts back at page 1.
  function pickAndClose<T>(setter: (v: T) => void) {
    return (v: T) => {
      setter(v)
      setPage(1)
      setSidebarOpen(false)
    }
  }

  function toggleWithReset(setter: (fn: (v: boolean) => boolean) => void) {
    return () => {
      setter((v) => !v)
      setPage(1)
    }
  }

  const sidebar = (
    <aside className={'sidebar' + (sidebarOpen ? ' open' : '')} aria-label="Filters">
      <div className="sb-brand">
        <span className="sb-logo" aria-hidden="true">F</span>
        <span className="sb-name">FaangJobs</span>
        <span className="grow" />
        <a className="sb-crmlink" href="/crm" title="CRM boshqaruv paneliga o'tish">
          CRM
        </a>
        <button
          className="sb-iconbtn"
          aria-label="Toggle theme"
          title="Toggle theme"
          onClick={() => setTheme((t) => (t === 'light' ? 'dark' : 'light'))}
        >
          {theme === 'light' ? <Moon /> : <Sun />}
        </button>
        <button
          className="sb-iconbtn sb-close"
          aria-label="Close filters"
          onClick={() => setSidebarOpen(false)}
        >
          <XIcon />
        </button>
      </div>

      <label className="sb-search">
        <SearchIcon />
        <input
          value={qInput}
          onChange={(e) => setQInput(e.target.value)}
          placeholder="Search jobs…"
          aria-label="Search jobs"
          autoComplete="off"
          spellCheck={false}
        />
        {qInput && (
          <button className="sb-x" aria-label="Clear search" onClick={() => setQInput('')}>
            <XIcon />
          </button>
        )}
      </label>

      {siteItems.length > 1 && (
        <div className="sb-section" role="group" aria-label="Site">
          <div className="sb-label">Site</div>
          <button
            className={'sb-item' + (site === '' ? ' on' : '')}
            onClick={() => pickAndClose(setSite)('')}
          >
            <span className="sb-item-text">All sites</span>
          </button>
          {siteItems.map((s) => (
            <button
              key={s.value}
              className={'sb-item' + (site === s.value ? ' on' : '')}
              onClick={() => pickAndClose(setSite)(site === s.value ? '' : s.value)}
            >
              <span className="sb-item-text">{s.value}</span>
              <span className="sb-count">{s.count ? numberFmt(s.count) : ''}</span>
            </button>
          ))}
        </div>
      )}

      <div className="sb-section" role="group" aria-label="Region">
        <div className="sb-label">Region</div>
        <button
          className={'sb-item' + (region === '' && country === '' ? ' on' : '')}
          onClick={() => {
            setRegion('')
            setState('')
            setCity('')
            pickAndClose(setCountry)('')
          }}
        >
          <span className="sb-item-text">Everywhere</span>
        </button>
        {germanyCount !== null && (
          <button
            className={'sb-item' + (country === GERMANY && region === '' ? ' on' : '')}
            onClick={() => {
              setRegion('')
              setState('')
              setCity('')
              pickAndClose(setCountry)(country === GERMANY && region === '' ? '' : GERMANY)
            }}
          >
            <span className="sb-item-text">All of Germany</span>
            <span className="sb-count">{germanyCount ? numberFmt(germanyCount) : ''}</span>
          </button>
        )}
        {regionItems.map((r) => (
          <button
            key={r.value}
            className={'sb-item' + (region === r.value ? ' on' : '')}
            onClick={() => {
              setCountry('')
              setState('')
              setCity('')
              pickAndClose(setRegion)(region === r.value ? '' : r.value)
            }}
          >
            <span className="sb-item-text">{r.value}</span>
            <span className="sb-count">{r.count ? numberFmt(r.count) : ''}</span>
          </button>
        ))}
      </div>

      <div className="sb-section" role="group" aria-label="Country">
        <div className="sb-label">{region ? `Country in ${region}` : 'Country'}</div>
        {region && (
          <button
            className={'sb-item' + (country === '' ? ' on' : '')}
            onClick={() => {
              setState('')
              setCity('')
              pickAndClose(setCountry)('')
            }}
          >
            <span className="sb-item-text">All of {region}</span>
          </button>
        )}
        {countryItems.map((c) => (
          <button
            key={c.value}
            className={'sb-item' + (country === c.value ? ' on' : '')}
            onClick={() => {
              setState('')
              setCity('')
              pickAndClose(setCountry)(country === c.value ? '' : c.value)
            }}
          >
            <span className="sb-item-text">{c.value}</span>
            <span className="sb-count">{c.count ? numberFmt(c.count) : ''}</span>
          </button>
        ))}
      </div>

      {stateItems.length > 0 && (
        <div className="sb-section" role="group" aria-label="State">
          <div className="sb-label">
            {country === 'Canada' ? 'Province' : country === GERMANY ? 'Bundesland' : 'State'}
          </div>
          <button
            className={'sb-item' + (state === '' ? ' on' : '')}
            onClick={() => {
              setCity('')
              pickAndClose(setState)('')
            }}
          >
            <span className="sb-item-text">All of {country || region}</span>
          </button>
          {stateItems.map((s) => (
            <button
              key={s.value}
              className={'sb-item' + (state === s.value ? ' on' : '')}
              onClick={() => {
                setCity('')
                pickAndClose(setState)(state === s.value ? '' : s.value)
              }}
            >
              <span className="sb-item-text">{s.value}</span>
              <span className="sb-count">{s.count ? numberFmt(s.count) : ''}</span>
            </button>
          ))}
        </div>
      )}

      {cityItems.length > 0 && (
        <div className="sb-section" role="group" aria-label="City">
          <div className="sb-label">{state ? `City in ${state}` : 'City'}</div>
          <button
            className={'sb-item' + (city === '' ? ' on' : '')}
            onClick={() => pickAndClose(setCity)('')}
          >
            <span className="sb-item-text">All of {state || country}</span>
          </button>
          {cityItems.map((c) => (
            <button
              key={c.value}
              className={'sb-item' + (city === c.value ? ' on' : '')}
              onClick={() => pickAndClose(setCity)(city === c.value ? '' : c.value)}
            >
              <span className="sb-item-text">{c.value}</span>
              <span className="sb-count">{c.count ? numberFmt(c.count) : ''}</span>
            </button>
          ))}
        </div>
      )}

      <div className="sb-section" role="group" aria-label="Category">
        <div className="sb-label">Category</div>
        <button
          className={'sb-item' + (category === '' ? ' on' : '')}
          onClick={() => pickAndClose(setCategory)('')}
        >
          <span className="sb-item-text">All categories</span>
        </button>
        {categoryItems.map((c) => (
          <button
            key={c.value}
            className={'sb-item' + (category === c.value ? ' on' : '')}
            onClick={() => pickAndClose(setCategory)(category === c.value ? '' : c.value)}
          >
            <span className="sb-item-text">{c.value}</span>
            <span className="sb-count">{c.count ? numberFmt(c.count) : ''}</span>
          </button>
        ))}
      </div>

      <div className="sb-section" role="group" aria-label="Filters">
        <div className="sb-label">Filters</div>
        <button
          className={'sb-item' + (remote ? ' on' : '')}
          onClick={toggleWithReset(setRemote)}
          aria-pressed={remote}
        >
          <span className="sb-check" aria-hidden="true">{remote && <Check />}</span>
          <span className="sb-item-text">Remote</span>
        </button>
        <button
          className={'sb-item' + (relocation ? ' on' : '')}
          onClick={toggleWithReset(setRelocation)}
          aria-pressed={relocation}
        >
          <span className="sb-check" aria-hidden="true">{relocation && <Check />}</span>
          <span className="sb-item-text">Relocation</span>
        </button>
      </div>

      <div className="sb-section" role="group" aria-label="Sort">
        <div className="sb-label">Sort</div>
        {SORTS.map((s) => (
          <button
            key={s.value}
            className={'sb-item' + (sort === s.value ? ' on' : '')}
            onClick={() => pickAndClose(setSort)(s.value)}
          >
            <span className="sb-item-text">{s.label}</span>
          </button>
        ))}
      </div>

      {hasFilters && (
        <div className="sb-section">
          <button className="sb-item sb-clear" onClick={clearFilters}>
            <XIcon /> Clear filters
          </button>
        </div>
      )}
    </aside>
  )

  // Profile badge: first 4 characters of the 42.uz username (sans "@").
  const avatarLabel = useMemo(() => {
    if (!me || me.anonymous) return ''
    return (me.username || me.name || me.id || '').replace(/^@/, '').slice(0, 4).toUpperCase()
  }, [me])
  const avatarTitle = me ? [me.name, me.username].filter(Boolean).join(' · ') : ''
  const avatar = avatarLabel ? (
    <span className="avatar" title={avatarTitle} aria-label={`Signed in as ${avatarTitle}`}>
      {avatarLabel}
    </span>
  ) : null

  return (
    <div className="shell">
      {sidebar}
      {sidebarOpen && <div className="backdrop" onClick={() => setSidebarOpen(false)} />}

      <main className="main">
        {avatar && <div className="avatar-corner">{avatar}</div>}
        <div className="mobilebar">
          <button className="sb-iconbtn" aria-label="Open filters" onClick={() => setSidebarOpen(true)}>
            <Menu />
          </button>
          <span className="sb-name">42 FaangJobs</span>
          <span className="grow" />
          {avatar}
        </div>

        <div className="page">
          <div className="page-icon" aria-hidden="true">🌍</div>
          <h1 className="page-title">42 FaangJobs</h1>
          <p className="page-desc">
            Software, data, infrastructure and security roles at top tech companies —
            every opening, everywhere. One click takes you to the original posting.
          </p>
          <p className="page-stats">
            {stats
              ? `${numberFmt(stats.totalJobs)} roles · ${numberFmt(stats.companies)} companies · ` +
                (relativeDate(stats.lastUpdated) === 'now'
                  ? 'updated just now'
                  : `updated ${relativeDate(stats.lastUpdated)} ago`)
              : ' '}
          </p>

          <div className="countline" role="status">
            {loading ? 'Loading…' : `${numberFmt(total)} ${total === 1 ? 'job' : 'jobs'}`}
          </div>

          <div className="list">
            {error && (
              <div className="state">
                <div className="h">Couldn’t load jobs — {error}</div>
                <button onClick={() => setRetryNonce((n) => n + 1)}>Retry</button>
              </div>
            )}

            {!error && loading && page === 1 && (
              <>
                {Array.from({ length: 10 }).map((_, i) => (
                  <div className="skel-row" key={i}>
                    <div className="skel" style={{ width: 11, height: 11 }} />
                    <div className="skel" style={{ width: `${46 - (i % 4) * 7}%` }} />
                    <div className="skel" style={{ width: '10%', marginLeft: 'auto' }} />
                  </div>
                ))}
              </>
            )}

            {!error && !(loading && page === 1) && jobs.length === 0 && (
              <div className="state">
                <div className="h">No jobs match.</div>
                {hasFilters && <button onClick={clearFilters}>Clear filters</button>}
              </div>
            )}

            {!(error || (loading && page === 1)) &&
              jobs.map((job) => (
                <JobRow
                  key={job.id}
                  job={job}
                  expanded={expandedId === job.id}
                  onToggle={() => setExpandedId((id) => (id === job.id ? null : job.id))}
                />
              ))}

            {hasMore && !(loading && page === 1) && (
              <button className="more" disabled={loading} onClick={() => setPage((p) => p + 1)}>
                {loading
                  ? 'Loading…'
                  : `Load more  ·  ${numberFmt(total - jobs.length)} remaining`}
              </button>
            )}
          </div>
        </div>
      </main>
    </div>
  )
}
