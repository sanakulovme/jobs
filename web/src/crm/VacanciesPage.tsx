import { useEffect, useState } from 'react'
import { crmApi } from './api'
import type { Match, Vacancy } from './api'
import { dateLabel, sourceLabel, specialtyLabel } from './labels'

const PAGE_SIZE = 20

export function VacanciesPage() {
  const [vacancies, setVacancies] = useState<Vacancy[]>([])
  const [total, setTotal] = useState(0)
  const [q, setQ] = useState('')
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [matchesFor, setMatchesFor] = useState<string | null>(null)
  const [showNew, setShowNew] = useState(false)

  function reload() {
    setLoading(true)
    crmApi
      .vacancies({ q, page, pageSize: PAGE_SIZE })
      .then((r) => {
        setVacancies(r.vacancies ?? [])
        setTotal(r.total)
        setLoading(false)
      })
      .catch((e: Error) => {
        setError(e.message)
        setLoading(false)
      })
  }
  useEffect(reload, [q, page])

  return (
    <div className="crm-page">
      <div className="crm-header">
        <div style={{ flex: 1 }}>
          <div className="crm-title">Vakansiyalar</div>
          <div className="crm-subtitle">Ochiq lavozimlarni shu yerda ko'rib, kandidatlarga biriktirasiz.</div>
        </div>
        <button className="btn-primary" onClick={() => setShowNew(true)}>+ Yangi vakansiya</button>
      </div>

      <div className="crm-field" style={{ maxWidth: 320 }}>
        <input
          className="crm-input"
          placeholder="Qidirish…"
          value={q}
          onChange={(e) => { setQ(e.target.value); setPage(1) }}
        />
      </div>

      {error && <div className="state">{error}</div>}
      {!error && loading && <div className="crm-empty">Yuklanmoqda…</div>}
      {!error && !loading && vacancies.length === 0 && <div className="crm-empty">Vakansiya topilmadi.</div>}

      <div className="crm-list">
        {vacancies.map((v) => (
          <VacancyRow
            key={v.id}
            vacancy={v}
            expanded={expandedId === v.id}
            onToggle={() => setExpandedId((id) => (id === v.id ? null : v.id))}
            onOpenMatches={() => setMatchesFor(v.id)}
            onChanged={reload}
          />
        ))}
      </div>

      {total > PAGE_SIZE && (
        <div className="crm-actions" style={{ justifyContent: 'center' }}>
          <button className="btn-quiet" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>Oldingi</button>
          <span className="crm-item-sub">{page} / {Math.ceil(total / PAGE_SIZE)}</span>
          <button className="btn-quiet" disabled={page * PAGE_SIZE >= total} onClick={() => setPage((p) => p + 1)}>Keyingi</button>
        </div>
      )}

      {matchesFor && <MatchesModal jobId={matchesFor} onClose={() => setMatchesFor(null)} />}
      {showNew && <NewVacancyModal onClose={() => setShowNew(false)} onCreated={reload} />}
    </div>
  )
}

function VacancyRow({
  vacancy,
  expanded,
  onToggle,
  onOpenMatches,
  onChanged,
}: {
  vacancy: Vacancy
  expanded: boolean
  onToggle: () => void
  onOpenMatches: () => void
  onChanged: () => void
}) {
  return (
    <div>
      <div className="crm-item" onClick={onToggle}>
        <div className="crm-item-main">
          <div className="crm-item-title">{vacancy.title}</div>
          <div className="crm-item-sub">{vacancy.company} · {vacancy.location}</div>
          <div className="crm-vac-tags">
            {(vacancy.specialties ?? []).map((s) => <span key={s} className="jtag">{specialtyLabel(s)}</span>)}
            <span className="jtag gray">{sourceLabel(vacancy.source)}</span>
          </div>
        </div>
        <div className="crm-item-meta">{dateLabel(vacancy.postedAt)}</div>
      </div>
      {expanded && <VacancyDetail vacancy={vacancy} onOpenMatches={onOpenMatches} onChanged={onChanged} />}
    </div>
  )
}

