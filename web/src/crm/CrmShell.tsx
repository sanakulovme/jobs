import { useState } from 'react'
import { CandidatesPage } from './CandidatesPage'
import { VacanciesPage } from './VacanciesPage'
import { ApplicationsPage } from './ApplicationsPage'
import { AnalyticsPage } from './AnalyticsPage'
import './crm.css'

type Section = 'candidates' | 'vacancies' | 'applications' | 'analytics'

const SECTIONS: { key: Section; label: string; ready: boolean }[] = [
  { key: 'candidates', label: 'Kandidatlar', ready: true },
  { key: 'vacancies', label: 'Vakansiyalar', ready: true },
  { key: 'applications', label: 'Arizalar', ready: true },
  { key: 'analytics', label: 'Analitika', ready: true },
]

// CrmShell is the left-sidebar workspace for the candidate/vacancy CRM,
// reachable at /crm — a sibling of the public job board (App.tsx), reusing
// its exact sidebar visual language (.sidebar/.sb-*) so the two feel like one
// product rather than two bolted-together tools.
export function CrmShell() {
  const [section, setSection] = useState<Section>('candidates')

  return (
    <div className="shell">
      <aside className="sidebar" aria-label="CRM navigation">
        <div className="sb-brand">
          <span className="sb-logo" aria-hidden="true">C</span>
          <span className="sb-name">Rekruting CRM</span>
        </div>
        <div className="sb-section" role="group" aria-label="Sections">
          {SECTIONS.map((s) => (
            <button
              key={s.key}
              className={'sb-item' + (section === s.key ? ' on' : '')}
              onClick={() => setSection(s.key)}
            >
              <span className="sb-item-text">{s.label}</span>
              {!s.ready && <span className="sb-count">soon</span>}
            </button>
          ))}
        </div>
        <div className="sb-section">
          <a className="sb-item" href="/">
            <span className="sb-item-text">← Ish e'lonlari</span>
          </a>
        </div>
      </aside>

      <main className="main">
        {section === 'candidates' && <CandidatesPage />}
        {section === 'vacancies' && <VacanciesPage />}
        {section === 'applications' && <ApplicationsPage />}
        {section === 'analytics' && <AnalyticsPage />}
      </main>
    </div>
  )
}
