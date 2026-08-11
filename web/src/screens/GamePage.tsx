import { useMemo } from 'react'
import { useStore } from '../store/store'
import { UnoCard, cardLabel, COLOR_HEX, COLOR_NAME } from '../components/UnoCard'
import { useCountdown } from '../hooks/useCountdown'
import { avatarStyle } from './RoomPage'
import type { GameStatePayload, RoomStatePayload } from '../protocol/types'

/** 牌桌主界面：对手席位、牌堆、手牌与全部对局交互。 */
export function GamePage() {
  const room = useStore((s) => s.room)!
  const game = useStore((s) => s.game)

  if (!game) {
    return (
      <div className="center-page">
        <div className="panel" style={{ alignItems: 'center' }}>
          <h2>对局进行中</h2>
          <p className="hint">正在同步牌局状态…</p>
        </div>
      </div>
    )
  }
  return <Table room={room} game={game} />
}

function Table({ room, game }: { room: RoomStatePayload; game: GameStatePayload }) {
  const playerId = useStore((s) => s.playerId)
  const wsStatus = useStore((s) => s.wsStatus)
  const playCard = useStore((s) => s.playCard)
  const drawCard = useStore((s) => s.drawCard)
  const pass = useStore((s) => s.pass)
  const chooseColor = useStore((s) => s.chooseColor)
  const callUno = useStore((s) => s.callUno)
  const catchUno = useStore((s) => s.catchUno)
  const sayUnoArmed = useStore((s) => s.sayUnoArmed)
  const toggleSayUno = useStore((s) => s.toggleSayUno)
  const resultDismissed = useStore((s) => s.resultDismissed)
  const dismissResult = useStore((s) => s.dismissResult)
  const leaveRoom = useStore((s) => s.leaveRoom)

  const remaining = useCountdown(game.turn_deadline)
  const isMyTurn = game.current_player_id === playerId
  const needChooseColor = game.color_chooser_id === playerId
  const awaitingDrawDecision =
    game.phase === 'awaiting_draw_decision' && isMyTurn
  const playableSet = useMemo(
    () => new Set(game.playable_card_ids ?? []),
    [game.playable_card_ids],
  )
  const nicknameOf = (id: string) =>
    room.members.find((m) => m.player_id === id)?.nickname ?? `玩家${id}`

  // 我方存在有效抓罚窗口时可以补喊 UNO；他人窗口则显示抓罚按钮。
  const myChallenge = game.uno_challenges?.some((c) => c.player_id === playerId)
  const opponents = game.players.filter((p) => p.player_id !== playerId)
  const canAct = isMyTurn && (game.phase === 'playing' || awaitingDrawDecision)
  const canDraw = canAct && !needChooseColor && game.phase === 'playing'

  const handClickable = (cardId: number): boolean => {
    if (!canAct || needChooseColor) return false
    return playableSet.has(cardId)
  }

  // 手牌扇形参数：数量越多角度越密。
  const handSize = game.hand.length
  const spread = Math.min(50, handSize * 5)
  const overlap = handSize > 12 ? -54 : handSize > 8 ? -44 : -28

  return (
    <div className="table-page">
      <div className="topbar">
        <div className="room-chip">
          房间 <b>{room.room_id}</b>
        </div>
        <div className="room-chip">
          牌堆 {game.draw_pile_count} ｜ 弃牌 {game.discard_pile_count}
        </div>
        {game.current_color && (
          <div className="room-chip">
            当前颜色
            <span
              style={{
                width: 14,
                height: 14,
                borderRadius: '50%',
                display: 'inline-block',
                background: COLOR_HEX[game.current_color],
              }}
            />
          </div>
        )}
        <div className="spacer" />
        <span className={`conn-dot ${wsStatus}`} title={`连接：${wsStatus}`} />
        <button className="btn btn-ghost" style={{ fontSize: 13 }} onClick={leaveRoom}>
          离开
        </button>
      </div>

      <div className="felt" />
      <DirectionRing direction={game.direction} />

      {/* 对手席位 */}
      <div className="opponents">
        {opponents.map((opponent, opponentIndex) => {
          const member = room.members.find(
            (m) => m.player_id === opponent.player_id,
          )
          const isCurrent = game.current_player_id === opponent.player_id
          const challenged = game.uno_challenges?.some(
            (c) => c.player_id === opponent.player_id,
          )
          const backs = Math.min(opponent.cards, 8)
          return (
            <div
              key={opponent.player_id}
              className={`seat ${isCurrent ? 'current' : ''}`}
              style={{ animationDelay: `${opponentIndex * 65}ms` }}
            >
              <div className="avatar" style={avatarStyle(opponent.seat)}>
                {nicknameOf(opponent.player_id).slice(0, 1).toUpperCase()}
                {member && !member.connected && (
                  <span className="badge-off" title="已断线">🔌</span>
                )}
              </div>
              <div className="name">{nicknameOf(opponent.player_id)}</div>
              <div className="tags">
                {member?.auto_play && <span className="tag-auto">托管中</span>}
                {isCurrent && <span>出牌中…</span>}
              </div>
              <div className="minihand">
                {Array.from({ length: backs }, (_, i) => (
                  <div key={i} className="card-back" />
                ))}
              </div>
              <div className="count">
                <b key={opponent.cards} className="count-value">{opponent.cards}</b> 张
              </div>
              {challenged && (
                <button
                  className="catch-btn"
                  onClick={() => catchUno(opponent.player_id)}
                >
                  抓罚 UNO！
                </button>
              )}
            </div>
          )
        })}
      </div>

      {/* 中央牌堆 */}
      <div className="table-center">
        <div className="pile">
          <div
            className={`draw-pile ${canDraw ? 'pulse' : ''}`}
            onClick={() => canDraw && drawCard()}
            onKeyDown={(event) => {
              if (canDraw && (event.key === 'Enter' || event.key === ' ')) {
                event.preventDefault()
                drawCard()
              }
            }}
            role="button"
            tabIndex={canDraw ? 0 : -1}
            aria-disabled={!canDraw}
            title={game.pending_draw > 0 ? `接受 +${game.pending_draw} 罚牌` : '摸一张牌'}
          >
            <div className="stack" style={{ transform: 'translate(4px, 4px)' }} />
            <div className="stack" style={{ transform: 'translate(2px, 2px)' }} />
            <div className="stack" />
            <span key={game.draw_pile_count} className="draw-feedback" aria-hidden="true" />
            {game.pending_draw > 0 && (
              <div className="pending-badge">+{game.pending_draw}</div>
            )}
          </div>
          <div className="pile-label">摸牌堆</div>
        </div>

        <div className="pile">
          <div className="discard" title={cardLabel(game.top_card)}>
            <div
              className="color-halo"
              style={{
                background: COLOR_HEX[game.current_color || game.top_card.color || ''],
              }}
            />
            {/* key 换牌时重放入场动画 */}
            <UnoCard key={game.top_card.id} card={game.top_card} width={104} />
          </div>
          <div className="pile-label">弃牌堆</div>
        </div>
      </div>

      {/* 我方区域 */}
      <div className="my-zone">
        <div className="action-bar">
          <span className={`turn-pill ${isMyTurn ? 'mine' : ''}`}>
            {game.phase === 'finished'
              ? '本局结束'
              : isMyTurn
                ? needChooseColor
                  ? '请选择颜色'
                  : awaitingDrawDecision
                    ? '出刚摸的牌或过'
                    : '轮到你出牌'
                : `等待 ${nicknameOf(game.current_player_id ?? '')}`}
          </span>
          {remaining !== null && game.phase !== 'finished' && (
            <span className={`timer ${remaining <= 5 && isMyTurn ? 'urgent' : ''}`}>
              {remaining}s
            </span>
          )}
          {myChallenge ? (
            <button className="uno-btn shout" onClick={callUno}>
              喊 UNO！
            </button>
          ) : (
            <button
              className={`uno-btn ${sayUnoArmed ? 'armed' : ''}`}
              onClick={toggleSayUno}
              disabled={handSize !== 2}
              title="剩两张牌时按下，出牌将同时宣告 UNO"
            >
              UNO
            </button>
          )}
          {awaitingDrawDecision && (
            <button className="btn" onClick={pass}>
              过（保留摸到的牌）
            </button>
          )}
        </div>

        <div className="hand">
          {game.hand.map((card, index) => {
            const mid = (handSize - 1) / 2
            const angle = handSize > 1 ? ((index - mid) / mid || 0) * (spread / 2) : 0
            const lift = Math.abs(index - mid) * (spread / 18)
            const playable = handClickable(card.id)
            const dimmed =
              canAct && !needChooseColor && !playable && playableSet.size > 0
            const isDrawn =
              awaitingDrawDecision && game.drawn_card_id === card.id
            return (
              <div
                key={card.id}
                className={`hand-card ${playable ? 'playable' : ''} ${dimmed ? 'dimmed' : ''} ${isDrawn ? 'drawn-highlight' : ''}`}
                style={{
                  transform: `rotate(${angle}deg) translateY(${lift}px)`,
                  marginLeft: index === 0 ? 0 : overlap,
                  zIndex: index,
                  animationDelay: `${Math.min(index, 12) * 24}ms`,
                }}
                title={cardLabel(card)}
                onClick={() => playable && playCard(card.id)}
                onKeyDown={(event) => {
                  if (playable && (event.key === 'Enter' || event.key === ' ')) {
                    event.preventDefault()
                    playCard(card.id)
                  }
                }}
                role="button"
                tabIndex={playable ? 0 : -1}
                aria-disabled={!playable}
              >
                <UnoCard card={card} width={96} />
              </div>
            )
          })}
        </div>
      </div>

      {/* 万能牌选色 */}
      {needChooseColor && (
        <div className="overlay">
          <div className="modal">
            <h3>选择生效颜色</h3>
            <div className="color-grid">
              {(['red', 'yellow', 'green', 'blue'] as const).map((color) => (
                <button
                  key={color}
                  className="color-cell"
                  style={{ background: COLOR_HEX[color] }}
                  onClick={() => chooseColor(color)}
                >
                  {COLOR_NAME[color]}
                </button>
              ))}
            </div>
          </div>
        </div>
      )}

      {/* 终局结算 */}
      {game.result && !resultDismissed && (
        <div className="overlay">
          <div className="modal">
            <div className="trophy">🏆</div>
            <h3>
              {game.result.winner_id === playerId
                ? '你赢了！'
                : `${nicknameOf(game.result.winner_id)} 获胜`}
            </h3>
            <table className="result-table">
              <thead>
                <tr>
                  <th>玩家</th>
                  <th>剩余手牌</th>
                  <th>手牌分</th>
                  <th>得分</th>
                </tr>
              </thead>
              <tbody>
                {game.result.players.map((entry) => (
                  <tr key={entry.player_id} className={entry.is_winner ? 'winner' : ''}>
                    <td>
                      {entry.is_winner ? '👑 ' : ''}
                      {nicknameOf(entry.player_id)}
                      {entry.player_id === playerId ? '（我）' : ''}
                    </td>
                    <td>{entry.cards_left}</td>
                    <td>{entry.hand_points}</td>
                    <td>{entry.is_winner ? `+${entry.score}` : '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            <div className="row" style={{ justifyContent: 'center' }}>
              <button className="btn btn-primary" onClick={dismissResult}>
                查看牌桌
              </button>
              <button className="btn" onClick={leaveRoom}>
                返回大厅
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

/** 环形方向指示：顺/逆时针箭头绕桌心旋转。 */
function DirectionRing({ direction }: { direction: 1 | -1 }) {
  return (
    <svg className="direction-ring" viewBox="0 0 100 70">
      <g
        style={{
          transformOrigin: '50px 35px',
          animation: `spin${direction === 1 ? 'CW' : 'CCW'} 14s linear infinite`,
        }}
      >
        <path
          d="M 50 6 A 42 28 0 0 1 92 35"
          fill="none"
          stroke="rgba(255,255,255,0.6)"
          strokeWidth="2"
          strokeDasharray="4 4"
        />
        <path
          d="M 50 64 A 42 28 0 0 1 8 35"
          fill="none"
          stroke="rgba(255,255,255,0.6)"
          strokeWidth="2"
          strokeDasharray="4 4"
        />
        <polygon points="92,35 88,27 96,29" fill="rgba(255,255,255,0.8)" />
        <polygon points="8,35 12,43 4,41" fill="rgba(255,255,255,0.8)" />
      </g>
      <style>{`
        @keyframes spinCW { to { transform: rotate(360deg); } }
        @keyframes spinCCW { to { transform: rotate(-360deg); } }
      `}</style>
    </svg>
  )
}
