/**
 * Incremental Server-Sent Events parser for a fetch-streamed body (the bus stream keeps the viewer
 * bearer, which an EventSource cannot send). Feed it decoded text chunks in order; it returns the
 * complete messages and comments each chunk finished. Lines end in LF or CRLF (the platform writes LF).
 */
export interface SseMessage {
  id?: string
  event?: string
  data: string
}

export interface SseBatch {
  messages: SseMessage[]
  /** Comment lines (`: heartbeat`, `: error …`) without the leading colon and space. */
  comments: string[]
  /** The last `retry:` the chunk carried, in milliseconds. */
  retry?: number
}

export class SseParser {
  private buffer = ''
  private data: string[] = []
  private id: string | undefined
  private event: string | undefined
  /** The last id the stream named: what to send as Last-Event-ID when reconnecting. */
  lastEventId: string | undefined

  push(chunk: string): SseBatch {
    const out: SseBatch = { messages: [], comments: [] }
    this.buffer += chunk
    let nl = this.buffer.indexOf('\n')
    while (nl >= 0) {
      let line = this.buffer.slice(0, nl)
      this.buffer = this.buffer.slice(nl + 1)
      if (line.endsWith('\r')) line = line.slice(0, -1)
      this.line(line, out)
      nl = this.buffer.indexOf('\n')
    }
    return out
  }

  private line(line: string, out: SseBatch): void {
    if (line === '') {
      if (this.data.length > 0) {
        const message: SseMessage = { data: this.data.join('\n') }
        if (this.id !== undefined) message.id = this.id
        if (this.event !== undefined) message.event = this.event
        out.messages.push(message)
      }
      this.data = []
      this.id = undefined
      this.event = undefined
      return
    }
    if (line.startsWith(':')) {
      out.comments.push(line.slice(1).replace(/^ /, ''))
      return
    }
    const colon = line.indexOf(':')
    const field = colon < 0 ? line : line.slice(0, colon)
    const value = colon < 0 ? '' : line.slice(colon + 1).replace(/^ /, '')
    switch (field) {
      case 'data':
        this.data.push(value)
        break
      case 'id':
        if (!value.includes('\0')) {
          this.id = value
          this.lastEventId = value
        }
        break
      case 'event':
        this.event = value
        break
      case 'retry':
        if (/^\d+$/.test(value)) out.retry = Number(value)
        break
      default:
        break
    }
  }
}
