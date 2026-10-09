import { useEffect, useRef, useState } from 'react'
import { crmApi } from './api'
import { DIRECTIONS, DIRECTION_LABEL, DIRECTION_ARBEITSAGENTUR } from './api'
import type { Candidate, CandidateInput, Direction, Document, GmailMailbox, ProfileSpecialty, ScrapeResult, ScrapeSource } from './api'
import { dateLabel, specialtyLabel } from './labels'

const GERMAN_LEVELS = ['', 'A1', 'A2', 'B1', 'B2', 'C1', 'C2']
const DOC_TYPES = [
  { value: 'cv', label: 'CV (Lebenslauf)' },
  { value: 'cover_letter', label: 'Ariza xati' },
  { value: 'motivation_letter', label: 'Motivatsiya xati' },
  { value: 'certificate', label: 'Sertifikat' },
  { value: 'other', label: 'Boshqa hujjat' },
]
const DOC_LANGUAGES = ['', 'Nemis', 'Ingliz', 'Rus', "O'zbek"]

// CandidatesPage is the "Kandidatlar" section's entry point: a 4-direction
// home screen first (MFA/ZFA is the only one with a working scraper today —
// the other three show as "tez orada"), then a direction-filtered candidate
// list, then a candidate's own detail/wizard page.
export function CandidatesPage() {
  const [direction, setDirection] = useState<Direction | null>(null)
  const [selectedId, setSelectedId] = useState<string | null>(null)

  if (selectedId) {
    return (
      <CandidateDetail
        id={selectedId}
        onBack={() => setSelectedId(null)}
      />
    )
  }
  if (direction) {
    return (
      <CandidateList
        direction={direction}
        onBack={() => setDirection(null)}
        onOpen={setSelectedId}
      />
    )
  }
  return <DirectionHome onPick={setDirection} />
}

function DirectionHome({ onPick }: { onPick: (d: Direction) => void }) {
  const [counts, setCounts] = useState<Record<string, number>>({})

  useEffect(() => {
    crmApi.candidates().then((r) => {
      const next: Record<string, number> = {}
      for (const c of r.candidates) next[c.direction] = (next[c.direction] ?? 0) + 1
      setCounts(next)
    }).catch(() => {})
  }, [])

  return (
    <div className="crm-page">
      <div className="crm-header">
        <div>
          <div className="crm-title">Yo'nalishlar</div>
          <div className="crm-subtitle">Kandidat qaysi yo'nalishda bo'lsa, o'sha bo'limga kiring.</div>
        </div>
      </div>
      <div className="crm-direction-grid">
        {DIRECTIONS.map((d) => (
          <button key={d} className="crm-direction-tile" onClick={() => onPick(d)}>
            <div className="crm-direction-name">{DIRECTION_LABEL[d]}</div>
            <div className="crm-direction-count">{`${counts[d] ?? 0} kandidat`}</div>
          </button>
        ))}
      </div>
    </div>
  )
}

