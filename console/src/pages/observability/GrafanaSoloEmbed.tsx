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
      <p className="m-0 rounded-[var(--card-radius)] border border-[var(--card-border)] px-2 py-3 text-center text-[var(--text-dense-caption)] text-muted-foreground bg-[var(--card-fill)]">
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
      className="w-full rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)]"
      style={{ height }}
      onError={() => setFailed(true)}
    />
  )
}
