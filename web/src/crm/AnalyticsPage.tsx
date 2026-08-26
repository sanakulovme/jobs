import { useEffect, useState } from 'react'
import { crmApi } from './api'
import type { Analytics, Reply } from './api'

const CATEGORY_LABEL: Record<string, string> = {
  green: 'Yashil',
  yellow: 'Sariq',
  red: 'Qizil',
  unknown: 'Aniqlanmagan',
}
const CATEGORY_TAG: Record<string, string> = {
  green: 'green',
  yellow: 'yellow',
  red: 'red',
  unknown: 'gray',
}

export function AnalyticsPage() {
  const [analytics, setAnalytics] = useState<Analytics | null>(null)
  const [error, setError] = useState('')

  function reload() {
    crmApi.analytics().then(setAnalytics).catch((e: Error) => setError(e.message))
  }
  useEffect(reload, [])

  return (
    <div className="crm-page">
      <div className="crm-header">
        <div>
          <div className="crm-title">Analitika</div>
          <div className="crm-subtitle">Scrape, yuborilgan xatlar va ish beruvchilardan kelgan javoblar.</div>
        </div>
      </div>

      {/* RunPanel ("Hozir boshlash") hidden for now — see the commented-out
          component below. Uncomment both to bring it back. */}

      {error && <div className="state">{error}</div>}
      {analytics && (
        <>
          <div className="crm-tiles">
            <Tile label="Scrape qilingan ish" value={analytics.scrapedJobs} sub={`${analytics.scrapedWithEmail} tasida email bor`} />
            <Tile label="Yuborilgan xat" value={analytics.sentApplications} />
            <Tile label="Javob qaytardi" value={analytics.replies} sub={`${analytics.replyRatePct}% javob darajasi`} />
            <Tile label="Ijobiylik darajasi" value={`${analytics.positivityRatePct}%`} sub={`${analytics.positiveReplies} / ${analytics.classifiedReplies} ijobiy`} />
          </div>

          <div className="crm-card">
            <div className="crm-card-title">Oxirgi 14 kun</div>
            <Timeline points={analytics.timeline} />
          </div>
        </>
      )}

      <RepliesFeed onChecked={reload} />
    </div>
  )
}

function Tile({ label, value, sub }: { label: string; value: number | string; sub?: string }) {
  return (
    <div className="crm-tile">
      <div className="crm-tile-label">{label}</div>
      <div className="crm-tile-value">{value}</div>
      {sub && <div className="crm-tile-sub">{sub}</div>}
    </div>
  )
}

function Timeline({ points }: { points: Analytics['timeline'] }) {
  const max = Math.max(1, ...points.map((p) => Math.max(p.sent, p.replies)))
  return (
    <div>
      <div className="crm-chart-legend">
        <span className="crm-chart-legend-item"><span className="crm-chart-legend-dot" style={{ background: 'var(--blue)' }} />Yuborilgan</span>
        <span className="crm-chart-legend-item"><span className="crm-chart-legend-dot" style={{ background: 'var(--yellow)' }} />Javoblar</span>
      </div>
      <div className="crm-chart">
        {points.map((p) => (
          <div key={p.date} className="crm-chart-col">
            <div className="crm-chart-bars">
              <div className="crm-chart-bar" style={{ height: `${(p.sent / max) * 100}%`, background: 'var(--blue)' }} title={`${p.date}: ${p.sent} yuborilgan`} />
              <div className="crm-chart-bar" style={{ height: `${(p.replies / max) * 100}%`, background: 'var(--yellow)' }} title={`${p.date}: ${p.replies} javob`} />
            </div>
            <div className="crm-chart-date">{p.date.slice(5)}</div>
          </div>
        ))}
      </div>
    </div>
  )
}

