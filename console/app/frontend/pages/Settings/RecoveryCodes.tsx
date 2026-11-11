import { Link } from '@inertiajs/react'
import { Copy, Download } from 'lucide-react'
import { toast } from 'sonner'
import SettingsLayout from '@/components/layouts/SettingsLayout'
import { PageHeader } from '@/components/PageHeader'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Button, buttonVariants } from '@/components/ui/button'
import { copyToClipboard } from '@/lib/clipboard'

interface RecoveryCodesProps {
  // A new set of Recovery codes, which Console doesn't keep.
  codes: string[]
  // Where Continue goes: Settings → User, or the Organization set-up came from.
  continue_to: string
}

// A new set of Recovery codes, shown once, right after it's made
// (RecoveryCodesController): when the user first gets Two-factor, or makes a
// new set on Settings → User.
export default function RecoveryCodes({ codes, continue_to }: RecoveryCodesProps) {
  const text = [
    `SolidTrace Recovery codes for ${window.location.host}`,
    'Each works once, in place of a code from your authenticator app.',
    '',
    ...codes,
    '',
  ].join('\n')

  const copy = async () => {
    if (await copyToClipboard(text)) {
      toast.success('Recovery codes copied')
    } else {
      toast.error('Could not copy the Recovery codes: download them instead.')
    }
  }

  const download = () => {
    const url = URL.createObjectURL(new Blob([text], { type: 'text/plain' }))
    const link = document.createElement('a')
    link.href = url
    link.download = 'solidtrace-recovery-codes.txt'
    link.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  }

  return (
    <SettingsLayout>
      <PageHeader
        title="Your Recovery codes"
        description="If you can't use your authenticator app, a Recovery code signs you in instead. Each works once."
      />
      <Card className="max-w-xl">
        <CardHeader>
          <CardTitle>Keep these somewhere safe</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-sm text-muted-foreground">
            This is the only time they're shown. Copy them into your password manager or download them now.
          </p>
          <ol aria-label="Recovery codes" className="grid grid-cols-2 gap-x-6 gap-y-1 rounded-md border bg-muted/40 p-4 font-mono text-sm">
            {codes.map((code) => <li key={code}>{code}</li>)}
          </ol>
          <div className="flex gap-2">
            <Button type="button" variant="outline" onClick={copy}><Copy /> Copy</Button>
            <Button type="button" variant="outline" onClick={download}><Download /> Download</Button>
          </div>
        </CardContent>
        <CardFooter className="justify-end">
          <Link href={continue_to} className={buttonVariants()}>Continue</Link>
        </CardFooter>
      </Card>
    </SettingsLayout>
  )
}
