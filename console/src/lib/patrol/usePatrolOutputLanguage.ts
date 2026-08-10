import { useCallback, useEffect, useState } from 'react'
import {
  DEFAULT_PATROL_OUTPUT_LANGUAGE,
  readPatrolOutputLanguage,
  writePatrolOutputLanguage,
  type PatrolOutputLanguage,
} from '@/lib/patrol/logLanguage'

export function usePatrolOutputLanguage() {
  const [lang, setLang] = useState<PatrolOutputLanguage>(DEFAULT_PATROL_OUTPUT_LANGUAGE)

  useEffect(() => {
    setLang(readPatrolOutputLanguage())
  }, [])

  const setOutputLanguage = useCallback((next: PatrolOutputLanguage) => {
    setLang(next)
    writePatrolOutputLanguage(next)
  }, [])

  return { lang, setOutputLanguage }
}
