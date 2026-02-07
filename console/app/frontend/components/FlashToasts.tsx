import { useEffect } from 'react'
import { usePage } from '@inertiajs/react'
import { toast } from 'sonner'
import { SharedProps } from '@/types'

export default function FlashToasts() {
  const { flash } = usePage<SharedProps>().props
  const notice = flash?.notice
  const alert = flash?.alert

  useEffect(() => {
    if (notice) {
      toast.success(notice)
    }
  }, [notice])

  useEffect(() => {
    if (alert) {
      toast.error(alert)
    }
  }, [alert])

  return null
}
