import { useState } from 'react'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

// What the second step accepts (User#pass_second_step): a code from the
// Authenticator app, or one of the user's Recovery codes. Signing in,
// removing a factor and making new Recovery codes all ask through these
// fields, so Passkeys join all three here.
export type SecondStepAnswer = { code: string; recovery_code: string }

export const emptySecondStepAnswer: SecondStepAnswer = { code: '', recovery_code: '' }

export function SecondStepFields({ answer, onChange, id = 'code', autoFocus }: {
  answer: SecondStepAnswer
  onChange: (answer: SecondStepAnswer) => void
  id?: string
  autoFocus?: boolean
}) {
  // One field at a time; switching clears both, so only the one shown is sent.
  const [recovery, setRecovery] = useState(false)
  const switchTo = (useRecovery: boolean) => {
    setRecovery(useRecovery)
    onChange(emptySecondStepAnswer)
  }

  return (
    <div className="space-y-2">
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
            autoFocus
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
      <button
        type="button"
        className="text-sm text-muted-foreground underline-offset-2 hover:underline"
        onClick={() => switchTo(!recovery)}
      >
        {recovery ? 'Use your authenticator app instead' : 'Use a Recovery code instead'}
      </button>
    </div>
  )
}
