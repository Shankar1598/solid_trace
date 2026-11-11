import { useState } from 'react'
import { router } from '@inertiajs/react'
import { KeyRound } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { getPasskey, passkeyCancelled, passkeyOptions } from '@/lib/passkeys'

// What the second step accepts (User#pass_second_step): a code from the
// Authenticator app, one of the user's Recovery codes, or an answer from one
// of their Passkeys. Signing in, removing a factor and making new Recovery
// codes all ask through these fields.
export type SecondStepAnswer = { code: string; recovery_code: string }

export const emptySecondStepAnswer: SecondStepAnswer = { code: '', recovery_code: '' }

/**
 * Asks the browser for an answer from one of the user's Passkeys to the
 * options Console gives at optionsPath. Null when the user closed the
 * browser's dialog; other failures are shown.
 */
export async function passkeyAnswer(optionsPath: string): Promise<PublicKeyCredentialJSON | null> {
  try {
    return await getPasskey(await passkeyOptions<PublicKeyCredentialRequestOptionsJSON>(optionsPath))
  } catch (error) {
    if (!passkeyCancelled(error)) toast.error(error instanceof Error ? error.message : "That passkey didn't work.")
    return null
  }
}

/** Which answers the signed-in user can give to prove it's them, on Settings → User. */
export type SecondStepProof = { authenticatorApp: boolean; passkey: boolean }

/** Sends a request to path with an answer from one of the signed-in user's Passkeys, proving it's them. */
export async function sendWithPasskeyProof(method: 'post' | 'delete', path: string) {
  const credential = await passkeyAnswer('/passkeys/proof_options')
  if (credential) router.visit(path, { method, data: { credential }, preserveScroll: true })
}

export function SecondStepFields({ answer, onChange, id = 'code', autoFocus, authenticatorApp = true, onPasskey }: {
  answer: SecondStepAnswer
  onChange: (answer: SecondStepAnswer) => void
  id?: string
  autoFocus?: boolean
  // Whether the user has an Authenticator app; without one, a Recovery code.
  authenticatorApp?: boolean
  // "Use a passkey", when one of the user's Passkeys can answer here.
  onPasskey?: () => void
}) {
  // One field at a time; switching clears both, so only the one shown is sent.
  const [recovery, setRecovery] = useState(!authenticatorApp)
  const switchTo = (useRecovery: boolean) => {
    setRecovery(useRecovery)
    onChange(emptySecondStepAnswer)
  }

  return (
    <div className="space-y-2">
      {onPasskey && (
        <div className="pb-2">
          <Button type="button" variant="outline" className="w-full" onClick={onPasskey}>
            <KeyRound className="mr-2 h-4 w-4" />Use a passkey
          </Button>
        </div>
      )}
      {recovery ? (
        <>
          <Label htmlFor={`${id}-recovery`}>Recovery code</Label>
          <Input
            id={`${id}-recovery`}
            autoComplete="off"
            autoCapitalize="none"
            spellCheck={false}
            placeholder="xxxx-xxxx-xxxx-xxxx"
            className="font-mono"
            value={answer.recovery_code}
            onChange={(e) => onChange({ ...answer, recovery_code: e.target.value })}
            autoFocus={authenticatorApp || autoFocus}
            required
          />
        </>
      ) : (
        <>
          <Label htmlFor={id}>Code from your authenticator app</Label>
          <Input
            id={id}
            inputMode="numeric"
            autoComplete="one-time-code"
            placeholder="123456"
            maxLength={7}
            value={answer.code}
            onChange={(e) => onChange({ ...answer, code: e.target.value })}
            autoFocus={autoFocus}
            required
          />
        </>
      )}
      {authenticatorApp && (
        <button
          type="button"
          className="text-sm text-muted-foreground underline-offset-2 hover:underline"
          onClick={() => switchTo(!recovery)}
        >
          {recovery ? 'Use your authenticator app instead' : 'Use a Recovery code instead'}
        </button>
      )}
    </div>
  )
}
