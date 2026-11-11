import { csrfToken } from './apiTokens'

/** Where Passkeys are offered (PasskeyOffer): at the Console URL's host, or not, with why. */
export type PasskeyOffer =
  | { offered: true, host: string, url: string }
  | { offered: false, reason: string }

/** PasskeyOffer, and whether this page, opened where it is, can use them. */
export type PasskeyOfferHere = PasskeyOffer & { here: boolean }

/** What a page opened elsewhere says instead of the Passkey controls (UsesPasskeys#passkeys_elsewhere). */
export function passkeysElsewhere(offer: PasskeyOffer): string {
  return offer.offered
    ? `Passkeys work at ${offer.url}.`
    : `Passkeys aren't offered on this install. ${offer.reason}`
}

/** Whether this browser does WebAuthn at all. */
export function passkeysSupported(): boolean {
  return typeof window !== 'undefined' && 'PublicKeyCredential' in window && !!navigator.credentials
}

/** Whether the browser offers Passkeys in an input's autofill (conditional mediation). */
export async function passkeyAutofillAvailable(): Promise<boolean> {
  if (!passkeysSupported() || typeof PublicKeyCredential.isConditionalMediationAvailable !== 'function') return false
  try {
    return await PublicKeyCredential.isConditionalMediationAvailable()
  } catch {
    return false
  }
}

/** Fetches a ceremony's options from Console (a POST, as it keeps the challenge). Throws Console's error. */
export async function passkeyOptions<T>(path: string): Promise<T> {
  const response = await fetch(path, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { Accept: 'application/json', 'X-CSRF-Token': csrfToken() },
  })
  const body = await response.json().catch(() => ({}))
  if (!response.ok) throw new Error(body.error ?? 'Passkeys are unavailable right now.')
  return body as T
}

/** Makes a Passkey with the browser's authenticator; returns it as JSON for Console. */
export async function createPasskey(options: PublicKeyCredentialCreationOptionsJSON): Promise<PublicKeyCredentialJSON> {
  const publicKey = typeof PublicKeyCredential.parseCreationOptionsFromJSON === 'function'
    ? PublicKeyCredential.parseCreationOptionsFromJSON(options)
    : creationOptionsFromJSON(options)
  const credential = await navigator.credentials.create({ publicKey })
  return credentialToJSON(credential as PublicKeyCredential)
}

/**
 * Asks the browser's authenticator for a Passkey's answer; returns it as JSON
 * for Console. Conditional mediation shows the Passkeys in the autofill of
 * an input whose autocomplete has "webauthn", until signal aborts.
 */
export async function getPasskey(
  options: PublicKeyCredentialRequestOptionsJSON,
  { mediation, signal }: { mediation?: CredentialMediationRequirement, signal?: AbortSignal } = {},
): Promise<PublicKeyCredentialJSON> {
  const publicKey = typeof PublicKeyCredential.parseRequestOptionsFromJSON === 'function'
    ? PublicKeyCredential.parseRequestOptionsFromJSON(options)
    : requestOptionsFromJSON(options)
  const credential = await navigator.credentials.get({ publicKey, mediation, signal })
  return credentialToJSON(credential as PublicKeyCredential)
}

/** Whether an error from the browser means the user closed its dialog or another ceremony replaced it. */
export function passkeyCancelled(error: unknown): boolean {
  return error instanceof DOMException && (error.name === 'NotAllowedError' || error.name === 'AbortError')
}

/** A default name for a new Passkey, from the browser and OS, as "Chrome on macOS". */
export function defaultPasskeyName(userAgent: string = navigator.userAgent): string {
  const browser =
    /Edg\//.test(userAgent) ? 'Edge'
      : /OPR\//.test(userAgent) ? 'Opera'
        : /Firefox\/|FxiOS/.test(userAgent) ? 'Firefox'
          : /Chrome\/|CriOS/.test(userAgent) ? 'Chrome'
            : /Safari\//.test(userAgent) ? 'Safari'
              : 'Browser'
  const os =
    /iPhone|iPad|iPod/.test(userAgent) ? 'iOS'
      : /Android/.test(userAgent) ? 'Android'
        : /Mac OS X|Macintosh/.test(userAgent) ? 'macOS'
          : /Windows/.test(userAgent) ? 'Windows'
            : /CrOS/.test(userAgent) ? 'ChromeOS'
              : /Linux/.test(userAgent) ? 'Linux'
                : null
  return os ? `${browser} on ${os}` : browser
}

// Browsers without the native JSON helpers (parse*FromJSON, toJSON): the
// same conversions, base64url to bytes and back.

function bytes(base64url: string): ArrayBuffer {
  const base64 = base64url.replace(/-/g, '+').replace(/_/g, '/').padEnd(Math.ceil(base64url.length / 4) * 4, '=')
  const binary = atob(base64)
  const buffer = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) buffer[i] = binary.charCodeAt(i)
  return buffer.buffer
}

function base64url(buffer: ArrayBuffer | ArrayBufferView): string {
  const view = buffer instanceof ArrayBuffer ? new Uint8Array(buffer) : new Uint8Array(buffer.buffer, buffer.byteOffset, buffer.byteLength)
  let binary = ''
  for (const byte of view) binary += String.fromCharCode(byte)
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

function descriptors(list: PublicKeyCredentialDescriptorJSON[] | undefined): PublicKeyCredentialDescriptor[] | undefined {
  return list?.map((descriptor) => ({
    type: 'public-key',
    id: bytes(descriptor.id),
    transports: descriptor.transports as AuthenticatorTransport[] | undefined,
  }))
}

function creationOptionsFromJSON(options: PublicKeyCredentialCreationOptionsJSON): PublicKeyCredentialCreationOptions {
  return {
    ...options,
    challenge: bytes(options.challenge),
    user: { ...options.user, id: bytes(options.user.id) },
    excludeCredentials: descriptors(options.excludeCredentials),
  } as PublicKeyCredentialCreationOptions
}

function requestOptionsFromJSON(options: PublicKeyCredentialRequestOptionsJSON): PublicKeyCredentialRequestOptions {
  return {
    ...options,
    challenge: bytes(options.challenge),
    allowCredentials: descriptors(options.allowCredentials),
  } as PublicKeyCredentialRequestOptions
}

function credentialToJSON(credential: PublicKeyCredential): PublicKeyCredentialJSON {
  if (typeof credential.toJSON === 'function') return credential.toJSON()

  const response = credential.response
  const json: Record<string, unknown> = {
    id: credential.id,
    rawId: base64url(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment,
    clientExtensionResults: credential.getClientExtensionResults(),
  }
  if (response instanceof AuthenticatorAttestationResponse) {
    json.response = {
      clientDataJSON: base64url(response.clientDataJSON),
      attestationObject: base64url(response.attestationObject),
      transports: response.getTransports?.() ?? [],
    }
  } else {
    const assertion = response as AuthenticatorAssertionResponse
    json.response = {
      clientDataJSON: base64url(assertion.clientDataJSON),
      authenticatorData: base64url(assertion.authenticatorData),
      signature: base64url(assertion.signature),
      userHandle: assertion.userHandle ? base64url(assertion.userHandle) : null,
    }
  }
  return json
}
