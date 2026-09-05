package battleengine

import (
	"fmt"

	"qqtang/internal/game/mapdata"
)

// nativeMapRNG is the client RNG at Client+0x522F0. Both the SpawnSeed and
// ItemSeed paths instantiate this same generator. One call advances the
// 32-bit state three times. Arithmetic intentionally wraps at uint32.
type nativeMapRNG struct {
	state uint32
}

func (rng *nativeMapRNG) next() uint32 {
	first := rng.state*0x41c64e6d + 0x3039
	second := first*0x41c64e6d + 0x3039
	third := second*0x41c64e6d + 0x3039
	rng.state = third
	return (((first>>16)%0x800)<<10^((second>>16)%0x400))<<10 ^ ((third >> 16) % 0x400)
}

// PlaceHiddenPickups reproduces Client+0x1C1BB8 and the ordinary branch of
// Client+0x1C2CE7. The client copies the map-owned candidate list, performs N
// pairs of full-range swaps using ItemSeed, then expands GAME_BEGIN.NewItems
// in wire order until coordinates are exhausted.
//
// candidates must be the client's map-owned primary candidate list. This
// function deliberately does not infer it from every breakable grid cell:
// multi-cell scenery and visual canHide metadata are not equivalent to one
// native candidate anchor.
func PlaceHiddenPickups(itemSeed uint32, candidates []Cell, items []mapdata.CompetitiveWallItem) ([]Pickup, error) {
	shuffled := append([]Cell(nil), candidates...)
	seen := make(map[Cell]struct{}, len(shuffled))
	for index, cell := range shuffled {
		if cell.Row < 0 || cell.Col < 0 {
			return nil, fmt.Errorf("hidden-item candidate %d has negative cell %d,%d", index, cell.Row, cell.Col)
		}
		if _, duplicate := seen[cell]; duplicate {
			return nil, fmt.Errorf("hidden-item candidate repeats cell %d,%d", cell.Row, cell.Col)
		}
		seen[cell] = struct{}{}
	}
	rng := nativeMapRNG{state: itemSeed}
	if count := uint32(len(shuffled)); count != 0 {
		for range shuffled {
			first := rng.next() % count
			second := rng.next() % count
			shuffled[first], shuffled[second] = shuffled[second], shuffled[first]
		}
	}

	result := make([]Pickup, 0, len(shuffled))
	coordinateIndex := 0
	for itemIndex, item := range items {
		if item.SceneID == 0 {
			return nil, fmt.Errorf("wall item %d has zero scene ID", itemIndex)
		}
		if item.Quantity < 0 {
			return nil, fmt.Errorf("wall item %d scene %d has negative quantity %d", itemIndex, item.SceneID, item.Quantity)
		}
		for quantityIndex := int16(0); quantityIndex < item.Quantity && coordinateIndex < len(shuffled); quantityIndex++ {
			result = append(result, Pickup{SceneID: item.SceneID, Cell: shuffled[coordinateIndex], State: PickupHidden})
			coordinateIndex++
		}
		if coordinateIndex == len(shuffled) {
			break
		}
	}
	return result, nil
}
