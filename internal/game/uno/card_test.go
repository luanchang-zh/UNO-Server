package uno

import "testing"

// TestNewStandardDeck_Composition 验证标准 108 张牌的精确组成。
func TestNewStandardDeck_Composition(t *testing.T) {
	deck := NewStandardDeck()
	if len(deck) != StandardDeckSize {
		t.Fatalf("deck size=%d, want %d", len(deck), StandardDeckSize)
	}

	seenIDs := make(map[CardID]struct{}, len(deck))
	counts := make(map[Kind]int)
	zerosByColor := make(map[Color]int)
	for _, card := range deck {
		if !card.Valid() {
			t.Fatalf("invalid card: %+v", card)
		}
		if _, exists := seenIDs[card.ID]; exists {
			t.Fatalf("duplicate card ID: %d", card.ID)
		}
		seenIDs[card.ID] = struct{}{}
		counts[card.Kind]++
		if card.Kind == KindNumber && card.Number == 0 {
			zerosByColor[card.Color]++
		}
	}

	wantCounts := map[Kind]int{
		KindNumber:       76,
		KindSkip:         8,
		KindReverse:      8,
		KindDrawTwo:      8,
		KindWild:         4,
		KindWildDrawFour: 4,
	}
	for kind, want := range wantCounts {
		if got := counts[kind]; got != want {
			t.Errorf("%s count=%d, want %d", kind, got, want)
		}
	}
	for _, color := range Colors() {
		if got := zerosByColor[color]; got != 1 {
			t.Errorf("%s zero count=%d, want 1", color, got)
		}
	}
}

// TestCardMatches 覆盖普通颜色、相同牌面和万能牌匹配规则。
func TestCardMatches(t *testing.T) {
	top := Card{ID: 1, Color: ColorRed, Kind: KindNumber, Number: 5}
	tests := []struct {
		name string
		card Card
		want bool
	}{
		{name: "same color", card: Card{ID: 2, Color: ColorRed, Kind: KindNumber, Number: 7}, want: true},
		{name: "same number", card: Card{ID: 3, Color: ColorBlue, Kind: KindNumber, Number: 5}, want: true},
		{name: "no match", card: Card{ID: 4, Color: ColorBlue, Kind: KindSkip}, want: false},
		{name: "wild", card: Card{ID: 5, Kind: KindWild}, want: true},
		{name: "wild draw four", card: Card{ID: 6, Kind: KindWildDrawFour}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.card.Matches(top, ColorRed); got != test.want {
				t.Fatalf("Matches()=%v, want %v", got, test.want)
			}
		})
	}

	wildTop := Card{ID: 7, Kind: KindWild}
	if (Card{ID: 8, Color: ColorBlue, Kind: KindNumber, Number: 9}).Matches(wildTop, ColorGreen) {
		t.Fatal("card matching only the old face must not match a wild top")
	}
}

// TestCardPoints 验证各类牌在结算时的分值。
func TestCardPoints(t *testing.T) {
	tests := []struct {
		card Card
		want int
	}{
		{card: Card{Kind: KindNumber, Number: 7}, want: 7},
		{card: Card{Kind: KindSkip}, want: 20},
		{card: Card{Kind: KindReverse}, want: 20},
		{card: Card{Kind: KindDrawTwo}, want: 20},
		{card: Card{Kind: KindWild}, want: 50},
		{card: Card{Kind: KindWildDrawFour}, want: 50},
	}
	for _, test := range tests {
		if got := test.card.Points(); got != test.want {
			t.Errorf("%s points=%d, want %d", test.card.Kind, got, test.want)
		}
	}
}

// TestSecureShufflePreservesDeck 验证生产洗牌器会且只会保留每张实体牌一次。
func TestSecureShufflePreservesDeck(t *testing.T) {
	deck := NewStandardDeck()
	if err := secureShuffle(deck); err != nil {
		t.Fatalf("secureShuffle() error: %v", err)
	}
	seen := make(map[CardID]bool, len(deck))
	for _, card := range deck {
		if seen[card.ID] {
			t.Fatalf("duplicate card after shuffle: %d", card.ID)
		}
		seen[card.ID] = true
	}
	if len(seen) != StandardDeckSize {
		t.Fatalf("cards after shuffle=%d", len(seen))
	}
}
