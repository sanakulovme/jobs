// Typed client for /api/crm/* — extends the plain getJSON pattern in ../api.ts
// with the write verbs (POST/PATCH/DELETE) and multipart upload the CRM needs.

export type Document = {
  id: string
  candidateId: string
  type: string
  name?: string
  language?: string
  keywords?: string
  isPrimaryCv: boolean
  originalFilename: string
  contentType?: string
  sizeBytes: number
  uploadedAt: string
}

export type ProfileSpecialty = { specialty: string; experienceYears?: number }

export type ApplicationProfile = {
  id: string
  candidateId: string
  name: string
  cvDocumentId?: string
  coverLetterDocumentId?: string
  motivationLetterDocumentId?: string
  specialties: ProfileSpecialty[]
  createdAt: string
  updatedAt: string
}

// Direction is the one recruiting vertical a candidate belongs to.
// 'mfa_zfa' and 'ausbildung' have a working scraper (both via Bundesagentur,
// just a different search term — see internal/httpapi/crm_scrape.go's
// directionScrapeConfigs, whose comment also flags real data-quality
// concerns with Ausbildung's results). 'til_kursi' was tried the same way
// and reverted — Bundesagentur returned zero relevant results for it, see
// that file's comment. 'au_pair' is shown as "tez orada" pending a source.
export type Direction = 'mfa_zfa' | 'ausbildung' | 'til_kursi' | 'au_pair'
export const DIRECTIONS: Direction[] = ['mfa_zfa', 'ausbildung', 'til_kursi', 'au_pair']
export const DIRECTION_LABEL: Record<Direction, string> = {
  mfa_zfa: 'MFA/ZFA',
  ausbildung: 'Ausbildung',
  til_kursi: 'Til kursi',
  au_pair: 'Au pair',
}
export const DIRECTION_READY: Record<Direction, boolean> = {
  mfa_zfa: true,
  ausbildung: true,
  til_kursi: false,
  au_pair: false,
}

export type GmailMailbox = {
  slot: string
  email?: string
  connectedAt?: string
  dailyCap: number
  sentToday: number
  sentTodayDate?: string
}

export type Candidate = {
  id: string
  fullName: string
  contactEmail?: string
  phone?: string
  germanLevel?: string
  citizenship?: string
  currentCountry?: string
  notes?: string
  direction: Direction
  gmailMailboxes?: GmailMailbox[]
  documents?: Document[]
  profiles?: ApplicationProfile[]
  createdAt: string
  updatedAt: string
}

export type DayPoint = { date: string; sent: number; replies: number }

export type Analytics = {
  scrapedJobs: number
  scrapedWithEmail: number
  sentApplications: number
  replies: number
  replyRatePct: number
  positiveReplies: number
  classifiedReplies: number
  positivityRatePct: number
  timeline: DayPoint[]
}

export type Reply = {
  id: string
  applicationId: string
  fromEmail?: string
  subject?: string
  body?: string
  category: string
  summary?: string
  receivedAt: string
}

export type Application = {
  id: string
  vacancyId: string
  candidateId: string
  candidateName: string
  vacancyTitle: string
  employer: string
  applicationProfileId?: string
  letterTemplateId?: string
  status: string
  subject: string
  body: string
  toEmail?: string
  fromEmail?: string
  documentIds?: string[]
  autoSent: boolean
  sentAt?: string
  error?: string
  createdAt: string
}

export type LetterTemplate = {
  id: string
  name: string
  subject: string
  body: string
  specialty?: string
  isDefault: boolean
  createdAt: string
  updatedAt: string
}

export type Vacancy = {
  id: string
  companyId: string
  company: string
  title: string
  location: string
  url: string
  description?: string
  postedAt: string
  source: string
  specialties?: string[]
  medicalSpecialty?: string
  applicationEmail?: string
  contactPerson?: string
  salutation?: string
  requiredGermanLevel?: string
  mainDuties?: string
  mandatoryRequirements?: string
}

export type Match = {
  candidate: Candidate
  profile: ApplicationProfile
  score: number
  matchedSpecialties: string[]
  germanOk: boolean
  gmailReady: boolean
  hasCv: boolean
  blockers: string[]
  alreadySent: boolean
}

export type CandidateInput = {
  fullName: string
  contactEmail?: string
  phone?: string
  germanLevel?: string
  citizenship?: string
  currentCountry?: string
  notes?: string
  direction: Direction
}

