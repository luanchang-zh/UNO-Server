import { useStore } from '../store/store'

export function Toasts() {
  const toasts = useStore((s) => s.toasts)
  return (
    <div className="toasts">
      {toasts.map((toast) => (
        <div key={toast.id} className={`toast ${toast.kind}`}>
          {toast.text}
        </div>
      ))}
    </div>
  )
}
