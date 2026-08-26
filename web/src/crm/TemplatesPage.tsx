import { useEffect, useRef, useState } from 'react'
import { crmApi } from './api'
import type { LetterTemplate } from './api'

export function TemplatesPage() {
  const [templates, setTemplates] = useState<LetterTemplate[] | null>(null)
  const [editing, setEditing] = useState<LetterTemplate | 'new' | null>(null)
  const [error, setError] = useState('')

  function reload() {
    crmApi
      .templates()
      .then((r) => setTemplates(r.templates))
      .catch((e: Error) => setError(e.message))
  }
  useEffect(reload, [])

  if (editing) {
    return (
      <TemplateEditor
        template={editing === 'new' ? null : editing}
        onDone={() => {
          setEditing(null)
          reload()
        }}
      />
    )
  }

  return (
    <div className="crm-page">
      <div className="crm-header">
        <div style={{ flex: 1 }}>
          <div className="crm-title">Xat shabloni</div>
          <div className="crm-subtitle">
            Ish beruvchiga yuboriladigan nemis tilidagi xat shablonlari. {'{{...}}'} o'rniga kandidat va vakansiya ma'lumotlari qo'yiladi.
          </div>
        </div>
        <button className="btn-primary" onClick={() => setEditing('new')}>+ Yangi shablon</button>
      </div>

      {error && <div className="state">{error}</div>}
      {templates === null && !error && <div className="crm-empty">Yuklanmoqda…</div>}
      {templates && templates.length === 0 && <div className="crm-empty">Hali shablon yo'q.</div>}

      <div className="crm-list">
        {templates?.map((t) => (
          <div key={t.id} className="crm-item" onClick={() => setEditing(t)}>
            <div className="crm-item-main">
              <div className="crm-item-title">
                {t.name}
                {t.isDefault && <span className="jtag green" style={{ marginLeft: 8 }}>Asosiy</span>}
                {t.specialty && <span className="jtag" style={{ marginLeft: 8 }}>{t.specialty}</span>}
              </div>
              <div className="crm-item-sub">{t.subject}</div>
            </div>
            <button
              className="btn-quiet crm-danger"
              onClick={(e) => {
                e.stopPropagation()
                crmApi.deleteTemplate(t.id).then(reload)
              }}
            >
              O'chirish
            </button>
          </div>
        ))}
      </div>
    </div>
  )
}

const TOKENS = [
  'vacancy_title', 'greeting', 'employer', 'city',
  'candidate_german_level', 'medical_specialty', 'candidate_name',
]

function TemplateEditor({ template, onDone }: { template: LetterTemplate | null; onDone: () => void }) {
  const [name, setName] = useState(template?.name ?? '')
  const [subject, setSubject] = useState(template?.subject ?? 'Bewerbung als {{vacancy_title}}')
  const [body, setBody] = useState(
    template?.body ??
      '{{greeting}}\n\nmit großem Interesse habe ich Ihre Stellenanzeige als {{vacancy_title}} bei {{employer}} in {{city}} gelesen und bewerbe mich hiermit um diese Position.\n\nIch verfüge über Deutschkenntnisse auf dem Niveau {{candidate_german_level}}.\n\nMit freundlichen Grüßen\n{{candidate_name}}',
  )
  const [specialty, setSpecialty] = useState(template?.specialty ?? '')
  const [isDefault, setIsDefault] = useState(template?.isDefault ?? false)
  const [specialtyOptions, setSpecialtyOptions] = useState<string[]>([])
  const [preview, setPreview] = useState<{ subject: string; body: string } | null>(null)
  const [previewError, setPreviewError] = useState('')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const debounceRef = useRef<number | undefined>(undefined)

  useEffect(() => { crmApi.specialties().then(setSpecialtyOptions).catch(() => {}) }, [])

  useEffect(() => {
    window.clearTimeout(debounceRef.current)
    debounceRef.current = window.setTimeout(() => {
      crmApi
        .previewDraft(subject, body)
        .then((r) => { setPreview(r); setPreviewError('') })
        .catch((e: Error) => { setPreview(null); setPreviewError(e.message) })
    }, 300)
    return () => window.clearTimeout(debounceRef.current)
  }, [subject, body])

  async function save() {
    if (!name || !subject || !body) {
      setError('Nomi, mavzu va matn kerak')
      return
    }
    setSaving(true)
    setError('')
    try {
      const input = { name, subject, body, specialty: specialty || undefined, isDefault }
      if (template) await crmApi.updateTemplate(template.id, input)
      else await crmApi.createTemplate(input)
      onDone()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="crm-page">
      <button className="btn-quiet" onClick={onDone} style={{ marginBottom: 10 }}>← Xat shabloni</button>

      <div className="crm-title" style={{ marginBottom: 14 }}>
        {template ? 'Shablonni tahrirlash' : 'Yangi shablon'}
      </div>

      <div className="crm-card">
        <div className="crm-field">
          <label className="crm-field-label">Nomi</label>
          <input className="crm-input" value={name} onChange={(e) => setName(e.target.value)} />
        </div>
        <div className="crm-row">
          <div className="crm-field">
            <label className="crm-field-label">Yo'nalish (ixtiyoriy — bo'sh bo'lsa har qanday vakansiyaga mos)</label>
            <select className="crm-select" value={specialty} onChange={(e) => setSpecialty(e.target.value)}>
              <option value="">Har qanday</option>
              {specialtyOptions.map((s) => <option key={s} value={s}>{s}</option>)}
            </select>
          </div>
          <label className="crm-item-sub" style={{ display: 'flex', gap: 6, alignItems: 'center', marginTop: 20 }}>
            <input type="checkbox" checked={isDefault} onChange={(e) => setIsDefault(e.target.checked)} />
            Asosiy shablon
          </label>
        </div>
        <div className="crm-field">
          <label className="crm-field-label">Mavzu</label>
          <input className="crm-input" value={subject} onChange={(e) => setSubject(e.target.value)} />
        </div>
        <div className="crm-field">
          <label className="crm-field-label">Matn</label>
          <textarea className="crm-textarea" style={{ minHeight: 180 }} value={body} onChange={(e) => setBody(e.target.value)} />
        </div>
        <div className="crm-item-sub">
          Mavjud token'lar: {TOKENS.map((t) => `{{${t}}}`).join(', ')}
        </div>

        {error && <div className="state" style={{ padding: '6px 0' }}>{error}</div>}
        <div className="crm-actions">
          <button className="btn-primary" disabled={saving} onClick={save}>
            {saving ? 'Saqlanmoqda…' : 'Saqlash'}
          </button>
        </div>
      </div>

      <div className="crm-card">
        <div className="crm-card-title">Namuna ko'rinish</div>
        <div className="crm-card-desc">Haqiqiy kandidat/vakansiya bo'lmasa, namuna ma'lumotlar bilan ko'rsatiladi.</div>
        {previewError && <div className="state" style={{ padding: '6px 0' }}>{previewError}</div>}
        {preview && (
          <>
            <div className="crm-vac-section-label">Mavzu</div>
            <div className="crm-vac-text">{preview.subject}</div>
            <div className="crm-vac-section-label">Matn</div>
            <div className="crm-vac-text">{preview.body}</div>
          </>
        )}
      </div>
    </div>
  )
}