export type ScrapeResult = {
  foundJobs: number
  newJobs: number
  runId: string
  stats: { considered: number; sent: number; skipped: number; failed: number; noEmail: number; details: string[] }
  drafts: Application[]
  dryRun: boolean
}

async function json<T>(res: Response): Promise<T> {
  if (!res.ok) {
    let msg = `${res.status} ${res.statusText}`
    try {
      const body = await res.json()
      if (body?.error) msg = body.error
    } catch {
      /* body wasn't JSON */
    }
    throw new Error(msg)
  }
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

function getJSON<T>(url: string, signal?: AbortSignal): Promise<T> {
  return fetch(url, { signal, headers: { Accept: 'application/json' } }).then(json<T>)
}

function postJSON<T>(url: string, body: unknown, signal?: AbortSignal): Promise<T> {
  return fetch(url, {
    method: 'POST',
    signal,
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: JSON.stringify(body),
  }).then(json<T>)
}

function patchJSON<T>(url: string, body: unknown, signal?: AbortSignal): Promise<T> {
  return fetch(url, {
    method: 'PATCH',
    signal,
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: JSON.stringify(body),
  }).then(json<T>)
}

function del(url: string, signal?: AbortSignal): Promise<void> {
  return fetch(url, { method: 'DELETE', signal }).then(json<void>)
}

export const crmApi = {
  specialties: (signal?: AbortSignal) =>
    getJSON<{ specialties: string[] }>('/api/crm/specialties', signal).then((r) => r.specialties),

  candidates: (params: { direction?: Direction } = {}, signal?: AbortSignal) => {
    const p = new URLSearchParams()
    if (params.direction) p.set('direction', params.direction)
    return getJSON<{ candidates: Candidate[]; total: number }>('/api/crm/candidates?' + p.toString(), signal)
  },
  candidate: (id: string, signal?: AbortSignal) =>
    getJSON<Candidate>(`/api/crm/candidates/${encodeURIComponent(id)}`, signal),
  createCandidate: (input: CandidateInput) => postJSON<Candidate>('/api/crm/candidates', input),
  updateCandidate: (id: string, input: CandidateInput) =>
    patchJSON<Candidate>(`/api/crm/candidates/${encodeURIComponent(id)}`, input),
  deleteCandidate: (id: string) => del(`/api/crm/candidates/${encodeURIComponent(id)}`),

  uploadDocument: (candidateId: string, file: File, meta: Partial<Document> & { isPrimaryCv?: boolean }) => {
    const form = new FormData()
    form.set('file', file)
    if (meta.type) form.set('type', meta.type)
    if (meta.name) form.set('name', meta.name)
    if (meta.language) form.set('language', meta.language)
    if (meta.keywords) form.set('keywords', meta.keywords)
    if (meta.isPrimaryCv) form.set('isPrimaryCv', 'true')
    return fetch(`/api/crm/candidates/${encodeURIComponent(candidateId)}/documents`, {
      method: 'POST',
      body: form,
    }).then(json<Document>)
  },
  deleteDocument: (candidateId: string, docId: string) =>
    del(`/api/crm/candidates/${encodeURIComponent(candidateId)}/documents/${encodeURIComponent(docId)}`),
  documentFileURL: (candidateId: string, docId: string) =>
    `/api/crm/candidates/${encodeURIComponent(candidateId)}/documents/${encodeURIComponent(docId)}/file`,

  createProfile: (
    candidateId: string,
    input: {
      name: string
      cvDocumentId?: string
      coverLetterDocumentId?: string
      motivationLetterDocumentId?: string
      specialties: ProfileSpecialty[]
    },
  ) => postJSON<ApplicationProfile>(`/api/crm/candidates/${encodeURIComponent(candidateId)}/profiles`, input),
  deleteProfile: (candidateId: string, profileId: string) =>
    del(`/api/crm/candidates/${encodeURIComponent(candidateId)}/profiles/${encodeURIComponent(profileId)}`),

  createGmailConnectLink: (candidateId: string, slot: string) =>
    postJSON<{ url: string; expiresAt: string }>(
      `/api/crm/candidates/${encodeURIComponent(candidateId)}/gmail/${encodeURIComponent(slot)}/connect-link`,
      {},
    ),
  disconnectGmail: (candidateId: string, slot: string) =>
    postJSON<Candidate>(
      `/api/crm/candidates/${encodeURIComponent(candidateId)}/gmail/${encodeURIComponent(slot)}/disconnect`,
      {},
    ),
  setMailboxCap: (candidateId: string, slot: string, dailyCap: number) =>
    patchJSON<Candidate>(
      `/api/crm/candidates/${encodeURIComponent(candidateId)}/gmail/${encodeURIComponent(slot)}`,
      { dailyCap },
    ),

  scrapeCandidate: (
    candidateId: string,
    input: { city: string; radiusKm?: number; onlyNew: boolean; count?: number; testMode: boolean },
  ) => postJSON<ScrapeResult>(`/api/crm/candidates/${encodeURIComponent(candidateId)}/scrape`, input),

  vacancies: (params: { q?: string; specialty?: string; page?: number; pageSize?: number } = {}, signal?: AbortSignal) => {
    const p = new URLSearchParams()
    if (params.q) p.set('q', params.q)
    if (params.specialty) p.set('specialty', params.specialty)
    if (params.page) p.set('page', String(params.page))
    if (params.pageSize) p.set('pageSize', String(params.pageSize))
    return getJSON<{ vacancies: Vacancy[]; total: number; page: number; pageSize: number }>(
      '/api/crm/vacancies?' + p.toString(),
      signal,
    )
  },
  createVacancy: (input: {
    title: string
    company: string
    location?: string
    applicationEmail?: string
    contactPerson?: string
    salutation?: string
    url?: string
    description?: string
    medicalSpecialty?: string
    specialties: string[]
  }) => postJSON<Vacancy>('/api/crm/vacancies', input),
  setVacancySpecialties: (jobId: string, specialties: string[]) =>
    postJSON<{ jobId: string; specialties: string[] }>(
      `/api/crm/vacancies/${encodeURIComponent(jobId)}/specialties`,
      { specialties },
    ),
  vacancyMatches: (jobId: string, signal?: AbortSignal) =>
    getJSON<{ vacancy: Vacancy; matches: Match[] }>(
      `/api/crm/vacancies/${encodeURIComponent(jobId)}/matches`,
      signal,
    ),

  templates: (signal?: AbortSignal) =>
    getJSON<{ templates: LetterTemplate[]; total: number }>('/api/crm/templates', signal),
  createTemplate: (input: { name: string; subject: string; body: string; specialty?: string; isDefault?: boolean }) =>
    postJSON<LetterTemplate>('/api/crm/templates', input),
  updateTemplate: (
    id: string,
    input: { name: string; subject: string; body: string; specialty?: string; isDefault?: boolean },
  ) => patchJSON<LetterTemplate>(`/api/crm/templates/${encodeURIComponent(id)}`, input),
  deleteTemplate: (id: string) => del(`/api/crm/templates/${encodeURIComponent(id)}`),
  previewDraft: (subject: string, body: string) =>
    postJSON<{ subject: string; body: string }>('/api/crm/templates/preview', { subject, body }),

  applications: (
    params: { candidateId?: string; vacancyId?: string; status?: string } = {},
    signal?: AbortSignal,
  ) => {
    const p = new URLSearchParams()
    if (params.candidateId) p.set('candidateId', params.candidateId)
    if (params.vacancyId) p.set('vacancyId', params.vacancyId)
    if (params.status) p.set('status', params.status)
    return getJSON<{ applications: Application[]; total: number }>(
      '/api/crm/applications?' + p.toString(),
      signal,
    )
  },

  runPipeline: (count: number, testMode: boolean) =>
    postJSON<{ runId: string; stats: { considered: number; sent: number; skipped: number; failed: number; noEmail: number; details: string[] }; drafts: Application[]; dryRun: boolean }>(
      '/api/crm/run',
      { count, testMode },
    ),
  lastRun: (signal?: AbortSignal) =>
    getJSON<{ run: { id: string; type: string; status: string; startedAt?: string; finishedAt?: string; stats?: Record<string, unknown> } | null }>(
      '/api/crm/run',
      signal,
    ),

  analytics: (signal?: AbortSignal) => getJSON<Analytics>('/api/crm/analytics', signal),
  checkReplies: (limit?: number) =>
    postJSON<{ checked: number; newReplies: number; failed: number; byCategory: Record<string, number> }>(
      '/api/crm/replies/check',
      { limit },
    ),
  replies: (category?: string, signal?: AbortSignal) => {
    const p = new URLSearchParams()
    if (category) p.set('category', category)
    return getJSON<{ replies: Reply[]; total: number; counts: Record<string, number> }>(
      '/api/crm/replies?' + p.toString(),
      signal,
    )
  },
}
