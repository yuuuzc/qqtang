package probe

import (
	"errors"
	"testing"

	"qqtang/internal/protocol/game"
)

func TestClassifyStartGameRejection(t *testing.T) {
	tests := []struct {
		context string
		id      uint16
		message string
	}{
		{"competitive room 1 requires at least two players", game.StartGameResultPlayersRequired, "玩家个数不足，不能开始游戏。"},
		{"room player 2 is not ready", game.StartGameResultPlayersNotReady, "必须所有玩家准备才可以开始游戏。"},
		{"competitive room 1 requires at least two teams", game.StartGameResultTeamInvalid, "需要至少两个队伍才可以开始游戏。"},
		{"standard competitive room 1 has unbalanced team 2 size 1; want 2", game.StartGameResultTeamInvalid, "标准场各队人数必须相同。"},
		{"single-player competitive Boss room 1 requires item 30098", game.StartGameResultItemRequired, "单人竞技需要激活单人BOSS卡。"},
	}
	for _, test := range tests {
		t.Run(test.context, func(t *testing.T) {
			got := classifyStartGameRejection(errors.New(test.context))
			if got.ResultID != test.id || got.Message != test.message {
				t.Fatalf("rejection = %+v, want id 0x%04X message %q", got, test.id, test.message)
			}
		})
	}
}
