package uno_test

import (
	"testing"

	"github.com/luanchang-zh/UNO-Server/internal/game/uno"
)

// TestPublicAPI 演示房间层如何创建对局并读取面向单名玩家的安全视图。
func TestPublicAPI(t *testing.T) {
	game, err := uno.New([]int64{101, 202}, uno.Config{
		Shuffle: func(_ []uno.Card) error { return nil },
	})
	if err != nil {
		t.Fatalf("创建对局失败：%v", err)
	}
	view, err := game.ViewFor(202)
	if err != nil {
		t.Fatalf("读取玩家视图失败：%v", err)
	}
	if view.Phase != uno.PhasePlaying || len(view.Players) != 2 ||
		len(view.Hand) != uno.InitialHandSize || view.CurrentPlayerID != 202 {
		t.Fatalf("公开接口返回了非预期视图：%+v", view)
	}
}
