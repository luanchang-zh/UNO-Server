import { useEffect, useState } from 'react'

/** 返回距 deadline 剩余的整秒数；无 deadline 时为 null。 */
export function useCountdown(deadline: string | undefined): number | null {
  const [remaining, setRemaining] = useState<number | null>(null)

  useEffect(() => {
    if (!deadline) {
      setRemaining(null)
      return
    }
    const target = new Date(deadline).getTime()
    const tick = () => {
      setRemaining(Math.max(0, Math.ceil((target - Date.now()) / 1000)))
    }
    tick()
    const timer = setInterval(tick, 250)
    return () => clearInterval(timer)
  }, [deadline])

  return remaining
}
