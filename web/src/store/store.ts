import { create } from 'zustand'
import { GameSocket, type WsStatus } from '../net/ws'
import { parseSafeJson } from '../net/json'
import {
  MSG,
  type Envelope,
  type ErrorPayload,
  type GameStatePayload,
  type GuestLoginResponse,
  type HelloPayload,
  type RoomStatePayload,
} from '../protocol/types'

const TOKEN_KEY = 'uno.token'
const NICKNAME_KEY = 'uno.nickname'

export interface Toast {
  id: number
  kind: 'error' | 'info' | 'success'
  text: string
}

interface AppState {
  // 身份与连接
  token: string
  playerId: string
  nickname: string
  wsStatus: WsStatus
  loggingIn: boolean
  // 房间与牌局（服务端推送的权威快照）
  room: RoomStatePayload | null
  game: GameStatePayload | null
  /** 结算后玩家点击"返回房间"隐藏结果面板。 */
  resultDismissed: boolean
  /** 下一次出牌是否同时喊 UNO。 */
  sayUnoArmed: boolean
  toasts: Toast[]

  login: (nickname: string) => Promise<void>
  logout: () => void
  connect: () => void
  createRoom: (maxPlayers: number) => void
  joinRoom: (roomId: string) => void
  leaveRoom: () => void
  setReady: (ready: boolean) => void
  startGame: () => void
  kick: (playerId: string) => void
  playCard: (cardId: number) => void
  drawCard: () => void
  pass: () => void
  chooseColor: (color: string) => void
  callUno: () => void
  catchUno: (playerId: string) => void
  toggleSayUno: () => void
  dismissResult: () => void
  pushToast: (kind: Toast['kind'], text: string) => void
  removeToast: (id: number) => void
}

let socket: GameSocket | null = null
let toastSeq = 0

export const useStore = create<AppState>((set, get) => {
  const pushToast = (kind: Toast['kind'], text: string) => {
    const id = ++toastSeq
    set((state) => ({ toasts: [...state.toasts.slice(-3), { id, kind, text }] }))
    setTimeout(() => get().removeToast(id), 3600)
  }

  const handleEnvelope = (envelope: Envelope) => {
    switch (envelope.type) {
      case 'hello': {
        const payload = envelope.payload as HelloPayload
        set({ playerId: payload.player_id, nickname: payload.nickname })
        break
      }
      case 'room_state': {
        const payload = envelope.payload as RoomStatePayload
        set((state) => ({
          room: payload,
          // 回到等待阶段说明新一局尚未开始，清空旧牌局视图。
          game: payload.phase === 'waiting' ? null : state.game,
          resultDismissed:
            payload.phase === 'waiting' ? false : state.resultDismissed,
        }))
        break
      }
      case 'game_state': {
        const payload = envelope.payload as GameStatePayload
        set((state) => ({
          game: payload,
          sayUnoArmed: state.sayUnoArmed && payload.hand.length === 2,
        }))
        break
      }
      case 'error': {
        const payload = envelope.payload as ErrorPayload
        pushToast('error', payload.message || payload.code)
        break
      }
      default:
        break
    }
  }

  const ensureSocket = (): GameSocket => {
    if (!socket) {
      socket = new GameSocket({
        onMessage: handleEnvelope,
        onStatus: (status: WsStatus) => set({ wsStatus: status }),
      })
    }
    return socket
  }

  return {
    token: localStorage.getItem(TOKEN_KEY) ?? '',
    playerId: '',
    nickname: localStorage.getItem(NICKNAME_KEY) ?? '',
    wsStatus: 'idle',
    loggingIn: false,
    room: null,
    game: null,
    resultDismissed: false,
    sayUnoArmed: false,
    toasts: [],

    async login(nickname: string) {
      set({ loggingIn: true })
      try {
        const response = await fetch('/api/v1/auth/guest', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ nickname }),
        })
        const body = parseSafeJson<GuestLoginResponse & { message?: string }>(
          await response.text(),
        )
        if (!response.ok) {
          throw new Error(body.message || '登录失败')
        }
        localStorage.setItem(TOKEN_KEY, body.token)
        localStorage.setItem(NICKNAME_KEY, body.nickname)
        set({
          token: body.token,
          playerId: body.player_id,
          nickname: body.nickname,
        })
        get().connect()
      } catch (error) {
        pushToast('error', error instanceof Error ? error.message : '登录失败')
      } finally {
        set({ loggingIn: false })
      }
    },

    logout() {
      localStorage.removeItem(TOKEN_KEY)
      socket?.close()
      socket = null
      set({
        token: '',
        playerId: '',
        wsStatus: 'idle',
        room: null,
        game: null,
        resultDismissed: false,
        sayUnoArmed: false,
      })
    },

    connect() {
      const token = get().token
      if (!token) return
      ensureSocket().connect(token)
    },

    createRoom(maxPlayers: number) {
      socket?.send(MSG.createRoom, { max_players: maxPlayers })
    },
    joinRoom(roomId: string) {
      socket?.send(MSG.joinRoom, { room_id: roomId.trim() })
    },
    leaveRoom() {
      socket?.send(MSG.leaveRoom)
      set({ room: null, game: null, resultDismissed: false, sayUnoArmed: false })
    },
    setReady(ready: boolean) {
      socket?.send(MSG.ready, { ready })
    },
    startGame() {
      socket?.send(MSG.start)
    },
    kick(playerId: string) {
      socket?.send(MSG.kick, { player_id: playerId })
    },

    playCard(cardId: number) {
      const { game, sayUnoArmed } = get()
      // 打出后剩一张时自动带上 UNO 宣告，避免玩家因忘记点按钮而被抓罚。
      const willHaveOne = (game?.hand.length ?? 0) === 2
      socket?.send(MSG.playCard, {
        card_id: cardId,
        say_uno: sayUnoArmed || willHaveOne,
      })
      set({ sayUnoArmed: false })
    },
    drawCard() {
      socket?.send(MSG.drawCard)
    },
    pass() {
      socket?.send(MSG.pass)
    },
    chooseColor(color: string) {
      socket?.send(MSG.chooseColor, { color })
    },
    callUno() {
      socket?.send(MSG.callUno)
    },
    catchUno(playerId: string) {
      socket?.send(MSG.catchUno, { player_id: playerId })
    },
    toggleSayUno() {
      set((state) => ({ sayUnoArmed: !state.sayUnoArmed }))
    },
    dismissResult() {
      set({ resultDismissed: true })
    },
    pushToast,
    removeToast(id: number) {
      set((state) => ({ toasts: state.toasts.filter((t) => t.id !== id) }))
    },
  }
})
