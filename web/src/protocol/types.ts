// 与后端 internal/protocol、internal/game/uno 的 JSON tag 逐字段对齐。

// ---------- 基础 ----------

/** 所有 WebSocket 消息的统一外壳。 */
export interface Envelope {
  type: string
  request_id?: string
  payload?: unknown
}

export interface HelloPayload {
  player_id: string
  nickname: string
}

export interface ErrorPayload {
  code: string
  message: string
}

// ---------- 登录 ----------

export interface GuestLoginResponse {
  player_id: string
  nickname: string
  token: string
  expires_at: string
}

// ---------- 房间 ----------

export interface RoomMemberView {
  player_id: string
  nickname: string
  ready: boolean
  is_owner: boolean
  connected: boolean
  auto_play: boolean
  timeout_strikes: number
}

export type RoomPhase = 'waiting' | 'playing' | 'settled'

export interface RoomStatePayload {
  room_id: string
  phase: RoomPhase
  max_players: number
  owner_id: string
  members: RoomMemberView[]
}

// ---------- 牌局 ----------

export type Color = '' | 'red' | 'yellow' | 'green' | 'blue'
export type CardKind =
  | 'number'
  | 'skip'
  | 'reverse'
  | 'draw_two'
  | 'wild'
  | 'wild_draw_four'

export interface Card {
  id: number
  color?: Color
  kind: CardKind
  number?: number
}

export type GamePhase =
  | 'playing'
  | 'awaiting_color'
  | 'awaiting_draw_decision'
  | 'finished'

export interface PlayerView {
  player_id: string
  seat: number
  cards: number
}

export interface UNOChallenge {
  player_id: string
  expires_at: string
}

export interface PlayerResult {
  player_id: string
  is_winner: boolean
  score: number
  hand_points: number
  cards_left: number
}

export interface RoundResult {
  winner_id: string
  score: number
  players: PlayerResult[]
}

/** game_state 消息体：引擎安全视图 + 房间层回合截止时间。 */
export interface GameStatePayload {
  phase: GamePhase
  players: PlayerView[]
  hand: Card[]
  playable_card_ids?: number[]
  top_card: Card
  current_color?: Color
  direction: 1 | -1
  current_player_id?: string
  color_chooser_id?: string
  pending_draw: number
  draw_pile_count: number
  discard_pile_count: number
  drawn_card_id?: number
  uno_challenges?: UNOChallenge[]
  winner_candidate_ids?: string[]
  result?: RoundResult
  turn_deadline?: string
}

// ---------- 客户端 → 服务端 消息类型常量 ----------

export const MSG = {
  ping: 'ping',
  createRoom: 'create_room',
  joinRoom: 'join_room',
  leaveRoom: 'leave_room',
  ready: 'ready',
  start: 'start',
  kick: 'kick',
  playCard: 'play_card',
  drawCard: 'draw_card',
  pass: 'pass',
  chooseColor: 'choose_color',
  callUno: 'call_uno',
  catchUno: 'catch_uno',
} as const
