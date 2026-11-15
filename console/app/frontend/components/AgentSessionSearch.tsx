import { Search, X } from 'lucide-react'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

/**
 * A search box of the Agent sessions pages: the text goes to onSearch when
 * the member presses Enter or Search. With onClear, a search that ran can
 * be cleared.
 */
export function AgentSessionSearch({ label, placeholder, onSearch, onClear, active = false }: {
  label: string
  placeholder: string
  onSearch: (text: string) => void
  onClear?: () => void
  active?: boolean
}) {
  const [text, setText] = useState('')
  return (
    <form
      role="search"
      aria-label={label}
      className="flex items-center gap-2"
      onSubmit={e => {
        e.preventDefault()
        if (text.trim() !== '') onSearch(text.trim())
      }}
    >
      <Input className="min-w-0 flex-1 text-sm" placeholder={placeholder} aria-label={label} value={text} onChange={e => setText(e.target.value)} />
      <Button type="submit" variant="outline" size="sm" disabled={text.trim() === ''}>
        <Search className="h-3.5 w-3.5" aria-hidden />
        Search
      </Button>
      {onClear && active && (
        <Button type="button" variant="ghost" size="sm" onClick={() => { setText(''); onClear() }}>
          <X className="h-3.5 w-3.5" aria-hidden />
          Clear
        </Button>
      )}
    </form>
  )
}
