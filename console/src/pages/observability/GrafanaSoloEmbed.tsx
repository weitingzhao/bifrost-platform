import { useState, type ReactNode } from 'react'

const SOLO_EMBED_SANDBOX =
  'allow-scripts allow-same-origin allow-popups allow-forms'

export function GrafanaSoloEmbed({
  url,
  title,
  height,
}: {
  url: string
  title: string
  height: number
}): ReactNode {
  const [failed, setFailed] = useState(false)
  if (failed) {
    return (
      <p className="m-0 rounded-md border border-[var(--border)] px-2 py-3 text-center text-[var(--text-dense-caption)] text-muted-foreground">
        Grafana panel unavailable
      </p>
    )
  }
  return (
    <iframe
      title={title}
      src={url}
      loading="lazy"
      sandbox={SOLO_EMBED_SANDBOX}
      className="w-full rounded-md border border-[var(--border)] bg-[var(--background)]"
      style={{ height }}
      onError={() => setFailed(true)}
    />
  )
}
