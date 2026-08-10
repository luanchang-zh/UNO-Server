import type { Envelope } from '../protocol/types'
import { parseSafeJson, stringifyWithIds } from './json'

export type WsStatus = 'idle' | 'connecting' | 'open' | 'closed'

export interface WsHandlers {
  onMessage: (envelope: Envelope) => void
  onStatus: (status: WsStatus) => void
}

const PING_INTERVAL_MS = 25_000
const RECONNECT_BASE_MS = 800
const RECONNECT_MAX_MS = 8_000

/**
 * GameSocket 维护到 /ws 的长连接：
 * 自动心跳、指数退避自动重连；服务端在鉴权成功后会自动换绑座位并补发全量状态。
 */
export class GameSocket {
  private socket: WebSocket | null = null
  private token = ''
  private handlers: WsHandlers
  private pingTimer: ReturnType<typeof setInterval> | null = null
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private reconnectAttempt = 0
  private manuallyClosed = false
  private requestSeq = 0

  constructor(handlers: WsHandlers) {
    this.handlers = handlers
  }

  connect(token: string): void {
    this.token = token
    this.manuallyClosed = false
    this.openSocket()
  }

  close(): void {
    this.manuallyClosed = true
    this.clearTimers()
    this.socket?.close()
    this.socket = null
    this.handlers.onStatus('closed')
  }

  /** 发送一条业务消息；返回本次的 request_id 便于关联错误。 */
  send(type: string, payload?: unknown): string {
    const requestId = `r${Date.now().toString(36)}-${++this.requestSeq}`
    const envelope: Envelope = { type, request_id: requestId }
    if (payload !== undefined) envelope.payload = payload
    if (this.socket?.readyState === WebSocket.OPEN) {
      this.socket.send(stringifyWithIds(envelope))
    }
    return requestId
  }

  private openSocket(): void {
    this.clearTimers()
    this.handlers.onStatus('connecting')
    const protocol = location.protocol === 'https:' ? 'wss' : 'ws'
    const url = `${protocol}://${location.host}/ws?token=${encodeURIComponent(this.token)}`
    const socket = new WebSocket(url)
    this.socket = socket

    socket.onopen = () => {
      if (this.socket !== socket) return
      this.reconnectAttempt = 0
      this.handlers.onStatus('open')
      this.pingTimer = setInterval(() => this.send('ping'), PING_INTERVAL_MS)
    }
    socket.onmessage = (event) => {
      if (this.socket !== socket) return
      try {
        const envelope = parseSafeJson<Envelope>(event.data as string)
        if (envelope.type === 'pong') return
        this.handlers.onMessage(envelope)
      } catch {
        // 忽略无法解析的消息
      }
    }
    socket.onclose = () => {
      if (this.socket !== socket) return
      this.clearTimers()
      this.socket = null
      this.handlers.onStatus('closed')
      if (!this.manuallyClosed) this.scheduleReconnect()
    }
    socket.onerror = () => socket.close()
  }

  private scheduleReconnect(): void {
    const delay = Math.min(
      RECONNECT_BASE_MS * 2 ** this.reconnectAttempt,
      RECONNECT_MAX_MS,
    )
    this.reconnectAttempt += 1
    this.reconnectTimer = setTimeout(() => this.openSocket(), delay)
  }

  private clearTimers(): void {
    if (this.pingTimer) {
      clearInterval(this.pingTimer)
      this.pingTimer = null
    }
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
  }
}
