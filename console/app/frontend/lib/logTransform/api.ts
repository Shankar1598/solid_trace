// The Log transform page's calls to Console, which passes them to EventStore.
import type { ProgramCheck } from './program'

/** EventStore's check of a draft program: its errors and warnings, with byte spans. */
export async function checkProgram({ orgSlug, projectSlug, program, signal }: {
  orgSlug: string; projectSlug: string; program: string; signal?: AbortSignal
}): Promise<ProgramCheck> {
  const token = document.querySelector<HTMLMetaElement>('meta[name="csrf-token"]')?.content ?? ''
  const response = await fetch(`/${orgSlug}/projects/${projectSlug}/log_transform/check`, {
    method: 'POST',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'X-CSRF-Token': token },
    credentials: 'same-origin',
    body: JSON.stringify({ program }),
    signal,
  })
  const body = await response.json().catch(() => null)
  if (response.ok && body && Array.isArray(body.errors)) return { errors: body.errors, warnings: body.warnings ?? [] }
  throw new Error(typeof body?.error === 'string' ? body.error : `The program could not be checked (HTTP ${response.status}).`)
}
