import { useEffect, useRef, useState } from 'react'

// The browser's speech recognition (Safari and Chrome: webkitSpeechRecognition), for dictating a message to the
// chief of staff (Owner 2026-10-18: ask by voice). Typed narrowly: only what this uses.
type Result = { isFinal: boolean; 0: { transcript: string } }
type ResultEvent = { resultIndex: number; results: ArrayLike<Result> }
type Recognition = {
  lang: string
  interimResults: boolean
  continuous: boolean
  start(): void
  stop(): void
  abort(): void
  onresult: ((e: ResultEvent) => void) | null
  onerror: ((e: { error: string }) => void) | null
  onend: (() => void) | null
}
type RecognitionClass = new () => Recognition

function recognitionClass(): RecognitionClass | undefined {
  if (typeof window === 'undefined') return undefined
  const w = window as unknown as { SpeechRecognition?: RecognitionClass; webkitSpeechRecognition?: RecognitionClass }
  return w.SpeechRecognition ?? w.webkitSpeechRecognition
}

/** Joins dictated text to what is typed: a space only between two Latin words. */
export function appendDictated(body: string, text: string): string {
  const t = text.trim()
  if (!t) return body
  if (!body) return t
  return /[A-Za-z0-9.,;:!?]$/.test(body) && /^[A-Za-z0-9]/.test(t) ? `${body} ${t}` : body + t
}

/** Dictation into a text box: each final phrase is handed to onText. */
export function useDictation(onText: (text: string) => void) {
  const [listening, setListening] = useState(false)
  const [error, setError] = useState('')
  const rec = useRef<Recognition | null>(null)
  const handler = useRef(onText)
  useEffect(() => { handler.current = onText }, [onText])
  useEffect(() => () => rec.current?.abort(), [])
  const supported = recognitionClass() !== undefined
  function start() {
    const Rec = recognitionClass()
    if (!Rec || rec.current) return
    const r = new Rec()
    r.lang = (typeof navigator !== 'undefined' && navigator.language) || 'zh-CN'
    r.interimResults = false
    r.continuous = true
    r.onresult = e => {
      for (let i = e.resultIndex; i < e.results.length; i++) {
        const res = e.results[i]
        if (res.isFinal) handler.current(res[0].transcript)
      }
    }
    r.onerror = e => setError(e.error === 'not-allowed' || e.error === 'service-not-allowed'
      ? 'Microphone access was not allowed for this page.' : `Dictation stopped (${e.error}).`)
    r.onend = () => { rec.current = null; setListening(false) }
    rec.current = r
    setError('')
    try {
      r.start()
      setListening(true)
    } catch (e) {
      rec.current = null
      setError(e instanceof Error ? e.message : 'Dictation could not start.')
    }
  }
  function stop() { rec.current?.stop() }
  return { supported, listening, error, start, stop }
}
