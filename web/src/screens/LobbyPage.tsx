import { useState, type FormEvent } from 'react'
import { useStore } from '../store/store'

/** 大厅：创建房间或按房间号加入。 */
export function LobbyPage() {
  const nickname = useStore((s) => s.nickname)
  const wsStatus = useStore((s) => s.wsStatus)
  const createRoom = useStore((s) => s.createRoom)
  const joinRoom = useStore((s) => s.joinRoom)
  const logout = useStore((s) => s.logout)
  const [maxPlayers, setMaxPlayers] = useState(4)
  const [roomId, setRoomId] = useState('')

  const online = wsStatus === 'open'

  const submitJoin = (event: FormEvent) => {
    event.preventDefault()
    if (roomId.trim()) joinRoom(roomId)
  }

  return (
    <div className="center-page">
      <div className="brand">
        <span className="brand-logo">UNO</span>
        <span className="brand-sub">在线对战</span>
      </div>
      <div className="panel">
        <div className="row" style={{ justifyContent: 'space-between' }}>
          <h2 style={{ margin: 0 }}>你好，{nickname}</h2>
          <span
            className={`conn-dot ${wsStatus}`}
            title={online ? '已连接' : '连接中…'}
          />
        </div>

        <p className="hint">选择本局人数后创建房间：</p>
        <div className="seg">
          {[2, 3, 4, 5, 6].map((n) => (
            <button
              key={n}
              className={n === maxPlayers ? 'on' : ''}
              onClick={() => setMaxPlayers(n)}
            >
              {n} 人
            </button>
          ))}
        </div>
        <button
          className="btn btn-primary"
          disabled={!online}
          onClick={() => createRoom(maxPlayers)}
        >
          创建房间
        </button>

        <div className="divider">或加入朋友的房间</div>
        <form className="row" onSubmit={submitJoin}>
          <input
            className="input"
            placeholder="输入房间号"
            value={roomId}
            onChange={(e) => setRoomId(e.target.value)}
          />
          <button
            className="btn btn-accent"
            type="submit"
            disabled={!online || !roomId.trim()}
          >
            加入
          </button>
        </form>

        <button className="btn btn-ghost" style={{ fontSize: 13 }} onClick={logout}>
          退出登录
        </button>
      </div>
    </div>
  )
}
