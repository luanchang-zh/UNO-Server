import { useEffect } from 'react'
import { useStore } from './store/store'
import { LoginPage } from './screens/LoginPage'
import { LobbyPage } from './screens/LobbyPage'
import { RoomPage } from './screens/RoomPage'
import { GamePage } from './screens/GamePage'
import { Toasts } from './components/Toasts'

/** 按 token / 房间 / 房间阶段路由到对应屏幕。 */
export function App() {
  const token = useStore((s) => s.token)
  const room = useStore((s) => s.room)
  const connect = useStore((s) => s.connect)

  // 已有 token（刷新页面）时自动重连，服务端会换绑座位并补发状态。
  useEffect(() => {
    if (token) connect()
  }, [token, connect])

  let screen
  if (!token) {
    screen = <LoginPage />
  } else if (!room) {
    screen = <LobbyPage />
  } else if (room.phase === 'waiting') {
    screen = <RoomPage />
  } else {
    screen = <GamePage />
  }

  return (
    <>
      {screen}
      <Toasts />
    </>
  )
}
