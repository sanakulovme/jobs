// Human-readable names for machine values the CRM shows: specialty slugs
// (model.SpecialtyVocabulary), job sources, and dates. The stored values
// stay the slugs; only what the admin reads changes.

const SPECIALTY_LABEL: Record<string, string> = {
  ausbildung: 'Ausbildung (o‘qish joyi)',
  daf_daz: 'Nemis tili (DaF/DaZ)',
  dialyse: 'Dializ',
  kardiologie: 'Kardiologiya',
  mfa: 'MFA',
  mrt: 'MRT',
  nephrologie: 'Nefrologiya',
  ophthalmologie: 'Oftalmologiya',
  orthopadie: 'Ortopediya',
  pflege: 'Hamshiralik (Pflege)',
  rontgen: 'Rentgen',
  zfa: 'ZFA (stomatologiya)',
}

export function specialtyLabel(slug: string): string {
  return SPECIALTY_LABEL[slug] ?? slug
}

export function sourceLabel(source: string): string {
  switch (source) {
    case 'bundesagentur':
      return 'arbeitsagentur.de'
    case 'site':
      return 'boshqa sayt'
    case 'manual':
      return "qo'lda kiritilgan"
  }
  return source
}

function pad(n: number): string {
  return n < 10 ? '0' + n : String(n)
}

// dateLabel renders an ISO time as DD.MM.YYYY (time too when withTime),
// "—" when missing or zero.
export function dateLabel(iso: string | undefined, withTime = false): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (isNaN(d.getTime()) || d.getUTCFullYear() < 2000) return '—'
  const date = `${pad(d.getDate())}.${pad(d.getMonth() + 1)}.${d.getFullYear()}`
  return withTime ? `${date} ${pad(d.getHours())}:${pad(d.getMinutes())}` : date
}
