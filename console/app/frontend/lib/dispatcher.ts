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

/** A Project's Factory settings, as the Dispatcher keeps them. */
export interface FactorySettingsValues {
  repository: string
  folder: string
  base_branch: string
}

/**
 * The Factory section's settings as a Project's page loads them after it
 * shows (FactorySettings#section): the settings (null when the Project has
 * none), or Console's error when the Dispatcher did not answer as it should.
 */
export type FactorySectionData =
  | { settings: FactorySettingsValues | null; error?: undefined }
  | { error: string }

/**
 * The Factory settings that the section shows. After a save, the redirect
 * back to the Project's page has no `factory` until the page loads it again.
 * The section keeps the last settings in that time, so the form stays
 * mounted: it keeps the values that the Admin typed and the error that the
 * Dispatcher gave.
 */
export function factorySectionShown(
  current: FactorySectionData | undefined,
  last: FactorySectionData | undefined,
): FactorySectionData | undefined {
  return current ?? last
}

/** Where the Factory section saves, and its link to the Project's Runs. */
export interface FactorySectionPaths {
  path: string
  runs_path: string
}
