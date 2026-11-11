import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

// What the second step accepts (User#pass_second_step): a code from the
// Authenticator app. Signing in and removing a factor both ask through these
// fields, so Recovery codes and Passkeys join both here.
export type SecondStepAnswer = { code: string }

export const emptySecondStepAnswer: SecondStepAnswer = { code: '' }

export function SecondStepFields({ answer, onChange, id = 'code', autoFocus }: {
  answer: SecondStepAnswer
  onChange: (answer: SecondStepAnswer) => void
  id?: string
  autoFocus?: boolean
}) {
  return (
    <div className="space-y-2">
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
    </div>
  )
}
