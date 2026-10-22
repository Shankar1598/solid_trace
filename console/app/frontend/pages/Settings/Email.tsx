import { useState } from 'react'
import { router, useForm, usePage } from '@inertiajs/react'
import { CheckCircle2, TriangleAlert } from 'lucide-react'
import SettingsLayout from '@/components/layouts/SettingsLayout'
import { PageHeader } from '@/components/PageHeader'
import { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { SharedProps } from '@/types'
import { toast } from 'sonner'

/** The install's email settings. The password is write-only: only whether one is saved, and readable. */
interface EmailSettings {
  smtp_address: string | null
  smtp_port: number
  smtp_username: string | null
  mail_from: string | null
  password_saved: boolean
  password_readable: boolean
  /** Why Console can't send email, or null when it can. */
  problem: string | null
}

/**
 * Settings → Email, for instance admins: the SMTP server and sender Console
 * sends every Organization's email with, and a test email to check them.
 */
export default function SettingsEmail({ settings }: { settings: EmailSettings }) {
  const { current_org, current_user } = usePage<SharedProps>().props
  const path = `/${current_org!.slug}/settings/email`

  const { data, setData, put, processing, errors, transform } = useForm({
    smtp_address: settings.smtp_address ?? '',
    smtp_port: String(settings.smtp_port),
    smtp_username: settings.smtp_username ?? '',
    // Never filled from the server: blank keeps the saved password.
    smtp_password: '',
    clear_password: false,
    mail_from: settings.mail_from ?? '',
  })

  transform((data) => ({ email_settings: data }))

  const save: React.FormEventHandler = (e) => {
    e.preventDefault()
    put(path, {
      preserveScroll: true,
      onSuccess: () => setData((data) => ({ ...data, smtp_password: '', clear_password: false })),
      onError: () => toast.error("Email settings weren't saved"),
    })
  }

  const [to, setTo] = useState(current_user?.email ?? '')
  const [sending, setSending] = useState(false)
  const sendTest: React.FormEventHandler = (e) => {
    e.preventDefault()
    router.post(`${path}/test`, { to }, {
      preserveScroll: true,
      onStart: () => setSending(true),
      onFinish: () => setSending(false),
    })
  }

  const passwordPlaceholder = !settings.password_saved ? ''
    : settings.password_readable ? 'Saved — leave blank to keep' : 'Enter it again'

  return (
    <SettingsLayout>
      <PageHeader
        title="Email"
        description="The SMTP server and sender Console sends email with. They apply to the whole install: every Organization."
      />
      <div className="space-y-6">
        <p className="flex items-center gap-2 text-sm" data-testid="email-status">
          {settings.problem ? (
            <><TriangleAlert className="h-4 w-4 text-warning" />{settings.problem}</>
          ) : (
            <><CheckCircle2 className="h-4 w-4 text-success" />Email is set up</>
          )}
        </p>

        <Card>
          <form onSubmit={save}>
            <CardHeader>
              <CardTitle>SMTP server</CardTitle>
              <CardDescription>
                Your email provider's, or a service such as Amazon SES or Postmark. Port 465 uses TLS; any other port, STARTTLS when the server offers it.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-6 pb-2">
              <div className="grid gap-6 sm:grid-cols-[1fr_8rem]">
                <Field id="smtp_address" label="Host" error={errors.smtp_address}>
                  <Input
                    id="smtp_address"
                    value={data.smtp_address}
                    onChange={(e) => setData('smtp_address', e.target.value)}
                    placeholder="smtp.example.com"
                  />
                </Field>
                <Field id="smtp_port" label="Port" error={errors.smtp_port}>
                  <Input
                    id="smtp_port"
                    inputMode="numeric"
                    value={data.smtp_port}
                    onChange={(e) => setData('smtp_port', e.target.value)}
                  />
                </Field>
              </div>

              <Field id="smtp_username" label="Username" error={errors.smtp_username}
                hint="Blank if the server takes mail without signing in. Clearing it clears the password too.">
                <Input
                  id="smtp_username"
                  autoComplete="off"
                  value={data.smtp_username}
                  onChange={(e) => setData('smtp_username', e.target.value)}
                />
              </Field>

              <Field id="smtp_password" label="Password" error={errors.smtp_password}>
                <Input
                  id="smtp_password"
                  type="password"
                  autoComplete="new-password"
                  value={data.smtp_password}
                  disabled={data.clear_password}
                  onChange={(e) => setData('smtp_password', e.target.value)}
                  placeholder={passwordPlaceholder}
                />
                {settings.password_saved && !settings.password_readable && (
                  <p className="text-xs text-warning">
                    The saved password can't be read, as Console's secret changed. Enter it again.
                  </p>
                )}
                {settings.password_saved && (
                  data.clear_password ? (
                    <p className="flex items-center gap-2 text-xs text-muted-foreground">
                      The saved password is cleared when you save.
                      <Button type="button" variant="link" size="sm" className="h-auto p-0 text-xs"
                        onClick={() => setData('clear_password', false)}>Keep it</Button>
                    </p>
                  ) : (
                    <Button type="button" variant="link" size="sm" className="h-auto p-0 text-xs"
                      onClick={() => setData((data) => ({ ...data, smtp_password: '', clear_password: true }))}>
                      Clear the saved password
                    </Button>
                  )
                )}
              </Field>

              <Field id="mail_from" label="Send from" error={errors.mail_from}
                hint="An address or domain your provider lets you send from, with or without a name.">
                <Input
                  id="mail_from"
                  value={data.mail_from}
                  onChange={(e) => setData('mail_from', e.target.value)}
                  placeholder="SolidTrace <alerts@example.com>"
                />
              </Field>
            </CardContent>
            <CardFooter className="justify-end">
              <Button type="submit" disabled={processing}>
                {processing ? 'Saving…' : 'Save changes'}
              </Button>
            </CardFooter>
          </form>
        </Card>

        <Card>
          <form onSubmit={sendTest}>
            <CardHeader>
              <CardTitle>Send a test email</CardTitle>
              <CardDescription>
                Sent at once with the saved settings, so you see the SMTP server's error if it fails.
              </CardDescription>
            </CardHeader>
            <CardContent className="pb-2">
              <Field id="test_to" label="To">
                <Input id="test_to" type="email" value={to} onChange={(e) => setTo(e.target.value)} />
              </Field>
            </CardContent>
            <CardFooter className="flex-wrap justify-end gap-3">
              {settings.problem && (
                <p className="text-xs text-muted-foreground">{settings.problem}</p>
              )}
              <Button type="submit" variant="outline" disabled={sending || !!settings.problem}>
                {sending ? 'Sending…' : 'Send test email'}
              </Button>
            </CardFooter>
          </form>
        </Card>
      </div>
    </SettingsLayout>
  )
}

function Field({ id, label, error, hint, children }: {
  id: string
  label: string
  error?: string
  hint?: string
  children: React.ReactNode
}) {
  return (
    <div className="space-y-2">
      <Label htmlFor={id}>{label}</Label>
      {children}
      {error && <p className="text-sm font-medium text-destructive">{error}</p>}
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    </div>
  )
}
