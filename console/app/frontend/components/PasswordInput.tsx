import { useState, type ComponentProps } from 'react'
import { Eye, EyeOff } from 'lucide-react'
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput } from '@/components/ui/input-group'

/**
 * A password field with an eye button that shows or hides what is typed.
 * It starts hidden, and the button doesn't submit the form.
 */
export function PasswordInput({ className, ...props }: Omit<ComponentProps<'input'>, 'type'>) {
  const [shown, setShown] = useState(false)
  const label = shown ? 'Hide password' : 'Show password'
  return (
    <InputGroup className={className}>
      <InputGroupInput {...props} type={shown ? 'text' : 'password'} />
      <InputGroupAddon align="inline-end">
        <InputGroupButton
          type="button"
          size="icon-xs"
          aria-label={label}
          aria-pressed={shown}
          title={label}
          onClick={() => setShown(s => !s)}
        >
          {shown ? <EyeOff /> : <Eye />}
        </InputGroupButton>
      </InputGroupAddon>
    </InputGroup>
  )
}
