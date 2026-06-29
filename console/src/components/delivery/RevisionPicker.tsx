import {
  cn,
  Input,
  Popover,
  PopoverAnchor,
  PopoverContent,
} from '@bifrost/ui'
import { ChevronDown } from 'lucide-react'
import { useMemo, useState } from 'react'
import type { RevisionsResponse } from '@/api/types'
import { validateGitRevision } from '@/lib/delivery/revisionValidation'

function buildRevisionOptions(data: RevisionsResponse | undefined): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  const add = (name: string) => {
    const n = name.trim()
    if (!n || seen.has(n)) return
    seen.add(n)
    out.push(n)
  }

  add(data?.default_ref ?? 'main')
  for (const ref of data?.common_refs ?? []) add(ref)
  for (const tag of data?.tags ?? []) add(tag.name)
  for (const branch of data?.branches ?? []) add(branch.name)
  return out
}

export interface RevisionPickerProps {
  value: string
  onChange: (value: string) => void
  revisions?: RevisionsResponse
  isLoading?: boolean
  /** Repo names shown in cross-repo warning (e.g. bifrost-platform, bifrost-ui). */
  repoLabels?: string[]
  className?: string
}

export function RevisionPicker({
  value,
  onChange,
  revisions,
  isLoading,
  repoLabels,
  className,
}: RevisionPickerProps) {
  const [open, setOpen] = useState(false)
  const options = useMemo(() => buildRevisionOptions(revisions), [revisions])
  const validationError = validateGitRevision(value)
  const trimmed = value.trim()
  const commonRefs = new Set(revisions?.common_refs ?? [])
  const showCrossRepoWarning =
    trimmed.length > 0
    && validationError == null
    && (repoLabels?.length ?? 0) > 1
    && !commonRefs.has(trimmed)
    && trimmed !== (revisions?.default_ref ?? 'main')

  const pickOption = (opt: string) => {
    onChange(opt)
    setOpen(false)
  }

  return (
    <div className={cn('flex flex-col gap-1', className)}>
      <div className="flex items-center gap-1.5">
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverAnchor asChild>
            <div className="relative w-96">
              <Input
                className="h-8 w-full pr-7 text-dense-body font-mono"
                value={value}
                onChange={e => onChange(e.target.value)}
                onFocus={() => setOpen(true)}
                placeholder="main, branch, tag, or sha"
                spellCheck={false}
                autoComplete="off"
                aria-expanded={open}
                aria-haspopup="listbox"
                role="combobox"
              />
              <button
                type="button"
                className="absolute inset-y-0 right-0 flex w-7 items-center justify-center text-muted-foreground hover:text-foreground"
                aria-label="Show revision options"
                onClick={() => setOpen(prev => !prev)}
              >
                <ChevronDown className={cn('h-3.5 w-3.5 transition-transform', open && 'rotate-180')} />
              </button>
            </div>
          </PopoverAnchor>
          <PopoverContent
            align="start"
            side="bottom"
            sideOffset={4}
            className="w-96 p-1"
            onOpenAutoFocus={e => e.preventDefault()}
          >
            {options.length === 0 ? (
              <p className="px-2 py-1.5 text-dense-caption text-muted-foreground">No refs available</p>
            ) : (
              <ul className="max-h-48 overflow-y-auto" role="listbox">
                {options.map(opt => {
                  const isCommon = commonRefs.has(opt)
                  const isSelected = opt === trimmed
                  return (
                    <li key={opt} role="option" aria-selected={isSelected}>
                      <button
                        type="button"
                        className={cn(
                          'flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-dense-body font-mono',
                          'hover:bg-accent hover:text-accent-foreground',
                          isSelected && 'bg-accent/60',
                        )}
                        onClick={() => pickOption(opt)}
                      >
                        <span className="min-w-0 flex-1 truncate whitespace-nowrap">{opt}</span>
                        {isCommon && (repoLabels?.length ?? 0) > 1 && (
                          <span className="shrink-0 text-dense-micro text-muted-foreground">all repos</span>
                        )}
                      </button>
                    </li>
                  )
                })}
              </ul>
            )}
          </PopoverContent>
        </Popover>
        {isLoading && (
          <span className="text-dense-caption text-muted-foreground shrink-0">loading refs…</span>
        )}
      </div>
      {validationError != null && (
        <span className="text-dense-caption text-destructive">{validationError}</span>
      )}
      {showCrossRepoWarning && (
        <span className="text-dense-caption text-warning">
          ⚠ Not in all repos ({repoLabels?.join(', ')}). Deploy may fail at clone-ui.
        </span>
      )}
      {!isLoading && options.length > 1 && validationError == null && !showCrossRepoWarning && (
        <span className="text-dense-micro text-muted-foreground/60">
          Pick from list or type any ref · {revisions?.common_refs?.length ?? 0} ref(s) in all repos
        </span>
      )}
    </div>
  )
}

export function isRevisionDeployReady(rev: string): boolean {
  return validateGitRevision(rev) == null
}
