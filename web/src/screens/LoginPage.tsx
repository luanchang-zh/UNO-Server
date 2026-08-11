import { useState, type FormEvent } from 'react'
import { useStore } from '../store/store'

/** 登录页：输入昵称，游客登录后自动建立 WebSocket 连接。 */
export function LoginPage() {
  const login = useStore((s) => s.login)
  const loggingIn = useStore((s) => s.loggingIn)
  const [nickname, setNickname] = useState(
    () => localStorage.getItem('uno.nickname') ?? '',
  )

  const submit = (event: FormEvent) => {
    event.preventDefault()
    void login(nickname)
  }

  return (
    <div className="center-page">
      <div className="brand">
        <span className="brand-logo">UNO</span>
        <span className="brand-sub">在线对战</span>
      </div>
      <form className="panel" onSubmit={submit}>
        <h2>开始游戏</h2>
        <p className="hint">输入一个昵称即可加入，无需注册。</p>
        <input
          className="input"
          placeholder="你的昵称（留空则为「游客」）"
          maxLength={32}
          value={nickname}
          onChange={(e) => setNickname(e.target.value)}
          autoFocus
        />
        <button
          className={`btn btn-primary ${loggingIn ? 'busy' : ''}`}
          type="submit"
          disabled={loggingIn}
        >
          {loggingIn ? '登录中…' : '进入游戏'}
        </button>
      </form>
    </div>
  )
}
