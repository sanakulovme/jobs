import { useEffect, useRef, useState } from 'react'
import { crmApi } from './api'
import type { Candidate, CandidateInput, Document, ProfileSpecialty } from './api'

const GERMAN_LEVELS = ['', 'A1', 'A2', 'B1', 'B2', 'C1', 'C2']
const DOC_TYPES = [
  { value: 'cv', label: 'CV (Lebenslauf)' },
  { value: 'cover_letter', label: 'Ariza xati' },
  { value: 'motivation_letter', label: 'Motivatsiya xati' },
  { value: 'certificate', label: 'Sertifikat' },
  { value: 'other', label: 'Boshqa hujjat' },
]
const DOC_LANGUAGES = ['', 'Nemis', 'Ingliz', 'Rus', "O'zbek"]

export function CandidatesPage() {
  const [candidates, setCandidates] = useState<Candidate[] | null>(null)
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [error, setError] = useState('')

  function reload() {
    crmApi
      .candidates()
      .then((r) => setCandidates(r.candidates))
      .catch((e: Error) => setError(e.message))
  }
  useEffect(reload, [])

  async function createCandidate() {
    try {
      const c = await crmApi.createCandidate({ fullName: 'Yangi kandidat' })
      setCandidates((prev) => [...(prev ?? []), c])
      setSelectedId(c.id)
    } catch (e) {
      setError((e as Error).message)
    }
  }

  if (selectedId) {
    return (
      <CandidateDetail
        id={selectedId}
        onBack={() => {
          setSelectedId(null)
          reload()
        }}
      />
    )
  }

  return (
    <div className="crm-page">
      <div className="crm-header">
        <div style={{ flex: 1 }}>
          <div className="crm-title">Kandidatlar</div>
          <div className="crm-subtitle">Profil, hujjatlar va Gmail ulanishini bir joyda boshqaring.</div>
        </div>
        <button className="btn-primary" onClick={createCandidate}>+ Yangi kandidat</button>
      </div>

      {error && <div className="state">{error}</div>}

      {candidates === null && !error && <div className="crm-empty">Yuklanmoqda…</div>}
      {candidates && candidates.length === 0 && (
        <div className="crm-empty">Hali kandidatlar yo'q — "Yangi kandidat" tugmasini bosing.</div>
      )}

      <div className="crm-list">
        {candidates?.map((c) => (
          <div key={c.id} className="crm-item" onClick={() => setSelectedId(c.id)}>
            <div className="crm-item-main">
              <div className="crm-item-title">{c.fullName}</div>
              <div className="crm-item-sub">{c.contactEmail || 'email kiritilmagan'}</div>
            </div>
            <div className="crm-item-meta">{c.profiles?.length ?? 0} profil</div>
          </div>
        ))}
      </div>
    </div>
  )
}

// --- detail: 3-step wizard --------------------------------------------

type Step = 'basic' | 'gmail' | 'profile'

