/**
 * The Dispatcher's status as Settings → Instance loads it after the page
 * shows (InstanceSettingsController#dispatcher_status): what the Dispatcher
 * answered, or Console's error when it did not answer as it should.
 */
export type DispatcherStatus =
  | { version?: string; token_active: boolean; token_error?: string; error?: undefined }
  | { error: string }

/** The line that the page shows for a status, and whether all is well. */
export function dispatcherStatusLine(status: DispatcherStatus): { ok: boolean; text: string } {
  if ('error' in status && status.error) return { ok: false, text: status.error }

  const { version, token_active: tokenActive, token_error: tokenError } = status as Exclude<DispatcherStatus, { error: string }>
  const answers = `The Dispatcher answers (${version || 'a development build'})`
  if (tokenActive) return { ok: true, text: `${answers}, and SolidTrace accepts its token.` }

  return {
    ok: false,
    text: tokenError
      ? `${answers}, but SolidTrace refuses its token: ${tokenError}`
      : `${answers}, but SolidTrace refuses its token. An Admin makes a new Dispatcher's token in Settings → API tokens & MCP.`,
  }
}
