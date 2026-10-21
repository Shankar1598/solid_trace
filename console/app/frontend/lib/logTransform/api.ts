// The Log transform page's calls to Console, which passes them to EventStore.
import type { PreviewAnswer } from './preview'
import type { ProgramCheck } from './program'

/** EventStore's check of a draft program: its errors and warnings, with byte spans. */
export async function checkProgram({ orgSlug, projectSlug, program, signal }: {
  orgSlug: string; projectSlug: string; program: string; signal?: AbortSignal
}): Promise<ProgramCheck> {
  const { response, body } = await post(`/${orgSlug}/projects/${projectSlug}/log_transform/check`, { program }, signal)
  if (response.ok && body && Array.isArray(body.errors)) return { errors: body.errors, warnings: body.warnings ?? [] }
  throw new Error(typeof body?.error === 'string' ? body.error : `The program could not be checked (HTTP ${response.status}).`)
}

/**
 * EventStore's preview of a draft program on `logs`, or on the Project's
 * recent arrivals without them: each Log before and after, dropped or the
 * error, with the counts; or the check's errors when it doesn't compile.
 */
export async function previewProgram({ orgSlug, projectSlug, program, logs }: {
  orgSlug: string; projectSlug: string; program: string; logs?: object[]
}): Promise<PreviewAnswer> {
  const { response, body } = await post(`/${orgSlug}/projects/${projectSlug}/log_transform/preview`, { program, logs })
  if (response.ok && body && Array.isArray(body.results)) return { preview: body }
  if (response.ok && body && Array.isArray(body.errors)) return { check: { errors: body.errors, warnings: body.warnings ?? [] } }
  throw new Error(typeof body?.error === 'string' ? body.error : `The draft could not be previewed (HTTP ${response.status}).`)
}

async function post(path: string, data: object, signal?: AbortSignal) {
  const token = document.querySelector<HTMLMetaElement>('meta[name="csrf-token"]')?.content ?? ''
  const response = await fetch(path, {
    method: 'POST',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'X-CSRF-Token': token },
    credentials: 'same-origin',
    body: JSON.stringify(data),
    signal,
  })
  const body = await response.json().catch(() => null)
  return { response, body }
}
