

/** Deep-link options when navigating from Control Room (or elsewhere) to Runtime Map. */
export type RuntimeMapNavigateOptions = {
  env?: string
  targetId?: string
}

export type OpenRuntimeMapFn = (options?: RuntimeMapNavigateOptions) => void
