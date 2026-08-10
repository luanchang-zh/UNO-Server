import type { Card as CardData } from '../protocol/types'

export const COLOR_HEX: Record<string, string> = {
  red: '#e4373f',
  yellow: '#f2c231',
  green: '#37b34a',
  blue: '#2c7de0',
  '': '#232327',
}

export const COLOR_NAME: Record<string, string> = {
  red: '红色',
  yellow: '黄色',
  green: '绿色',
  blue: '蓝色',
}

/** 中央与角标使用的牌面文字。 */
function faceText(card: CardData): { center: string; corner: string } {
  switch (card.kind) {
    case 'number': {
      const n = String(card.number ?? 0)
      return { center: n, corner: n }
    }
    case 'skip':
      return { center: '⊘', corner: '⊘' }
    case 'reverse':
      return { center: '⇄', corner: '⇄' }
    case 'draw_two':
      return { center: '+2', corner: '+2' }
    case 'wild':
      return { center: '', corner: 'W' }
    case 'wild_draw_four':
      return { center: '+4', corner: '+4' }
  }
}

const WILD_QUADRANTS: Array<{ color: string; d: string }> = [
  { color: '#e4373f', d: 'M50 67 L50 33 A34 34 0 0 0 16 67 Z' },
  { color: '#f2c231', d: 'M50 67 L16 67 A34 34 0 0 0 50 101 Z' },
  { color: '#37b34a', d: 'M50 67 L50 101 A34 34 0 0 0 84 67 Z' },
  { color: '#2c7de0', d: 'M50 67 L84 67 A34 34 0 0 0 50 33 Z' },
]

interface CardProps {
  card: CardData
  width?: number
  onClick?: () => void
}

/** SVG 绘制的 UNO 牌面：彩底 + 倾斜白椭圆 + 大字与双角标。 */
export function UnoCard({ card, width = 92, onClick }: CardProps) {
  const height = (width * 134) / 92
  const isWild = card.kind === 'wild' || card.kind === 'wild_draw_four'
  const base = isWild ? '#161619' : COLOR_HEX[card.color ?? ''] ?? '#232327'
  const { center, corner } = faceText(card)
  const bigFont = center.length > 1 ? 34 : 46

  return (
    <svg
      className="uno-card"
      width={width}
      height={height}
      viewBox="0 0 100 134"
      onClick={onClick}
      role={onClick ? 'button' : undefined}
    >
      <rect x="1" y="1" width="98" height="132" rx="10" fill="#f7f7f4" />
      <rect x="6" y="6" width="88" height="122" rx="7" fill={base} />
      {/* 倾斜白椭圆是 UNO 牌面的招牌元素 */}
      <ellipse
        cx="50"
        cy="67"
        rx="30"
        ry="46"
        fill={isWild ? '#101013' : '#f7f7f4'}
        transform="rotate(-32 50 67)"
        opacity={isWild ? 1 : 0.92}
      />
      {isWild && (
        <g transform="rotate(-32 50 67)">
          {WILD_QUADRANTS.map((q) => (
            <path key={q.color} d={q.d} fill={q.color} />
          ))}
        </g>
      )}
      {center && (
        <text
          x="50"
          y="67"
          textAnchor="middle"
          dominantBaseline="central"
          fontSize={bigFont}
          fontWeight="900"
          fontStyle="italic"
          fill={isWild ? '#fff' : base}
          stroke={isWild ? 'none' : '#1b1b1b'}
          strokeWidth={isWild ? 0 : 1.6}
          paintOrder="stroke"
          style={{ userSelect: 'none' }}
        >
          {center}
        </text>
      )}
      <text
        x="13"
        y="24"
        fontSize="15"
        fontWeight="800"
        fontStyle="italic"
        fill="#f7f7f4"
        style={{ userSelect: 'none' }}
      >
        {corner}
      </text>
      {/* 以牌面中心旋转 180° 得到对角的倒置角标 */}
      <text
        x="13"
        y="24"
        fontSize="15"
        fontWeight="800"
        fontStyle="italic"
        fill="#f7f7f4"
        transform="rotate(180 50 67)"
        style={{ userSelect: 'none' }}
      >
        {corner}
      </text>
    </svg>
  )
}

/** 牌面中文描述，用于悬浮提示。 */
export function cardLabel(card: CardData): string {
  const colorName = COLOR_NAME[card.color ?? ''] ?? ''
  switch (card.kind) {
    case 'number':
      return `${colorName} ${card.number ?? 0}`
    case 'skip':
      return `${colorName} 跳过`
    case 'reverse':
      return `${colorName} 反转`
    case 'draw_two':
      return `${colorName} +2`
    case 'wild':
      return '万能牌'
    case 'wild_draw_four':
      return '万能 +4'
  }
}
