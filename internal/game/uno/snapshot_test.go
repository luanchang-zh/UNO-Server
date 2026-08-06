package uno

import (
	"encoding/json"
	"errors"
	"math/rand"
	"reflect"
	"strconv"
	"testing"
)

// TestSnapshotRestoreRoundTrip 验证未完成万能牌动作的 JSON 持久化，
// 以及恢复后继续执行玩法命令的能力。
func TestSnapshotRestoreRoundTrip(t *testing.T) {
	engine, err := New([]int64{11, 22}, Config{Shuffle: noShuffle})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	var wild Card
	for _, card := range engine.players[1].hand {
		if card.Kind == KindWild {
			wild = card
			break
		}
	}
	if wild.ID == 0 {
		t.Fatal("deterministic hand did not contain a wild")
	}
	if err := engine.Play(22, wild.ID, false); err != nil {
		t.Fatalf("Play wild: %v", err)
	}

	want := engine.Snapshot()
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	var decoded Snapshot
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	restored, err := Restore(decoded, Config{Shuffle: noShuffle})
	if err != nil {
		t.Fatalf("Restore() error: %v", err)
	}
	if got := restored.Snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("restored snapshot differs\ngot:  %+v\nwant: %+v", got, want)
	}
	if err := restored.ChooseColor(22, ColorGreen); err != nil {
		t.Fatalf("continued ChooseColor() error: %v", err)
	}
}

// TestSnapshotIsDeepCopy 验证调用方无法通过快照修改运行中的引擎状态。
func TestSnapshotIsDeepCopy(t *testing.T) {
	engine, err := New([]int64{1, 2}, Config{Shuffle: noShuffle})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	snapshot := engine.Snapshot()
	original := snapshot.Players[0].Hand[0]
	snapshot.Players[0].Hand[0] = Card{}
	snapshot.DrawPile[0] = Card{}
	snapshot.DiscardPile[0] = Card{}

	fresh := engine.Snapshot()
	if fresh.Players[0].Hand[0] != original {
		t.Fatal("mutating snapshot changed live hand")
	}
	if !fresh.DrawPile[0].Valid() || !fresh.DiscardPile[0].Valid() {
		t.Fatal("mutating snapshot changed live piles")
	}
}

// TestRestoreRejectsDuplicateCard 验证恢复过程会拒绝破坏实体牌守恒的重复牌。
func TestRestoreRejectsDuplicateCard(t *testing.T) {
	engine, err := New([]int64{1, 2}, Config{Shuffle: noShuffle})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	snapshot := engine.Snapshot()
	snapshot.DrawPile[0] = snapshot.Players[0].Hand[0]
	_, err = Restore(snapshot, Config{Shuffle: noShuffle})
	if !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("Restore() error=%v, want ErrInvalidSnapshot", err)
	}
}

// TestCompleteRoundSimulation 只通过公开命令驱动对局直至结算，
// 并在每次状态迁移后校验快照不变量。
func TestCompleteRoundSimulation(t *testing.T) {
	engine, err := New([]int64{1, 2, 3, 4}, Config{Shuffle: noShuffle})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	driveRound(t, engine)
}

// TestRandomizedRoundSimulations 使用多种确定性牌序执行轻量属性测试，
// 持续检查状态机与实体牌守恒。
func TestRandomizedRoundSimulations(t *testing.T) {
	for seed := int64(1); seed <= 25; seed++ {
		t.Run(strconv.FormatInt(seed, 10), func(t *testing.T) {
			random := rand.New(rand.NewSource(seed))
			shuffle := func(cards []Card) error {
				random.Shuffle(len(cards), func(left, right int) {
					cards[left], cards[right] = cards[right], cards[left]
				})
				return nil
			}
			engine, err := New([]int64{1, 2, 3, 4}, Config{Shuffle: shuffle})
			if err != nil {
				t.Fatalf("New() error: %v", err)
			}
			driveRound(t, engine)
		})
	}
}

// driveRound 只使用公开命令推进一局，并校验过程中产生的每个状态。
func driveRound(t *testing.T, engine *Engine) {
	t.Helper()
	const maxActions = 10_000
	for action := 0; action < maxActions && engine.phase != PhaseFinished; action++ {
		if err := validateSnapshot(engine.Snapshot()); err != nil {
			t.Fatalf("action %d invalid state: %v", action, err)
		}
		switch engine.phase {
		case PhaseAwaitingColor:
			chooserID := engine.players[engine.pendingColor.seat].id
			if err := engine.ChooseColor(chooserID, ColorRed); err != nil {
				t.Fatalf("action %d choose color: %v", action, err)
			}
		case PhaseAwaitingDrawDecision:
			playerID := engine.currentPlayerID()
			if err := engine.Play(playerID, engine.drawnCardID, true); err != nil {
				t.Fatalf("action %d play drawn: %v", action, err)
			}
		case PhasePlaying:
			playerID := engine.currentPlayerID()
			playable, err := engine.PlayableCards(playerID)
			if err != nil {
				t.Fatalf("action %d playable cards: %v", action, err)
			}
			if len(playable) > 0 {
				if err := engine.Play(playerID, playable[0].ID, true); err != nil {
					t.Fatalf("action %d play: %v", action, err)
				}
				continue
			}
			if _, err := engine.Draw(playerID); err != nil {
				t.Fatalf("action %d draw: %v", action, err)
			}
		}
	}
	if engine.phase != PhaseFinished || engine.result == nil {
		t.Fatalf("round did not finish: phase=%s result=%+v", engine.phase, engine.result)
	}
	if err := validateSnapshot(engine.Snapshot()); err != nil {
		t.Fatalf("final snapshot: %v", err)
	}
}
