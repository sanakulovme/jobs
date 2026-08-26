import { useEffect, useState } from 'react'
import { crmApi } from './api'
import type { Application } from './api'

const STATUS_LABEL: Record<string, string> = {
  draft: 'Qoralama',
  sent: 'Yuborilgan',
  failed: 'Xato',
}
const STATUS_TAG: Record<string, string> = {
  draft: '',
  sent: 'green',
  failed: 'red',
}

export function ApplicationsPage() {
  const [applications, setApplications] = useState<Application[] | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    crmApi
      .applications()
      .then((r) => setApplications(r.applications))
      .catch((e: Error) => setError(e.message))
  }, [])

  return (
    <div className="crm-page">
      <div className="crm-header">
        <div>
          <div className="crm-title">Arizalar</div>
          <div className="crm-subtitle">Kandidatlarning vakansiyalarga yuborilgan (yoki sinov rejimida tayyorlangan) xatlari.</div>
        </div>
      </div>

      {error && <div className="state">{error}</div>}
      {applications === null && !error && <div className="crm-empty">Yuklanmoqda…</div>}
      {applications && applications.length === 0 && (
        <div className="crm-empty">
          Hali ariza yo'q. Gmail ulanmagani uchun avtomatik yuborish hali ishlamaydi — Analitika bo'limida sinov
          rejimida ("test mode") ishga tushirib ko'rishingiz mumkin.
        </div>
      )}

      <div className="crm-list">
        {applications?.map((app) => (
          <div key={app.id} className="crm-item" style={{ cursor: 'default' }}>
            <div className="crm-item-main">
              <div className="crm-item-title">{app.candidateName} → {app.employer}</div>
              <div className="crm-item-sub">{app.vacancyTitle}</div>
              <div className="crm-item-sub">
                {app.fromEmail || '—'} → {app.toEmail || '—'}
                {app.documentIds && app.documentIds.length > 0 && ` · ${app.documentIds.length} ilova`}
              </div>
              {app.error && <div className="crm-item-sub" style={{ color: 'var(--red)' }}>{app.error}</div>}
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              {STATUS_TAG[app.status] ? (
                <span className={'jtag ' + STATUS_TAG[app.status]}>{STATUS_LABEL[app.status] ?? app.status}</span>
              ) : (
                <span className="jtag">{STATUS_LABEL[app.status] ?? app.status}</span>
              )}
              <span className="crm-item-meta">{new Date(app.sentAt || app.createdAt).toLocaleDateString()}</span>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
