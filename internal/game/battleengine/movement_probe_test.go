package battleengine

import "testing"

func TestResolveNativeMovementProbeUsesProductionCollisionPath(t *testing.T) {
	grid := testOpenGrid(3, 3)
	probe := NativeMovementProbe{
		Grid: grid, Position: PositionAtCellCenter(Cell{Row: 1, Col: 1}),
		Direction: DirectionRight, TickMS: 50, SpeedPixelsPerSecond: 40,
	}
	result, err := ResolveNativeMovementProbe(probe)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Position{X: 62, Y: 60}); result.Position != want || !result.Moved {
		t.Fatalf("open probe=%+v, want position %+v", result, want)
	}

	grid.Cells[1*3+2] = Tile{Kind: CellSolid}
	probe.Grid = grid
	result, err = ResolveNativeMovementProbe(probe)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Position{X: 60, Y: 60}); result.Position != want || result.Moved {
		t.Fatalf("blocked probe=%+v, want position %+v", result, want)
	}
}

func TestResolveNativeMovementProbeCoversCornerThreshold(t *testing.T) {
	grid := testOpenGrid(5, 5)
	grid.Cells[1*5+2] = Tile{Kind: CellSolid}
	for _, test := range []struct {
		name string
		y    int32
		want int32
	}{
		{name: "six corrects", y: 46, want: 44},
		{name: "seven stays", y: 47, want: 47},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := ResolveNativeMovementProbe(NativeMovementProbe{
				Grid: grid, Position: Position{X: 60, Y: test.y}, Direction: DirectionRight,
				TickMS: 50, SpeedPixelsPerSecond: 40,
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Position.X != 60 || result.Position.Y != test.want {
				t.Fatalf("probe position=%+v, want X=60 Y=%d", result.Position, test.want)
			}
		})
	}
}