function CandidateList({
  direction,
  onBack,
  onOpen,
}: {
  direction: Direction
  onBack: () => void
  onOpen: (id: string) => void
}) {
  const [candidates, setCandidates] = useState<Candidate[] | null>(null)
  const [error, setError] = useState('')

  function reload() {
    crmApi
      .candidates({ direction })
      .then((r) => setCandidates(r.candidates))
      .catch((e: Error) => setError(e.message))
  }
  useEffect(reload, [direction])

  async function createCandidate() {
    try {
      const c = await crmApi.createCandidate({ fullName: 'Yangi kandidat', direction })
      setCandidates((prev) => [...(prev ?? []), c])
      onOpen(c.id)
    } catch (e) {
      setError((e as Error).message)
    }
  }

  return (
    <div className="crm-page">
      <button className="btn-quiet" onClick={onBack} style={{ marginBottom: 10 }}>← Yo'nalishlar</button>

      <div className="crm-header">
        <div style={{ flex: 1 }}>
          <div className="crm-title">{DIRECTION_LABEL[direction]} kandidatlari</div>
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
          <div key={c.id} className="crm-item" onClick={() => onOpen(c.id)}>
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

type Step = 'basic' | 'gmail' | 'profile' | 'scrape'

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
        <button className={'crm-step' + (step === 'scrape' ? ' active' : '')} onClick={() => setStep('scrape')}>
          <span className="crm-step-num">4</span>
          Scrape va yuborish
        </button>
      </div>

      {step === 'basic' && <BasicInfoStep candidate={candidate} onSaved={setCandidate} />}
      {step === 'gmail' && <GmailStep candidate={candidate} onChanged={reload} />}
      {step === 'profile' && <ProfileStep candidate={candidate} onChanged={reload} />}
      {step === 'scrape' && <ScrapeStep candidate={candidate} />}
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
    direction: candidate.direction,
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
      <div className="crm-field">
        <label className="crm-field-label">Yo'nalish *</label>
        <select className="crm-select" value={form.direction} onChange={(e) => setForm({ ...form, direction: e.target.value as CandidateInput['direction'] })}>
          {DIRECTIONS.map((d) => <option key={d} value={d}>{DIRECTION_LABEL[d]}</option>)}
        </select>
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
// own click drives the OAuth consent screen. A candidate can connect up to
// 4 mailboxes — sending rotates across whichever ones still have room under
// their own daily cap, so one mailbox hitting its limit doesn't stall
// applications.

const MAILBOX_SLOTS = ['1', '2', '3', '4']

function GmailStep({ candidate, onChanged }: { candidate: Candidate; onChanged: () => void }) {
  return (
    <div className="crm-card">
      <div className="crm-card-title">Gmail hisoblari</div>
      <div className="crm-card-desc">
        Kandidat bir nechta Gmail hisobini ulashi mumkin (4 tagacha) — arizalar shulardan navbat bilan yuboriladi;
        bittasi kunlik limitga yetsa, tizim avtomatik keyingisiga o'tadi.
      </div>
      <div className="crm-mailbox-grid">
        {MAILBOX_SLOTS.map((slot) => (
          <MailboxCard
            key={slot}
            candidateId={candidate.id}
            slot={slot}
            mailbox={candidate.gmailMailboxes?.find((m) => m.slot === slot)}
            onChanged={onChanged}
          />
        ))}
      </div>
    </div>
  )
}

function MailboxCard({
  candidateId,
  slot,
  mailbox,
  onChanged,
}: {
  candidateId: string
  slot: string
  mailbox?: GmailMailbox
  onChanged: () => void
}) {
  const connected = !!mailbox?.email
  const [link, setLink] = useState<{ url: string; expiresAt: string } | null>(null)
  const [error, setError] = useState('')
  const [generating, setGenerating] = useState(false)
  const [disconnecting, setDisconnecting] = useState(false)
  const [cap, setCap] = useState(mailbox?.dailyCap || 30)
  const [savingCap, setSavingCap] = useState(false)

  async function generateLink() {
    setGenerating(true)
    setError('')
    try {
      setLink(await crmApi.createGmailConnectLink(candidateId, slot))
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setGenerating(false)
    }
  }

  async function disconnect() {
    setDisconnecting(true)
    try {
      await crmApi.disconnectGmail(candidateId, slot)
      onChanged()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setDisconnecting(false)
    }
  }

  async function saveCap() {
    setSavingCap(true)
    setError('')
    try {
      await crmApi.setMailboxCap(candidateId, slot, cap)
      onChanged()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setSavingCap(false)
    }
  }

  return (
    <div className={'crm-mailbox' + (connected ? '' : ' off')}>
      <div className="crm-mailbox-head">
        <span className="crm-mailbox-slot">#{slot}</span>
        <span className="crm-mailbox-email">{connected ? mailbox!.email : 'Ulanmagan'}</span>
      </div>

      {connected ? (
        <>
          <div className="crm-item-sub">Bugun yuborildi: {mailbox!.sentToday} / {mailbox!.dailyCap || 250}</div>
          <div className="crm-row" style={{ marginTop: 8 }}>
            <div className="crm-field" style={{ maxWidth: 100 }}>
              <label className="crm-field-label">Kunlik limit</label>
              <input
                className="crm-input"
                type="number"
                min={1}
                max={250}
                value={cap}
                onChange={(e) => setCap(Number(e.target.value) || 1)}
              />
            </div>
            <button className="btn-quiet" disabled={savingCap} onClick={saveCap} style={{ alignSelf: 'flex-end', marginBottom: 2 }}>
              {savingCap ? 'Saqlanmoqda…' : 'Saqlash'}
            </button>
          </div>
          <div className="crm-actions">
            <button className="btn-quiet crm-danger" disabled={disconnecting} onClick={disconnect}>
              {disconnecting ? 'Uzilmoqda…' : 'Uzish'}
            </button>
          </div>
        </>
      ) : (
        <>
          <div className="crm-actions">
            <button className="btn-quiet" disabled={generating} onClick={generateLink}>
              {generating ? 'Yaratilmoqda…' : 'Ulash havolasini yaratish'}
            </button>
          </div>
          {link && (
            <div className="crm-vac-detail" style={{ marginTop: 8 }}>
              <div className="crm-item-sub">
                Havolani kandidatga yuboring (Telegram, WhatsApp va h.k.). {dateLabel(link.expiresAt, true)} gacha amal qiladi.
              </div>
              <input className="crm-input" readOnly value={link.url} onFocus={(e) => e.target.select()} style={{ marginTop: 6 }} />
            </div>
          )}
        </>
      )}

      {error && <div className="state" style={{ padding: '6px 0' }}>{error}</div>}
    </div>
  )
}

// --- step 4 ----------------------------------------------------------------
// Fetches fresh vacancies — from arbeitsagentur.de scoped to one city (for
// directions that have a search there), or from any page URL the AI reads —
// merges them into a shared on-demand pool, then immediately runs
// auto-apply scoped to just this candidate. Real
// sending requires two deliberate checks (arm + confirm) — never just one
// checkbox — so a misclick can't email a real employer.

function ScrapeStep({ candidate }: { candidate: Candidate }) {
  const hasArbeitsagentur = DIRECTION_ARBEITSAGENTUR[candidate.direction]
  const [source, setSource] = useState<ScrapeSource>(hasArbeitsagentur ? 'arbeitsagentur' : 'site')
  const [siteUrl, setSiteUrl] = useState('')
  const [city, setCity] = useState('')
  const [radiusKm, setRadiusKm] = useState(50)
  const [onlyNew, setOnlyNew] = useState(true)
  const [realSend, setRealSend] = useState(false)
  const [confirmed, setConfirmed] = useState(false)
  const [running, setRunning] = useState(false)
  const [result, setResult] = useState<ScrapeResult | null>(null)
  const [error, setError] = useState('')

  const armed = realSend && confirmed
  const testMode = !armed

  function setRealSendChecked(v: boolean) {
    setRealSend(v)
    if (!v) setConfirmed(false) // unchecking "real send" always disarms confirmation too
  }

  async function run() {
    if (source === 'arbeitsagentur' && !city.trim()) {
      setError('Shahar nomini kiriting')
      return
    }
    if (source === 'site' && !siteUrl.trim()) {
      setError('Sayt manzilini kiriting')
      return
    }
    setRunning(true)
    setError('')
    setResult(null)
    try {
      setResult(
        await crmApi.scrapeCandidate(
          candidate.id,
          source === 'site' ? { source, url: siteUrl, onlyNew, testMode } : { source, city, radiusKm, onlyNew, testMode },
        ),
      )
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setRunning(false)
    }
  }

  return (
    <div className="crm-card">
      <div className="crm-card-title">Scrape va ariza yuborish</div>
      <div className="crm-card-desc">
        Tanlangan saytdan yangi vakansiyalarni qidiradi, so'ng ularni shu kandidat bilan solishtirib, mos
        kelganlariga ariza tayyorlaydi.
      </div>

      <div className="crm-field">
        <label className="crm-field-label">Qaysi saytdan</label>
        <div className="crm-source-options">
          <label className={'crm-source-option' + (hasArbeitsagentur ? '' : ' disabled')}>
            <input
              type="radio"
              name="scrape-source"
              checked={source === 'arbeitsagentur'}
              disabled={!hasArbeitsagentur}
              onChange={() => setSource('arbeitsagentur')}
            />
            <span>
              <strong>arbeitsagentur.de</strong> (tavsiya etiladi)
              <span className="crm-item-sub">
                {hasArbeitsagentur
                  ? "Germaniya mehnat agentligi — rasmiy baza, shahar va radius bo'yicha"
                  : "Bu yo'nalish uchun arbeitsagentur.de qidiruvi yo'q"}
              </span>
            </span>
          </label>
          <label className="crm-source-option">
            <input type="radio" name="scrape-source" checked={source === 'site'} onChange={() => setSource('site')} />
            <span>
              <strong>Boshqa sayt</strong>
              <span className="crm-item-sub">Istalgan karyera yoki ish e'lonlari sahifasi — AI sahifani o'qib, e'lonlarni ajratadi</span>
            </span>
          </label>
        </div>
      </div>

      {source === 'arbeitsagentur' ? (
        <div className="crm-row">
          <div className="crm-field">
            <label className="crm-field-label">Shahar (yoki hudud)</label>
            <input className="crm-input" placeholder="Masalan, Berlin" value={city} onChange={(e) => setCity(e.target.value)} />
          </div>
          <div className="crm-field" style={{ maxWidth: 120 }}>
            <label className="crm-field-label">Radius (km)</label>
            <input className="crm-input" type="number" min={1} value={radiusKm} onChange={(e) => setRadiusKm(Number(e.target.value) || 50)} />
          </div>
        </div>
      ) : (
        <div className="crm-field">
          <label className="crm-field-label">Sahifa manzili (URL)</label>
          <input
            className="crm-input"
            type="url"
            placeholder="https://www.praxis-beispiel.de/karriere"
            value={siteUrl}
            onChange={(e) => setSiteUrl(e.target.value)}
          />
          <div className="crm-item-sub" style={{ marginTop: 4 }}>
            E'lonlar ro'yxati ko'rinadigan sahifa havolasini kiriting. Kontentni JavaScript bilan yuklaydigan saytlar
            (masalan, LinkedIn, Indeed) ishlamasligi mumkin.
          </div>
        </div>
      )}

      <label className="crm-item-sub" style={{ display: 'flex', gap: 6, alignItems: 'center', marginBottom: 10 }}>
        <input type="checkbox" checked={onlyNew} onChange={(e) => setOnlyNew(e.target.checked)} />
        Faqat yangi ishlar (avval topilgan ishlarga qayta ariza yubormaslik)
      </label>

      <div className="crm-realsend-box">
        <label className="crm-item-sub" style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
          <input type="checkbox" checked={realSend} onChange={(e) => setRealSendChecked(e.target.checked)} />
          Sinov rejimini o'chirish — <strong>HAQIQIY</strong> xat ish beruvchiga yuboriladi
        </label>
        {realSend && (
          <label className="crm-item-sub crm-danger" style={{ display: 'flex', gap: 6, alignItems: 'center', marginTop: 8 }}>
            <input type="checkbox" checked={confirmed} onChange={(e) => setConfirmed(e.target.checked)} />
            Men tushunaman — bu ish beruvchilarga haqiqiy email yuboradi, orqaga qaytarib bo'lmaydi
          </label>
        )}
      </div>

      <div className="crm-actions">
        <button className={armed ? 'btn-primary crm-danger-btn' : 'btn-primary'} disabled={running} onClick={run}>
          {running ? 'Ishlamoqda…' : armed ? 'Scrape va HAQIQIY ariza yubor' : 'Scrape va ariza yubor (sinov)'}
        </button>
      </div>

      {error && <div className="state" style={{ padding: '6px 0' }}>{error}</div>}
      {result && (
        <div className="crm-vac-detail" style={{ marginTop: 12 }}>
          <div className="crm-item-sub">
            {result.dryRun ? 'Sinov rejimi' : 'HAQIQIY yuborildi'} · Topildi: {result.foundJobs} · Yangi: {result.newJobs} ·
            Ko'rib chiqildi: {result.stats.considered} · Yuborilardi: {result.stats.sent} ·
            O'tkazib yuborildi: {result.stats.skipped} · Xato: {result.stats.failed}
          </div>
          {result.stats.details.length > 0 && (
            <div className="crm-vac-text" style={{ marginTop: 8 }}>{result.stats.details.join('\n')}</div>
          )}
        </div>
      )}
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
                  <div className="crm-item-sub">{(p.specialties ?? []).map((s) => specialtyLabel(s.specialty)).join(', ')}</div>
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
      {docType === 'cv' && (
        <div className="crm-item-sub" style={{ marginBottom: 10 }}>
          CV'ni PDF formatida yuklang — AI xat yozishda faqat PDF CV'ni o'qiy oladi. Rasm yoki boshqa format
          xatga ilova bo'lib ketadi, lekin AI uni o'qimaydi.
        </div>
      )}
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
              {specialtyLabel(s)}
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