function VacancyDetail({
  vacancy,
  onOpenMatches,
  onChanged,
}: {
  vacancy: Vacancy
  onOpenMatches: () => void
  onChanged: () => void
}) {
  const [specialties, setSpecialties] = useState<string[]>([])
  const [selected, setSelected] = useState<string[]>(vacancy.specialties ?? [])
  const [saving, setSaving] = useState(false)

  useEffect(() => { crmApi.specialties().then(setSpecialties).catch(() => {}) }, [])

  function toggle(s: string) {
    setSelected((prev) => (prev.includes(s) ? prev.filter((x) => x !== s) : [...prev, s]))
  }

  async function save() {
    setSaving(true)
    try {
      await crmApi.setVacancySpecialties(vacancy.id, selected)
      onChanged()
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="crm-vac-detail" onClick={(e) => e.stopPropagation()}>
      {vacancy.contactPerson && (
        <div className="crm-item-sub">Kontakt: {vacancy.salutation} {vacancy.contactPerson}</div>
      )}
      {vacancy.applicationEmail && <div className="crm-item-sub">Email: {vacancy.applicationEmail}</div>}
      {vacancy.requiredGermanLevel && <div className="crm-item-sub">Nemis tili: {vacancy.requiredGermanLevel}</div>}

      {vacancy.mainDuties && (
        <>
          <div className="crm-vac-section-label">Asosiy vazifalar</div>
          <div className="crm-vac-text">{vacancy.mainDuties}</div>
        </>
      )}
      {vacancy.mandatoryRequirements && (
        <>
          <div className="crm-vac-section-label">Majburiy talablar</div>
          <div className="crm-vac-text">{vacancy.mandatoryRequirements}</div>
        </>
      )}

      <div className="crm-vac-section-label">Yo'nalishlar</div>
      <div className="crm-chips">
        {specialties.map((s) => (
          <button key={s} type="button" className={'crm-chip' + (selected.includes(s) ? ' on' : '')} onClick={() => toggle(s)}>
            {specialtyLabel(s)}
          </button>
        ))}
      </div>

      <div className="crm-actions">
        <button className="btn-quiet" disabled={saving} onClick={save}>
          {saving ? 'Saqlanmoqda…' : 'Yo\'nalishlarni saqlash'}
        </button>
        <a className="btn-quiet" href={vacancy.url} target="_blank" rel="noreferrer">Asl e'lonni ochish</a>
        <button className="btn-primary" onClick={onOpenMatches}>Mos kandidatlar</button>
      </div>
    </div>
  )
}

function MatchesModal({ jobId, onClose }: { jobId: string; onClose: () => void }) {
  const [data, setData] = useState<{ vacancy: Vacancy; matches: Match[] } | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    crmApi.vacancyMatches(jobId).then(setData).catch((e: Error) => setError(e.message))
  }, [jobId])

  return (
    <div className="crm-modal-backdrop" onClick={onClose}>
      <div className="crm-modal" onClick={(e) => e.stopPropagation()}>
        <div className="crm-modal-header">
          <div className="crm-modal-title">Mos kandidatlar</div>
          <button className="crm-modal-close" onClick={onClose}>✕</button>
        </div>

        {error && <div className="state">{error}</div>}
        {!data && !error && <div className="crm-empty">Yuklanmoqda…</div>}
        {data && (
          <>
            <div className="crm-item-sub" style={{ marginBottom: 10 }}>
              {data.vacancy.title} · {data.vacancy.company}
            </div>
            {(data.matches ?? []).length === 0 && <div className="crm-empty">Mos kandidat topilmadi.</div>}
            {(data.matches ?? []).map((m) => (
              <div key={m.candidate.id} className="crm-match-item">
                <div className="crm-match-top">
                  <span className="crm-match-name">{m.candidate.fullName}</span>
                  <span className="crm-match-score">Ball: {m.score}</span>
                  {m.alreadySent && <span className="jtag green">Yuborilgan</span>}
                </div>
                <div className="crm-item-sub">{m.profile.name} · {m.matchedSpecialties.map(specialtyLabel).join(', ')}</div>
                {m.blockers.length > 0 && (
                  <div className="crm-match-blockers">
                    {m.blockers.map((b) => <span key={b} className="jtag red">{b}</span>)}
                  </div>
                )}
              </div>
            ))}
          </>
        )}
      </div>
    </div>
  )
}

function NewVacancyModal({ onClose, onCreated }: { onClose: () => void; onCreated: () => void }) {
  const [specialtyOptions, setSpecialtyOptions] = useState<string[]>([])
  const [title, setTitle] = useState('')
  const [company, setCompany] = useState('')
  const [location, setLocation] = useState('')
  const [applicationEmail, setApplicationEmail] = useState('')
  const [url, setUrl] = useState('')
  const [selected, setSelected] = useState<string[]>([])
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  useEffect(() => { crmApi.specialties().then(setSpecialtyOptions).catch(() => {}) }, [])

  function toggle(s: string) {
    setSelected((prev) => (prev.includes(s) ? prev.filter((x) => x !== s) : [...prev, s]))
  }

  async function create() {
    if (!title || !company) {
      setError('Sarlavha va kompaniya kerak')
      return
    }
    setSaving(true)
    setError('')
    try {
      await crmApi.createVacancy({ title, company, location, applicationEmail, url, specialties: selected })
      onCreated()
      onClose()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="crm-modal-backdrop" onClick={onClose}>
      <div className="crm-modal" onClick={(e) => e.stopPropagation()}>
        <div className="crm-modal-header">
          <div className="crm-modal-title">Yangi vakansiya</div>
          <button className="crm-modal-close" onClick={onClose}>✕</button>
        </div>

        <div className="crm-field">
          <label className="crm-field-label">Sarlavha *</label>
          <input className="crm-input" value={title} onChange={(e) => setTitle(e.target.value)} />
        </div>
        <div className="crm-row">
          <div className="crm-field">
            <label className="crm-field-label">Kompaniya *</label>
            <input className="crm-input" value={company} onChange={(e) => setCompany(e.target.value)} />
          </div>
          <div className="crm-field">
            <label className="crm-field-label">Shahar</label>
            <input className="crm-input" value={location} onChange={(e) => setLocation(e.target.value)} />
          </div>
        </div>
        <div className="crm-row">
          <div className="crm-field">
            <label className="crm-field-label">Email</label>
            <input className="crm-input" value={applicationEmail} onChange={(e) => setApplicationEmail(e.target.value)} />
          </div>
          <div className="crm-field">
            <label className="crm-field-label">Havola</label>
            <input className="crm-input" value={url} onChange={(e) => setUrl(e.target.value)} />
          </div>
        </div>
        <div className="crm-field">
          <label className="crm-field-label">Yo'nalishlar</label>
          <div className="crm-chips">
            {specialtyOptions.map((s) => (
              <button key={s} type="button" className={'crm-chip' + (selected.includes(s) ? ' on' : '')} onClick={() => toggle(s)}>
                {specialtyLabel(s)}
              </button>
            ))}
          </div>
        </div>

        {error && <div className="state" style={{ padding: '6px 0' }}>{error}</div>}
        <div className="crm-actions">
          <button className="btn-primary" disabled={saving} onClick={create}>
            {saving ? 'Saqlanmoqda…' : 'Yaratish'}
          </button>
        </div>
      </div>
    </div>
  )
}
