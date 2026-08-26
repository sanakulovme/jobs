import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import type { Job } from './api'
import { api } from './api'
import { cleanLocation, cleanTitle, relativeDate, sanitizeHTML } from './format'
import { ArrowUpRight, Triangle } from './icons'

type Props = {
  job: Job
  expanded: boolean
  onToggle: () => void
}

// A single database-style row: toggle triangle, title + company, right-aligned
// quiet properties, and a one-click Apply that opens the source posting.
export function JobRow({ job, expanded, onToggle }: Props) {
  return (
    <div className="jitem">
      <div className="jrow">
        <button
          className={'jtoggle' + (expanded ? ' open' : '')}
          onClick={onToggle}
          aria-expanded={expanded}
          aria-label={expanded ? 'Hide details' : 'Show details'}
        >
          <Triangle />
        </button>
        <button className="jmain" onClick={onToggle}>
          <span className="jtitle" title={job.title}>{cleanTitle(job.title)}</span>
          <span className="jco" title={job.company}>{job.company}</span>
        </button>
        <div className="jside">
          {job.remote && <span className="jtag blue">Remote</span>}
          {job.relocation && <span className="jtag green">Relocation</span>}
          <span className="jloc" title={job.location}>{cleanLocation(job.location)}</span>
          <span className="jtime">{relativeDate(job.postedAt)}</span>
          <a
            className="japply"
            href={job.url}
            target="_blank"
            rel="noopener noreferrer"
            aria-label={`Apply at ${job.company} (opens the original posting)`}
          >
            Apply <ArrowUpRight />
          </a>
        </div>
      </div>
      {expanded && <Detail job={job} />}
    </div>
  )
}

// Facts renders the best-effort structured fields (medical specialty, contact
// info, requirements, ...) extracted from a posting's free text. Only present
// fields are shown — most sources don't populate any of these, and a source
// posting rarely mentions all of them.
function Facts({ job }: { job: Job }) {
  const rows: [string, ReactNode][] = []
  if (job.referenceNumber) rows.push(['Reference number', job.referenceNumber])
  if (job.medicalSpecialty) rows.push(['Medical specialty', job.medicalSpecialty])
  if (job.contactPerson) rows.push(['Contact person', [job.salutation, job.contactPerson].filter(Boolean).join(' ')])
  if (job.applicationEmail) rows.push(['Application email', <a href={`mailto:${job.applicationEmail}`}>{job.applicationEmail}</a>])
  if (job.requiredGermanLevel) rows.push(['Required German level', job.requiredGermanLevel])
  if (job.website) rows.push(['Website', <a href={job.website} target="_blank" rel="noopener noreferrer">{job.website}</a>])
  if (job.applicationPortal) rows.push(['Application portal', <a href={job.applicationPortal} target="_blank" rel="noopener noreferrer">{job.applicationPortal}</a>])
  if (job.requiredQualifications?.length) {
    rows.push(['Required qualifications', (
      <ul className="jfacts-list">
        {job.requiredQualifications.map((q, i) => <li key={i}>{q}</li>)}
      </ul>
    )])
  }
  if (job.mainDuties) rows.push(['Main duties', job.mainDuties])
  if (job.mandatoryRequirements) rows.push(['Mandatory requirements', job.mandatoryRequirements])
  if (job.preferredRequirements) rows.push(['Preferred requirements', job.preferredRequirements])

  const flags: string[] = []
  if (job.requiresDriversLicense) flags.push("Driver's license required")
  if (job.requiresOwnCar) flags.push('Own car required')
  if (job.requiresGermanMfaTraining) flags.push('German MFA training required')

  if (rows.length === 0 && flags.length === 0) return null

  return (
    <div className="jfacts">
      {flags.length > 0 && (
        <div className="jfacts-flags">
          {flags.map((f) => <span className="jtag yellow" key={f}>{f}</span>)}
        </div>
      )}
      {rows.map(([label, value]) => (
        <div className="jfacts-row" key={label}>
          <span className="jfacts-label">{label}</span>
          <span className="jfacts-value">{value}</span>
        </div>
      ))}
    </div>
  )
}

function Detail({ job }: { job: Job }) {
  const [full, setFull] = useState<Job | null>(null)
  const [state, setState] = useState<'loading' | 'ok' | 'error'>('loading')

  useEffect(() => {
    const ctrl = new AbortController()
    setState('loading')
    api
      .job(job.id, ctrl.signal)
      .then((j) => {
        setFull(j)
        setState('ok')
      })
      .catch((e: unknown) => {
        if ((e as Error).name !== 'AbortError') setState('error')
      })
    return () => ctrl.abort()
  }, [job.id])

  const desc = full?.description?.trim()
  const allLocations = (job.locations && job.locations.length > 1
    ? job.locations
    : [job.location]
  ).map(cleanLocation)

  return (
    <div className="jdetail">
      <div className="jdetail-meta">
        {(job.categories || []).map((c) => (
          <span className="jtag" key={c}>{c}</span>
        ))}
        {job.department && <span className="jtag">{job.department}</span>}
        <span>{allLocations.join(' · ')}</span>
      </div>

      {state === 'ok' && full && <Facts job={full} />}

      {state === 'loading' && <div className="state" style={{ padding: '8px 0' }}>Loading…</div>}
      {state === 'error' && (
        <div className="state" style={{ padding: '8px 0' }}>
          Couldn’t load the description — open the posting with Apply.
        </div>
      )}
      {state === 'ok' &&
        (desc ? (
          <div className="jdesc" dangerouslySetInnerHTML={{ __html: sanitizeHTML(desc) }} />
        ) : (
          <div className="state" style={{ padding: '8px 0' }}>
            No description provided — open the posting with Apply.
          </div>
        ))}

      <div className="jdetail-actions">
        <a className="btn-primary" href={job.url} target="_blank" rel="noopener noreferrer">
          Apply <ArrowUpRight />
        </a>
      </div>
    </div>
  )
}
