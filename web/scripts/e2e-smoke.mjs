// 双人端到端冒烟：复用与前端相同的大整数安全解析逻辑。
const BASE = 'http://localhost:8080'

const parseSafe = (text) =>
  JSON.parse(text.replace(/([:[,]\s*)(\d{15,})(?=\s*[,}\]])/g, '$1"$2"'))
const stringifyIds = (v) =>
  JSON.stringify(v).replace(/"player_id":"(\d+)"/g, '"player_id":$1')

async function login(nickname) {
  const res = await fetch(`${BASE}/api/v1/auth/guest`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ nickname }),
  })
  return parseSafe(await res.text())
}

function connect(token, name, sink) {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(`ws://localhost:8080/ws?token=${token}`)
    ws.onopen = () => resolve(ws)
    ws.onerror = reject
    ws.onmessage = (e) => {
      const env = parseSafe(e.data)
      sink(name, env)
    }
  })
}

const send = (ws, type, payload) =>
  ws.send(stringifyIds({ type, payload }))

const wait = (ms) => new Promise((r) => setTimeout(r, ms))

const inbox = {}
const sink = (name, env) => {
  ;(inbox[name] ??= []).push(env)
  if (env.type === 'error') console.log(`[${name}] ERROR`, env.payload)
}
const last = (name, type) =>
  [...(inbox[name] ?? [])].reverse().find((e) => e.type === type)

const a = await login('玩家A')
const b = await login('玩家B')
console.log('login ok, idA len =', String(a.player_id).length, typeof a.player_id)

const wsA = await connect(a.token, 'A', sink)
const wsB = await connect(b.token, 'B', sink)
await wait(300)

send(wsA, 'create_room', { max_players: 2 })
await wait(300)
const roomState = last('A', 'room_state')
const roomId = roomState.payload.room_id
console.log('room created:', roomId)

send(wsB, 'join_room', { room_id: roomId })
await wait(300)
send(wsB, 'ready', { ready: true })
await wait(300)
send(wsA, 'start', {})
await wait(500)

const gsA = last('A', 'game_state')
const gsB = last('B', 'game_state')
console.log('A hand:', gsA.payload.hand.length, 'B hand:', gsB.payload.hand.length)
console.log('phase:', gsA.payload.phase, 'current:', typeof gsA.payload.current_player_id, gsA.payload.current_player_id)
console.log('turn_deadline:', gsA.payload.turn_deadline)
console.log('top card:', gsA.payload.top_card)

// 当前玩家打出一张可出的牌
const current = gsA.payload.current_player_id === a.player_id ? { ws: wsA, gs: gsA, name: 'A' } : { ws: wsB, gs: gsB, name: 'B' }
const playable = current.gs.payload.playable_card_ids ?? []
if (playable.length > 0) {
  send(current.ws, 'play_card', { card_id: playable[0], say_uno: false })
} else {
  send(current.ws, 'draw_card', {})
}
await wait(400)
const after = last(current.name, 'game_state')
console.log('after action phase:', after.payload.phase, 'hand:', after.payload.hand.length)

wsA.close()
wsB.close()
console.log('E2E OK')
process.exit(0)
