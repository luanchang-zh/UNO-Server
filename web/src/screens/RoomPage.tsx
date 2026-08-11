import { useStore } from '../store/store'
import { COLOR_HEX } from '../components/UnoCard'

const AVATAR_COLORS = ['red', 'blue', 'green', 'yellow']

export function avatarStyle(index: number) {
  const key = AVATAR_COLORS[index % AVATAR_COLORS.length]
  return { background: COLOR_HEX[key] }
}

/** 等待房间：成员就位、准备、房主开局/踢人。 */
export function RoomPage() {
  const room = useStore((s) => s.room)!
  const playerId = useStore((s) => s.playerId)
  const setReady = useStore((s) => s.setReady)
  const startGame = useStore((s) => s.startGame)
  const leaveRoom = useStore((s) => s.leaveRoom)
  const kick = useStore((s) => s.kick)
  const pushToast = useStore((s) => s.pushToast)

  const me = room.members.find((m) => m.player_id === playerId)
  const isOwner = room.owner_id === playerId
  const allReady =
    room.members.length >= 2 && room.members.every((m) => m.ready)
  const emptySeats = Math.max(0, room.max_players - room.members.length)

  const copyRoomId = () => {
    void navigator.clipboard
      .writeText(room.room_id)
      .then(() => pushToast('success', '房间号已复制'))
      .catch(() => pushToast('info', `房间号：${room.room_id}`))
  }

  return (
    <div className="center-page">
      <div className="panel" style={{ width: 'min(680px, 94vw)' }}>
        <div className="row" style={{ justifyContent: 'space-between' }}>
          <h2 style={{ margin: 0 }}>等待开局</h2>
          <button className="btn btn-ghost" onClick={copyRoomId} title="点击复制">
            房间号 <b style={{ color: 'var(--accent)', letterSpacing: 1 }}>{room.room_id}</b> 📋
          </button>
        </div>

        <div className="room-grid">
          {room.members.map((member, index) => (
            <div
              key={member.player_id}
              className={`member-card ${member.player_id === playerId ? 'me' : ''}`}
              style={{ animationDelay: `${Math.min(index, 5) * 55}ms` }}
            >
              {member.is_owner && <span className="crown" title="房主">👑</span>}
              {isOwner && !member.is_owner && (
                <button
                  className="kick"
                  title="移出房间"
                  onClick={() => kick(member.player_id)}
                >
                  ✕
                </button>
              )}
              <div className="avatar" style={avatarStyle(index)}>
                {member.nickname.slice(0, 1).toUpperCase()}
              </div>
              <div className="who">
                {member.nickname}
                {member.player_id === playerId ? '（我）' : ''}
              </div>
              <div className={`status ${member.ready ? 'ready' : ''}`}>
                {!member.connected ? '离线' : member.ready ? '已准备 ✓' : '未准备'}
              </div>
            </div>
          ))}
          {Array.from({ length: emptySeats }, (_, i) => (
            <div
              key={`empty-${i}`}
              className="member-card empty"
              style={{ animationDelay: `${Math.min(room.members.length + i, 5) * 55}ms` }}
            >
              等待加入…
            </div>
          ))}
        </div>

        <div className="row" style={{ justifyContent: 'center', marginTop: 6 }}>
          {isOwner ? (
            <button
              className={`btn btn-primary ${allReady ? 'ready-to-start' : ''}`}
              disabled={!allReady}
              onClick={startGame}
              title={allReady ? '' : '所有玩家准备后才能开始'}
            >
              开始游戏（{room.members.length}/{room.max_players}）
            </button>
          ) : (
            <button
              className={me?.ready ? 'btn' : 'btn btn-accent'}
              onClick={() => setReady(!me?.ready)}
            >
              {me?.ready ? '取消准备' : '准备'}
            </button>
          )}
          <button className="btn btn-ghost" onClick={leaveRoom}>
            离开房间
          </button>
        </div>
      </div>
    </div>
  )
}
