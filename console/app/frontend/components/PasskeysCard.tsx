import { useState } from 'react'
import { router } from '@inertiajs/react'
import { KeyRound } from 'lucide-react'
import { toast } from 'sonner'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  createPasskey,
  defaultPasskeyName,
  getPasskey,
  passkeyCancelled,
  passkeyOptions,
  passkeysElsewhere,
  passkeysSupported,
  type PasskeyOfferHere,
} from '@/lib/passkeys'

/** One of the user's Passkeys for the Console URL's host. */
export interface PasskeyRow {
  id: number
  name: string
  created_at: string
  last_used_at: string | null
}

const date = (iso: string) => new Date(iso).toLocaleDateString(undefined, { dateStyle: 'medium' })

function failed(error: unknown) {
  if (passkeyCancelled(error)) return
  if (error instanceof DOMException && error.name === 'InvalidStateError') {
    toast.error('This device already has one of your passkeys.')
  } else {
    toast.error(error instanceof Error ? error.message : "That passkey didn't work.")
  }
}

/**
 * Settings → User → Passkeys (PasskeysController): add one, named with a
 * default from the browser and OS, list them with when each was made and
 * last used, rename, and remove one with an answer from it. Opened at an
 * address other than the Console URL, it says where Passkeys work instead.
 */
export function PasskeysCard({ offer, passkeys }: { offer: PasskeyOfferHere, passkeys: PasskeyRow[] }) {
  const usable = offer.here && passkeysSupported()
  const [name, setName] = useState(() => (typeof navigator === 'undefined' ? 'Passkey' : defaultPasskeyName()))
  const [busy, setBusy] = useState(false)

  const add: React.FormEventHandler = async (e) => {
    e.preventDefault()
    setBusy(true)
    try {
      const options = await passkeyOptions<PublicKeyCredentialCreationOptionsJSON>('/passkeys/creation_options')
      const credential = await createPasskey(options)
      router.post('/passkeys', { name, credential }, { preserveScroll: true })
    } catch (error) {
      failed(error)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card data-testid="passkeys">
      <CardHeader>
        <CardTitle className="flex items-center gap-2"><KeyRound className="h-4 w-4" />Passkeys</CardTitle>
        <CardDescription>
          {offer.here
            ? 'Sign in with your fingerprint, face or PIN instead of your password.'
            : passkeysElsewhere(offer)}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {passkeys.length > 0 && (
          <ul className="divide-y" aria-label="Your passkeys">
            {passkeys.map((passkey) => (
              <PasskeyItem key={passkey.id} passkey={passkey} removable={usable} />
            ))}
          </ul>
        )}
        {usable && (
          <form onSubmit={add} className="flex flex-wrap items-end gap-2" aria-label="Add a passkey">
            <div className="space-y-2">
              <Label htmlFor="passkey-name">Name</Label>
              <Input id="passkey-name" value={name} maxLength={100} onChange={(e) => setName(e.target.value)} required />
            </div>
            <Button type="submit" disabled={busy}>{busy ? 'Adding…' : 'Add a passkey'}</Button>
          </form>
        )}
        {offer.here && !passkeysSupported() && (
          <p className="text-sm text-muted-foreground">This browser can't use passkeys.</p>
        )}
      </CardContent>
    </Card>
  )
}

function PasskeyItem({ passkey, removable }: { passkey: PasskeyRow, removable: boolean }) {
  const [renaming, setRenaming] = useState(false)
  const [name, setName] = useState(passkey.name)
  const [busy, setBusy] = useState(false)

  const rename: React.FormEventHandler = (e) => {
    e.preventDefault()
    router.patch(`/passkeys/${passkey.id}`, { name }, { preserveScroll: true, onSuccess: () => setRenaming(false) })
  }

  // Proves it's the user with an answer from the Passkey being removed.
  const remove = async () => {
    setBusy(true)
    try {
      const options = await passkeyOptions<PublicKeyCredentialRequestOptionsJSON>(`/passkeys/${passkey.id}/removal_options`)
      const credential = await getPasskey(options)
      router.delete(`/passkeys/${passkey.id}`, { data: { credential }, preserveScroll: true })
    } catch (error) {
      failed(error)
    } finally {
      setBusy(false)
    }
  }

  return (
    <li className="flex flex-wrap items-center justify-between gap-2 py-3" aria-label={passkey.name}>
      {renaming ? (
        <form onSubmit={rename} className="flex flex-wrap items-center gap-2" aria-label={`Rename ${passkey.name}`}>
          <Input aria-label="Name" value={name} maxLength={100} onChange={(e) => setName(e.target.value)} required className="w-56" />
          <Button type="submit" size="sm">Save</Button>
          <Button type="button" size="sm" variant="ghost" onClick={() => { setName(passkey.name); setRenaming(false) }}>Cancel</Button>
        </form>
      ) : (
        <div>
          <div className="text-sm font-medium">{passkey.name}</div>
          <div className="text-xs text-muted-foreground">
            Added {date(passkey.created_at)} · {passkey.last_used_at ? `Last used ${date(passkey.last_used_at)}` : 'Never used'}
          </div>
        </div>
      )}
      {!renaming && (
        <div className="flex gap-2">
          <Button type="button" size="sm" variant="outline" onClick={() => setRenaming(true)}>Rename</Button>
          {removable && (
            <Button type="button" size="sm" variant="destructive" onClick={remove} disabled={busy}>
              {busy ? 'Removing…' : 'Remove'}
            </Button>
          )}
        </div>
      )}
    </li>
  )
}