function CandidateDetail({ id, onBack }: { id: string; onBack: () => void }) {
  const [candidate, setCandidate] = useState<Candidate | null>(null)
  const [step, setStep] = useState<Step>('basic')
  const [error, setError] = useState('')

  function reload() {
    crmApi
      .candidate(id)
      .then(setCandidate)
      .catch((e: Error) => setError(e.message))
  }
  useEffect(reload, [id])

  if (error) return <div className="crm-page"><div className="state">{error}</div></div>
  if (!candidate) return <div className="crm-page"><div className="crm-empty">Yuklanmoqda…</div></div>

  const basicDone = !!candidate.fullName
  const profileDone = (candidate.profiles?.length ?? 0) > 0

  return (
    <div className="crm-page">
      <button className="btn-quiet" onClick={onBack} style={{ marginBottom: 10 }}>← {candidate.fullName}</button>

      <div className="crm-header">
        <div>
          <div className="crm-title">Kandidatni ishga tayyorlang</div>
          <div className="crm-subtitle">Har bir bosqich alohida saqlanadi. Istalgan vaqtda shu joydan davom ettirishingiz mumkin.</div>
        </div>
      </div>

      <div className="crm-steps">
        <button className={'crm-step' + (step === 'basic' ? ' active' : '') + (basicDone ? ' done' : '')} onClick={() => setStep('basic')}>
          <span className="crm-step-num">{basicDone ? '✓' : '1'}</span>
          Asosiy ma'lumotlar
        </button>
        <button className={'crm-step' + (step === 'gmail' ? ' active' : '')} onClick={() => setStep('gmail')}>
          <span className="crm-step-num">2</span>
          Gmail ulanishi
        </button>
        <button className={'crm-step' + (step === 'profile' ? ' active' : '') + (profileDone ? ' done' : '')} onClick={() => setStep('profile')}>
          <span className="crm-step-num">{profileDone ? '✓' : '3'}</span>
          Ariza profili
        </button>
      </div>

      {step === 'basic' && <BasicInfoStep candidate={candidate} onSaved={setCandidate} />}
      {step === 'gmail' && <GmailStep candidate={candidate} onChanged={reload} />}
      {step === 'profile' && <ProfileStep candidate={candidate} onChanged={reload} />}
    </div>
  )
}

// --- step 1 ------------------------------------------------------------

