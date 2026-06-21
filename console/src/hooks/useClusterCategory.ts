import { useCallback, useEffect, useState } from 'react'
import type { ClusterCategory } from '@/lib/cluster/clusterCategories'
import { parseCategoryFromUrl, writeCategoryToUrl } from '@/lib/cluster/clusterCategories'

export function useClusterCategory() {
  const [category, setCategoryState] = useState<ClusterCategory | null>(() => parseCategoryFromUrl())

  useEffect(() => {
    const onPopState = () => setCategoryState(parseCategoryFromUrl())
    window.addEventListener('popstate', onPopState)
    return () => window.removeEventListener('popstate', onPopState)
  }, [])

  const setCategory = useCallback((next: ClusterCategory | null) => {
    writeCategoryToUrl(next)
    setCategoryState(next)
  }, [])

  const toggleCategory = useCallback(
    (next: ClusterCategory) => {
      setCategory(category === next ? null : next)
    },
    [category, setCategory],
  )

  return { category, setCategory, toggleCategory }
}