/* RunPanel — hidden from the page for now (see the commented-out call in
   AnalyticsPage above). Left in place, not deleted, so re-enabling it later
   is a two-line uncomment rather than rewriting it from scratch.

function RunPanel({ onRun }: { onRun: () => void }) {
  const [count, setCount] = useState(25)
  const [running, setRunning] = useState(false)
  const [result, setResult] = useState<{ considered: number; sent: number; skipped: number; failed: number; noEmail: number; details: string[] } | null>(null)
  const [error, setError] = useState('')

  async function run() {
    setRunning(true)
    setError('')
    setResult(null)
    try {
      // testMode is always true here: real sending needs internal/gmail
      // (phase 6, blocked on Google OAuth credentials), which doesn't exist
      // in this build yet — the backend rejects testMode:false with 501.
      const r = await crmApi.runPipeline(count, true)
      setResult(r.stats)
      onRun()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setRunning(false)
    }
  }

  return (
    <div className="crm-card">
      <div className="crm-card-title">Qo'lda ishga tushirish</div>
      <div className="crm-card-desc">Mavjud vakansiyalarni kandidatlar bilan solishtirib, moslik hisobotini chiqaradi.</div>

      <div className="crm-run-panel">
        <div className="crm-field" style={{ maxWidth: 140 }}>
          <label className="crm-field-label">Nechta vakansiya tekshirilsin</label>
          <input className="crm-input" type="number" min={1} value={count} onChange={(e) => setCount(Number(e.target.value) || 1)} />
        </div>
        <label className="crm-item-sub" style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
          <input type="checkbox" checked disabled />
          Sinov rejimi (xat yuborilmaydi)
        </label>
        <button className="btn-primary" disabled={running} onClick={run}>
          {running ? 'Ishlamoqda…' : 'Hozir boshlash'}
        </button>
      </div>
      <div className="crm-run-note">
        Gmail integratsiyasi hali ulanmagan, shuning uchun sinov rejimi majburiy — hech qanday xat haqiqatda yuborilmaydi,
        faqat qaysi kandidat-vakansiya juftliklari yuborilishi kerakligi hisoblanadi.
      </div>

      {error && <div className="state" style={{ padding: '6px 0' }}>{error}</div>}
      {result && (
        <div className="crm-vac-detail" style={{ marginTop: 12 }}>
          <div className="crm-item-sub">
            Ko'rib chiqildi: {result.considered} · Yuborilardi: {result.sent} · O'tkazib yuborildi: {result.skipped} ·
            Xato: {result.failed} · Email yo'q: {result.noEmail}
          </div>
          {result.details.length > 0 && (
            <div className="crm-vac-text" style={{ marginTop: 8 }}>{result.details.join('\n')}</div>
          )}
        </div>
      )}
    </div>
  )
}

*/

function RepliesFeed({ onChecked }: { onChecked: () => void }) {
  const [category, setCategory] = useState('')
  const [data, setData] = useState<{ replies: Reply[]; total: number; counts: Record<string, number> } | null>(null)
  const [error, setError] = useState('')
  const [checking, setChecking] = useState(false)
  const [checkNote, setCheckNote] = useState('')

  function reload() {
    crmApi.replies(category || undefined).then(setData).catch((e: Error) => setError(e.message))
  }

  useEffect(reload, [category])

  async function handleCheck() {
    setChecking(true)
    setError('')
    setCheckNote('')
    try {
      const r = await crmApi.checkReplies()
      setCheckNote(`${r.checked} ta ariza tekshirildi, ${r.newReplies} ta yangi javob topildi${r.failed ? `, ${r.failed} ta xatolik` : ''}.`)
      reload()
      // Refresh the top-level stat tiles (reply count/rate, positivity,
      // 14-day timeline) too — they're fetched once on page load and don't
      // otherwise know a new reply just landed.
      onChecked()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setChecking(false)
    }
  }

  return (
    <div className="crm-card">
      <div className="crm-card-title-row">
        <div className="crm-card-title">Javoblar</div>
        <button className="btn-quiet" onClick={handleCheck} disabled={checking}>
          {checking ? 'Tekshirilmoqda…' : 'Javoblarni tekshirish'}
        </button>
      </div>
      {checkNote && <div className="crm-item-sub">{checkNote}</div>}

      <div className="crm-reply-filters">
        <button className={'crm-chip' + (category === '' ? ' on' : '')} onClick={() => setCategory('')}>
          Barchasi {data ? `(${data.total})` : ''}
        </button>
        {(['green', 'yellow', 'red', 'unknown'] as const).map((c) => (
          <button key={c} className={'crm-chip' + (category === c ? ' on' : '')} onClick={() => setCategory(c)}>
            {CATEGORY_LABEL[c]} {data ? `(${data.counts[c] ?? 0})` : ''}
          </button>
        ))}
      </div>

      {error && <div className="state">{error}</div>}
      {data && data.replies.length === 0 && (
        <div className="crm-empty">
          Hali javob yo'q — bu Gmail ulanib, xatlar yuborilgandan keyin avtomatik to'ldiriladi.
        </div>
      )}
      {data?.replies.map((rep) => (
        <div key={rep.id} className="crm-reply-item">
          <div className="crm-reply-top">
            <span className="crm-reply-employer">{rep.fromEmail || 'Noma\'lum'}</span>
            <span className={'jtag ' + CATEGORY_TAG[rep.category]}>{CATEGORY_LABEL[rep.category] ?? rep.category}</span>
          </div>
          <div className="crm-item-sub">{rep.subject}</div>
          {rep.summary && <div className="crm-item-sub">{rep.summary}</div>}
          <div className="crm-item-meta">{new Date(rep.receivedAt).toLocaleDateString()}</div>
        </div>
      ))}
    </div>
  )
}
