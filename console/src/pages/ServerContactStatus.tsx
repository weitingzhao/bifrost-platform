import { useEffect, useState } from 'react'
import { useServerContact } from '@/api/serverContact'
import { notHeardNotice } from '@/pages/serverContactNotice'

export function ServerContactStatus() {
  const last = useServerContact()
  const [now, setNow] = useState(Date.now)
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [])
  return last != null && Math.max(now, Date.now()) - last > 60_000
    ? <p role="status" className="m-0 text-sm">{notHeardNotice(last)}</p> : null
}

