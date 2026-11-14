import AuthLayout from '@/components/layouts/AuthLayout'

// The approval page's and the device page's error page: Console does not
// send the browser back to the client, because it could not check where to
// send it. The device page gives its own description.
export default function OauthError({ message, description }: { message: string; description?: string }) {
  return (
    <AuthLayout title="Sign-in stopped" description={description ?? 'Console did not send you back to the program.'}>
      <p role="alert" className="text-sm">{message}</p>
    </AuthLayout>
  )
}