function BasicInfoStep({ candidate, onSaved }: { candidate: Candidate; onSaved: (c: Candidate) => void }) {
  const [form, setForm] = useState<CandidateInput>({
    fullName: candidate.fullName,
    contactEmail: candidate.contactEmail ?? '',
    phone: candidate.phone ?? '',
    germanLevel: candidate.germanLevel ?? '',
    citizenship: candidate.citizenship ?? '',
    currentCountry: candidate.currentCountry ?? '',
    notes: candidate.notes ?? '',
  })
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  async function save() {
    setSaving(true)
    setError('')
    try {
      onSaved(await crmApi.updateCandidate(candidate.id, form))
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="crm-card">
      <div className="crm-card-title">Asosiy ma'lumotlar</div>
      <div className="crm-card-desc">Kandidatning aloqa va ishga tayyorgarlik ma'lumotlarini saqlang.</div>

      <div className="crm-field">
        <label className="crm-field-label">To'liq ismi *</label>
        <input className="crm-input" value={form.fullName} onChange={(e) => setForm({ ...form, fullName: e.target.value })} />
      </div>
      <div className="crm-row">
        <div className="crm-field">
          <label className="crm-field-label">Aloqa uchun pochta</label>
          <input className="crm-input" value={form.contactEmail} onChange={(e) => setForm({ ...form, contactEmail: e.target.value })} />
        </div>
        <div className="crm-field">
          <label className="crm-field-label">Telefon</label>
          <input className="crm-input" value={form.phone} onChange={(e) => setForm({ ...form, phone: e.target.value })} />
        </div>
      </div>
      <div className="crm-row">
        <div className="crm-field">
          <label className="crm-field-label">Nemis tili darajasi</label>
          <select className="crm-select" value={form.germanLevel} onChange={(e) => setForm({ ...form, germanLevel: e.target.value })}>
            {GERMAN_LEVELS.map((l) => <option key={l} value={l}>{l || 'Tanlanmagan'}</option>)}
          </select>
        </div>
        <div className="crm-field">
          <label className="crm-field-label">Fuqaroligi</label>
          <input className="crm-input" value={form.citizenship} onChange={(e) => setForm({ ...form, citizenship: e.target.value })} />
        </div>
      </div>
      <div className="crm-field">
        <label className="crm-field-label">Hozirgi davlat</label>
        <input className="crm-input" value={form.currentCountry} onChange={(e) => setForm({ ...form, currentCountry: e.target.value })} />
      </div>
      <div className="crm-field">
        <label className="crm-field-label">Izohlar</label>
        <textarea className="crm-textarea" value={form.notes} onChange={(e) => setForm({ ...form, notes: e.target.value })} />
      </div>

      {error && <div className="state" style={{ padding: '6px 0' }}>{error}</div>}
      <div className="crm-actions">
        <button className="btn-primary" disabled={saving || !form.fullName} onClick={save}>
          {saving ? 'Saqlanmoqda…' : 'Saqlash'}
        </button>
      </div>
    </div>
  )
}

// --- step 2 --------------------------------------------------------------
// Connecting Gmail must happen in the CANDIDATE's own browser session (their
// own Google login), not the admin's — so instead of a direct "Connect"
// button, the admin generates a one-time shareable link here and sends it to
// the candidate outside the CRM (Telegram, WhatsApp, ...). The candidate's
// own click drives the OAuth consent screen.

function GmailStep({ candidate, onChanged }: { candidate: Candidate; onChanged: () => void }) {
  const connected = !!candidate.gmailEmail
  const [link, setLink] = useState<{ url: string; expiresAt: string } | null>(null)
  const [error, setError] = useState('')
  const [generating, setGenerating] = useState(false)
  const [disconnecting, setDisconnecting] = useState(false)

  async function generateLink() {
    setGenerating(true)
    setError('')
    try {
      setLink(await crmApi.createGmailConnectLink(candidate.id))
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setGenerating(false)
    }
  }

  async function disconnect() {
    setDisconnecting(true)
    try {
      await crmApi.disconnectGmail(candidate.id)
      onChanged()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setDisconnecting(false)
    }
  }

  return (
    <div className="crm-card">
      <div className="crm-card-title">Gmail ulanishi</div>
      <div className="crm-card-desc">Kandidat o'z Gmail hisobini ulasin — arizalar shu manzildan yuboriladi.</div>

      <div className={'crm-conn' + (connected ? '' : ' off')}>
        <span>{connected ? candidate.gmailEmail : 'Ulanmagan'}</span>
        <span>{connected ? 'Ulangan' : 'Hali ulanmagan'}</span>
      </div>

      {connected ? (
        <div className="crm-actions">
          <button className="btn-quiet crm-danger" disabled={disconnecting} onClick={disconnect}>
            {disconnecting ? 'Uzilmoqda…' : 'Uzish'}
          </button>
        </div>
      ) : (
        <>
          <div className="crm-actions">
            <button className="btn-primary" disabled={generating} onClick={generateLink}>
              {generating ? 'Yaratilmoqda…' : 'Ulash havolasini yaratish'}
            </button>
          </div>
          {link && (
            <div className="crm-vac-detail" style={{ marginTop: 10 }}>
              <div className="crm-item-sub">
                Bu havolani kandidatga yuboring (Telegram, WhatsApp va h.k.) — u o'zi bosib, o'z Gmail hisobini ulaydi.
                Havola {new Date(link.expiresAt).toLocaleString()} gacha amal qiladi.
              </div>
              <input className="crm-input" readOnly value={link.url} onFocus={(e) => e.target.select()} style={{ marginTop: 8 }} />
            </div>
          )}
        </>
      )}

      {error && <div className="state" style={{ padding: '6px 0' }}>{error}</div>}
    </div>
  )
}

// --- step 3 --------------------------------------------------------------

function ProfileStep({ candidate, onChanged }: { candidate: Candidate; onChanged: () => void }) {
  const [specialties, setSpecialties] = useState<string[]>([])
  useEffect(() => { crmApi.specialties().then(setSpecialties).catch(() => {}) }, [])

  return (
    <>
      {(candidate.profiles?.length ?? 0) > 0 && (
        <div className="crm-card">
          <div className="crm-card-title">Mavjud profillar</div>
          <div className="crm-list">
            {candidate.profiles!.map((p) => (
              <div key={p.id} className="crm-item" style={{ cursor: 'default' }}>
                <div className="crm-item-main">
                  <div className="crm-item-title">{p.name}</div>
                  <div className="crm-item-sub">{(p.specialties ?? []).map((s) => s.specialty).join(', ')}</div>
                </div>
                <button
                  className="btn-quiet crm-danger"
                  onClick={() => crmApi.deleteProfile(candidate.id, p.id).then(onChanged)}
                >
                  O'chirish
                </button>
              </div>
            ))}
          </div>
        </div>
      )}

      <DocumentLibrary candidate={candidate} onChanged={onChanged} />

      <NewProfileForm candidate={candidate} specialties={specialties} onCreated={onChanged} />
    </>
  )
}

function DocumentLibrary({ candidate, onChanged }: { candidate: Candidate; onChanged: () => void }) {
  const fileRef = useRef<HTMLInputElement>(null)
  const [docType, setDocType] = useState('cv')
  const [name, setName] = useState('')
  const [language, setLanguage] = useState('')
  const [keywords, setKeywords] = useState('')
  const [isPrimary, setIsPrimary] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [error, setError] = useState('')

  async function upload() {
    const file = fileRef.current?.files?.[0]
    if (!file) {
      setError('Fayl tanlang')
      return
    }
    setUploading(true)
    setError('')
    try {
      await crmApi.uploadDocument(candidate.id, file, {
        type: docType, name, language, keywords, isPrimaryCv: isPrimary,
      })
      setName(''); setKeywords(''); setIsPrimary(false)
      if (fileRef.current) fileRef.current.value = ''
      onChanged()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setUploading(false)
    }
  }

  const docs = candidate.documents ?? []

  return (
    <div className="crm-card">
      <div className="crm-card-title">Hujjatlar</div>
      <div className="crm-card-desc">Faylni bir marta yuklang va uni CV, profil yoki har bir ariza uchun qayta ishlating.</div>

      {docs.length === 0 && <div className="crm-item-sub" style={{ marginBottom: 10 }}>Hali hujjat yo'q.</div>}
      <div className="crm-doc-list">
        {docs.map((d: Document) => (
          <div key={d.id} className="crm-doc-item">
            <span className="crm-doc-name">
              {d.name || d.originalFilename}
              {d.isPrimaryCv && ' · Asosiy'}
            </span>
            <span className="crm-doc-meta">{DOC_TYPES.find((t) => t.value === d.type)?.label ?? d.type}</span>
            <a className="btn-quiet" href={crmApi.documentFileURL(candidate.id, d.id)} target="_blank" rel="noreferrer">
              Ko'rish
            </a>
            <button className="btn-quiet crm-danger" onClick={() => crmApi.deleteDocument(candidate.id, d.id).then(onChanged)}>
              O'chirish
            </button>
          </div>
        ))}
      </div>

      <div className="crm-card-title" style={{ fontSize: 13.5, marginTop: 16 }}>Hujjat qo'shish</div>
      <div className="crm-row">
        <div className="crm-field">
          <label className="crm-field-label">Turi</label>
          <select className="crm-select" value={docType} onChange={(e) => setDocType(e.target.value)}>
            {DOC_TYPES.map((t) => <option key={t.value} value={t.value}>{t.label}</option>)}
          </select>
        </div>
        <div className="crm-field">
          <label className="crm-field-label">Fayl</label>
          <input ref={fileRef} className="crm-input" type="file" />
        </div>
      </div>
      <div className="crm-row">
        <div className="crm-field">
          <label className="crm-field-label">Nomi (ixtiyoriy)</label>
          <input className="crm-input" value={name} onChange={(e) => setName(e.target.value)} />
        </div>
        <div className="crm-field">
          <label className="crm-field-label">Til</label>
          <select className="crm-select" value={language} onChange={(e) => setLanguage(e.target.value)}>
            {DOC_LANGUAGES.map((l) => <option key={l} value={l}>{l || 'Tanlanmagan'}</option>)}
          </select>
        </div>
      </div>
      <div className="crm-field">
        <label className="crm-field-label">Kalit so'zlar</label>
        <input className="crm-input" value={keywords} onChange={(e) => setKeywords(e.target.value)} />
      </div>
      {docType === 'cv' && (
        <label className="crm-item-sub" style={{ display: 'flex', gap: 6, alignItems: 'center', marginBottom: 12 }}>
          <input type="checkbox" checked={isPrimary} onChange={(e) => setIsPrimary(e.target.checked)} />
          Bu CVni asosiy CV qilib belgilang.
        </label>
      )}
      {error && <div className="state" style={{ padding: '6px 0' }}>{error}</div>}
      <div className="crm-actions">
        <button className="btn-primary" disabled={uploading} onClick={upload}>
          {uploading ? 'Yuklanmoqda…' : "Kutubxonaga qo'shish"}
        </button>
      </div>
    </div>
  )
}

function NewProfileForm({
  candidate,
  specialties,
  onCreated,
}: {
  candidate: Candidate
  specialties: string[]
  onCreated: () => void
}) {
  const [name, setName] = useState('')
  const [cvId, setCvId] = useState('')
  const [coverId, setCoverId] = useState('')
  const [motivationId, setMotivationId] = useState('')
  const [selected, setSelected] = useState<string[]>([])
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  const docs = candidate.documents ?? []
  const byType = (t: string) => docs.filter((d) => d.type === t)

  function toggle(s: string) {
    setSelected((prev) => (prev.includes(s) ? prev.filter((x) => x !== s) : [...prev, s]))
  }

  async function create() {
    if (!name || selected.length === 0) {
      setError("Nomi va kamida bitta yo'nalish kerak")
      return
    }
    setSaving(true)
    setError('')
    try {
      const specs: ProfileSpecialty[] = selected.map((s) => ({ specialty: s }))
      await crmApi.createProfile(candidate.id, {
        name,
        cvDocumentId: cvId || undefined,
        coverLetterDocumentId: coverId || undefined,
        motivationLetterDocumentId: motivationId || undefined,
        specialties: specs,
      })
      setName(''); setCvId(''); setCoverId(''); setMotivationId(''); setSelected([])
      onCreated()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="crm-card">
      <div className="crm-card-title">Ariza profili</div>
      <div className="crm-card-desc">Har bir profilga CV, ariza xati, motivatsiya xati va kamida bitta yo'nalish kiradi.</div>

      <div className="crm-field">
        <label className="crm-field-label">Profil nomi</label>
        <input className="crm-input" placeholder="Masalan, Dialyse — nemischa" value={name} onChange={(e) => setName(e.target.value)} />
      </div>

      <div className="crm-row">
        <DocPicker label="CV" docs={byType('cv')} value={cvId} onChange={setCvId} />
        <DocPicker label="Ariza xati" docs={byType('cover_letter')} value={coverId} onChange={setCoverId} />
        <DocPicker label="Motivatsiya xati" docs={byType('motivation_letter')} value={motivationId} onChange={setMotivationId} />
      </div>

      <div className="crm-field">
        <label className="crm-field-label">Yo'nalishlar</label>
        <div className="crm-chips">
          {specialties.map((s) => (
            <button key={s} type="button" className={'crm-chip' + (selected.includes(s) ? ' on' : '')} onClick={() => toggle(s)}>
              {s}
            </button>
          ))}
        </div>
      </div>

      {error && <div className="state" style={{ padding: '6px 0' }}>{error}</div>}
      <div className="crm-actions">
        <button className="btn-primary" disabled={saving} onClick={create}>
          {saving ? 'Saqlanmoqda…' : 'Profil yaratish'}
        </button>
      </div>
    </div>
  )
}

function DocPicker({
  label,
  docs,
  value,
  onChange,
}: {
  label: string
  docs: Document[]
  value: string
  onChange: (v: string) => void
}) {
  return (
    <div className="crm-field">
      <label className="crm-field-label">{label}</label>
      <select className="crm-select" value={value} onChange={(e) => onChange(e.target.value)}>
        <option value="">Kutubxonadan tanlash</option>
        {docs.map((d) => (
          <option key={d.id} value={d.id}>{d.name || d.originalFilename}</option>
        ))}
      </select>
    </div>
  )
}
