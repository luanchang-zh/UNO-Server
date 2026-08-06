package uno

import "fmt"

// Color 表示 UNO 的四种可选颜色；万能牌尚未选色时使用 ColorNone。
type Color string

const (
	// ColorNone 表示当前尚未选择颜色。
	ColorNone Color = ""
	// ColorRed 表示红色。
	ColorRed Color = "red"
	// ColorYellow 表示黄色。
	ColorYellow Color = "yellow"
	// ColorGreen 表示绿色。
	ColorGreen Color = "green"
	// ColorBlue 表示蓝色。
	ColorBlue Color = "blue"
)

// Colors 按稳定顺序返回四种可选颜色。
func Colors() []Color {
	return []Color{ColorRed, ColorYellow, ColorGreen, ColorBlue}
}

// Valid 判断该颜色能否在对局中被选择。
func (c Color) Valid() bool {
	switch c {
	case ColorRed, ColorYellow, ColorGreen, ColorBlue:
		return true
	default:
		return false
	}
}

// Kind 表示牌的数字类型或功能类型。
type Kind string

const (
	// KindNumber 表示有色数字牌。
	KindNumber Kind = "number"
	// KindSkip 表示跳过下一座位。
	KindSkip Kind = "skip"
	// KindReverse 表示反转出牌方向。
	KindReverse Kind = "reverse"
	// KindDrawTwo 表示向累计罚牌增加两张。
	KindDrawTwo Kind = "draw_two"
	// KindWild 表示改变当前生效颜色。
	KindWild Kind = "wild"
	// KindWildDrawFour 表示改变颜色并向累计罚牌增加四张。
	KindWildDrawFour Kind = "wild_draw_four"
)

// CardID 唯一标识一张实体牌，可区分牌面相同的重复牌。
type CardID uint16

// Card 表示一张不可变的 UNO 实体牌；Number 仅对 KindNumber 有意义，
// 功能牌的 Number 固定为零。
type Card struct {
	// ID 是该实体牌在标准牌库中的稳定唯一标识。
	ID CardID `json:"id"`
	// Color 是有色牌的固有颜色；万能牌固定为 ColorNone。
	Color Color `json:"color,omitempty"`
	// Kind 是牌面类型，用于判断规则效果和匹配关系。
	Kind Kind `json:"kind"`
	// Number 是数字牌点数；所有功能牌固定为零。
	Number int `json:"number,omitempty"`
}

// Valid 判断牌是否符合本项目的标准表示。
func (c Card) Valid() bool {
	if c.ID == 0 {
		return false
	}
	switch c.Kind {
	case KindNumber:
		return c.Color.Valid() && c.Number >= 0 && c.Number <= 9
	case KindSkip, KindReverse, KindDrawTwo:
		return c.Color.Valid() && c.Number == 0
	case KindWild, KindWildDrawFour:
		return c.Color == ColorNone && c.Number == 0
	default:
		return false
	}
}

// IsWild 判断该牌是否需要选色。
func (c Card) IsWild() bool {
	return c.Kind == KindWild || c.Kind == KindWildDrawFour
}

// IsDrawPenalty 判断该牌能否参与叠罚。
func (c Card) IsDrawPenalty() bool {
	return c.Kind == KindDrawTwo || c.Kind == KindWildDrawFour
}

// Points 返回该牌在单局结算时的分值。
func (c Card) Points() int {
	switch c.Kind {
	case KindNumber:
		return c.Number
	case KindSkip, KindReverse, KindDrawTwo:
		return 20
	case KindWild, KindWildDrawFour:
		return 50
	default:
		return 0
	}
}

// Matches 判断该牌在普通回合能否打出；叠罚会覆盖普通的颜色与牌面匹配，
// 因此由 Engine 单独处理。
func (c Card) Matches(top Card, currentColor Color) bool {
	if c.IsWild() {
		return true
	}
	if c.Color == currentColor {
		return true
	}
	if top.IsWild() || c.Kind != top.Kind {
		return false
	}
	if c.Kind == KindNumber {
		return c.Number == top.Number
	}
	return true
}

// String 返回紧凑、便于阅读的牌描述。
func (c Card) String() string {
	if c.Kind == KindNumber {
		return fmt.Sprintf("%s-%d#%d", c.Color, c.Number, c.ID)
	}
	if c.IsWild() {
		return fmt.Sprintf("%s#%d", c.Kind, c.ID)
	}
	return fmt.Sprintf("%s-%s#%d", c.Color, c.Kind, c.ID)
}
