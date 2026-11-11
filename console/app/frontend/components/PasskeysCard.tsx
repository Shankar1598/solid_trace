import { useState } from 'react'
import { router, useForm } from '@inertiajs/react'
import { KeyRound } from 'lucide-react'
import { toast } from 'sonner'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  FactorProofFields,
  SecondStepFields,
  emptyFactorProof,
  emptySecondStepAnswer,
  passkeyAnswer,
  sendWithPasskeyProof,
  type FactorProof,
  type SecondStepProof,
} from '@/components/SecondStepFields'
import {
  createPasskey,
  defaultPasskeyName,
  passkeyCancelled,
  passkeyOptions,
  passkeysElsewhere,
  passkeysSupported,
  type PasskeyOfferHere,
} from '@/lib/passkeys'

/**
 * One of the user's Passkeys, made for host. other_host: made for a host
 * other than the Console URL's, so it can't be used, only removed, and
 * still counts as Two-factor.
 */
export interface PasskeyRow {
  id: number
  name: string
  host: string
  other_host: boolean
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
 * last used, rename, and remove one for a code, a Passkey or a Recovery
 * code. Opened at an address other than the Console URL, it says where
 * Passkeys work instead of adding them.
 */
export function PasskeysCard({ offer, passkeys, proof, twoFactor }: {
  offer: PasskeyOfferHere
  passkeys: PasskeyRow[]
  proof: SecondStepProof
  // Whether the user has Two-factor, which decides how adding proves it's them.
  twoFactor: boolean
}) {
  const usable = offer.here && passkeysSupported()
  const [name, setName] = useState(() => (typeof navigator === 'undefined' ? 'Passkey' : defaultPasskeyName()))
  const [answer, setAnswer] = useState(emptyFactorProof)
  const [busy, setBusy] = useState(false)

  // Adding needs proof it's the user in the same request: the typed answer,
  // or an answer from one of their Passkeys, asked for first.
  const add = async (withPasskey: boolean) => {
    setBusy(true)
    try {
      let given: FactorProof | { credential: PublicKeyCredentialJSON } = answer
      if (withPasskey) {
        const credential = await passkeyAnswer('/passkeys/proof_options')
        if (!credential) return
        given = { credential }
      }
      const options = await passkeyOptions<PublicKeyCredentialCreationOptionsJSON>('/passkeys/creation_options')
      const credential = await createPasskey(options)
      router.post('/passkeys', { name, credential, proof: given }, {
        preserveScroll: true,
        onFinish: () => setAnswer(emptyFactorProof),
      })
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
              <PasskeyItem key={passkey.id} passkey={passkey} proof={proof} />
            ))}
          </ul>
        )}
        {usable && (
          <form
            onSubmit={(e) => { e.preventDefault(); add(false) }}
            className="space-y-3"
            aria-label="Add a passkey"
          >
            <div className="max-w-xs space-y-2">
              <Label htmlFor="passkey-name">Name</Label>
              <Input id="passkey-name" value={name} maxLength={100} onChange={(e) => setName(e.target.value)} required />
            </div>
            <div className="max-w-xs">
              <FactorProofFields
                id="add-passkey"
                twoFactor={twoFactor}
                proof={proof}
                answer={answer}
                onChange={setAnswer}
                onPasskey={() => add(true)}
              />
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

function PasskeyItem({ passkey, proof }: { passkey: PasskeyRow, proof: SecondStepProof }) {
  const [renaming, setRenaming] = useState(false)
  const [removing, setRemoving] = useState(false)
  const [name, setName] = useState(passkey.name)
  const removal = useForm(emptySecondStepAnswer)

  const rename: React.FormEventHandler = (e) => {
    e.preventDefault()
    router.patch(`/passkeys/${passkey.id}`, { name }, { preserveScroll: true, onSuccess: () => setRenaming(false) })
  }

  // Proves it's the user with a code, a Recovery code or a Passkey.
  const remove: React.FormEventHandler = (e) => {
    e.preventDefault()
    removal.delete(`/passkeys/${passkey.id}`, { preserveScroll: true, onFinish: () => removal.setData(emptySecondStepAnswer) })
  }

  return (
    <li className="space-y-3 py-3" aria-label={passkey.name}>
      <div className="flex flex-wrap items-center justify-between gap-2">
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
              {passkey.other_host
                ? `Made for ${passkey.host}; can't be used here`
                : `Added ${date(passkey.created_at)} · ${passkey.last_used_at ? `Last used ${date(passkey.last_used_at)}` : 'Never used'}`}
            </div>
          </div>
        )}
        {!renaming && !removing && (
          <div className="flex gap-2">
            {!passkey.other_host && (
              <Button type="button" size="sm" variant="outline" onClick={() => setRenaming(true)}>Rename</Button>
            )}
            <Button type="button" size="sm" variant="destructive" onClick={() => setRemoving(true)}>Remove</Button>
          </div>
        )}
      </div>
      {removing && (
        <form onSubmit={remove} className="space-y-3" aria-label={`Remove ${passkey.name}`}>
          <div className="max-w-xs">
            <SecondStepFields
              id={`remove-passkey-${passkey.id}`}
              answer={removal.data}
              onChange={removal.setData}
              authenticatorApp={proof.authenticatorApp}
              onPasskey={proof.passkey ? () => sendWithPasskeyProof('delete', `/passkeys/${passkey.id}`) : undefined}
            />
          </div>
          <div className="flex gap-2">
            <Button type="submit" size="sm" variant="destructive" disabled={removal.processing}>
              {removal.processing ? 'Removing…' : 'Remove passkey'}
            </Button>
            <Button type="button" size="sm" variant="ghost" onClick={() => { removal.setData(emptySecondStepAnswer); setRemoving(false) }}>
              Cancel
            </Button>
          </div>
        </form>
      )}
    </li>
  )
}
