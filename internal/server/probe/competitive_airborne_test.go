package probe

import (
	"testing"
	"time"

	"qqtang/internal/game/mapdata"
)

func TestCompetitiveAirborneNativeCadence(t *testing.T) {
	if got := competitiveAirborneInterval(5 * time.Second); got != 8375*time.Millisecond {
		t.Fatalf("initial interval = %s", got)
	}
	if got := competitiveAirborneInterval(80 * time.Second); got != 4*time.Second {
		t.Fatalf("minimum interval = %s", got)
	}
	if normal, powerful, special := competitiveAirborneCounts(5*time.Second, 100); normal != 5 || powerful != 1 || special != 0 {
		t.Fatalf("early counts = %d/%d/%d", normal, powerful, special)
	}
	if normal, powerful, special := competitiveAirborneCounts(160*time.Second, 100); normal != 15 || powerful != 10 || special != 3 {
		t.Fatalf("late counts = %d/%d/%d", normal, powerful, special)
	}
	if _, _, special := competitiveAirborneCounts(160*time.Second, 299); special != 0 {
		t.Fatalf("unexpected special count %d", special)
	}
}

func TestBuildCompetitiveAirborneBatchUsesUniqueMapCells(t *testing.T) {
	cells := make([]mapdata.CompetitiveCell, 0, 40)
	for row := byte(0); row < 4; row++ {
		for col := byte(0); col < 10; col++ {
			cells = append(cells, mapdata.CompetitiveCell{Row: row, Col: col})
		}
	}
	schedule := competitiveAirborneSchedule{RoomID: 1, GameID: 7, MapID: 411, ArbitratorID: 2, BossID: "first_mate", Cells: cells}
	batch, err := buildCompetitiveAirborneBatch(schedule, 32*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if batch.PlayerID != 2 || batch.Time != 32_000 || len(batch.Bombs) != 10 {
		t.Fatalf("batch = %+v", batch)
	}
	seen := make(map[[2]byte]struct{}, len(batch.Bombs))
	for _, bomb := range batch.Bombs {
		cell := [2]byte{bomb.Row, bomb.Col}
		if _, duplicate := seen[cell]; duplicate {
			t.Fatalf("duplicate cell %v", cell)
		}
		seen[cell] = struct{}{}
	}
}
