package battleengine

import "sort"

func (engine *Engine) placeBomb(actorIndex int) (Event, bool) {
	actor := &engine.actors[actorIndex]
	cell := actor.Position.Cell()
	capacity := engine.actorCapabilities(actor, actor.Facing).EffectiveBombCapacity
	if engine.activeBombCount(actor.PlayerID) >= int(capacity) || engine.bombAt(cell) >= 0 {
		return Event{}, false
	}
	return engine.placeBombOnSnapshotEmptyCell(actorIndex)
}

// placeBombOnSnapshotEmptyCell is used by Step after it has evaluated native
// local occupancy against the pre-placement battlefield. FUN_005b08e0 checks
// the local cell before producing 0xFA3, while the remote 0xFA3 consumer at
// FUN_006084bf creates the bubble without repeating that occupancy check.
// Consequently, different players that all pass their local check in one
// input frame may produce multiple bubbles in one cell (the native 叠炮 race).
func (engine *Engine) placeBombOnSnapshotEmptyCell(actorIndex int) (Event, bool) {
	actor := &engine.actors[actorIndex]
	cell := actor.Position.Cell()
	// The original local producer rejects placement unless the actor's scene
	// cell is an ordinary open grid cell. Native pass/up-wall can temporarily
	// render an actor over scenery, but it does not turn that scenery into a
	// legal bubble cell.
	tile, inside := engine.grid.Cell(cell)
	if !inside || tile.Kind != CellOpen || tile.MapElementOccupied {
		return Event{}, false
	}
	capacity := engine.actorCapabilities(actor, actor.Facing).EffectiveBombCapacity
	if engine.activeBombCount(actor.PlayerID) >= int(capacity) {
		return Event{}, false
	}
	bomb := Bomb{
		ID: engine.nextBombID, OwnerID: actor.PlayerID, Cell: cell, Power: actor.BombPower,
		ExplodeAtMS:     saturatingAdd(engine.elapsedMS, engine.rules.BombFuseMS),
		SceneFourEffect: engine.sceneFourEffectActive(actor),
	}
	engine.nextBombID++
	engine.bombs = append(engine.bombs, bomb)
	return Event{Kind: EventBombPlaced, TimeMS: engine.elapsedMS, PlayerID: actor.PlayerID, BombID: bomb.ID, Cell: cell}, true
}

func (engine *Engine) explodeDueBombs() []Event {
	return engine.explodeDueBombsMatching(nil)
}

func (engine *Engine) explodeDueBombsMatching(acceptRoot func(Bomb) bool) []Event {
	queue := make([]uint32, 0)
	scheduled := make(map[uint32]bool)
	for _, bomb := range engine.bombs {
		if bomb.EffectiveExplodeAtMS() <= engine.elapsedMS && (acceptRoot == nil || acceptRoot(bomb)) {
			queue = append(queue, bomb.ID)
			scheduled[bomb.ID] = true
		}
	}
	exploded := make(map[uint32]bool)
	events := make([]Event, 0)
	for len(queue) > 0 {
		bombID := queue[0]
		queue = queue[1:]
		bombIndex := engine.bombIndexByID(bombID)
		if bombIndex < 0 || exploded[bombID] {
			continue
		}
		bomb := engine.bombs[bombIndex]
		exploded[bombID] = true
		blastCells, wallEvents := engine.blastCells(bomb, scheduled, &queue)
		rowMin, rowMax := bomb.Cell.Row, bomb.Cell.Row
		colMin, colMax := bomb.Cell.Col, bomb.Cell.Col
		for _, cell := range blastCells {
			if cell.Row < rowMin {
				rowMin = cell.Row
			}
			if cell.Row > rowMax {
				rowMax = cell.Row
			}
			if cell.Col < colMin {
				colMin = cell.Col
			}
			if cell.Col > colMax {
				colMax = cell.Col
			}
		}
		events = append(events, Event{
			Kind: EventBombExploded, TimeMS: engine.elapsedMS, PlayerID: bomb.OwnerID,
			BombID: bomb.ID, Cell: bomb.Cell,
			BlastRowMin: rowMin, BlastRowMax: rowMax, BlastColMin: colMin, BlastColMax: colMax,
		})
		for _, cell := range blastCells {
			engine.addFlame(cell, bomb.OwnerID)
		}
		events = append(events, wallEvents...)
	}
	if len(exploded) == 0 {
		return events
	}
	keptBombs := engine.bombs[:0]
	for _, bomb := range engine.bombs {
		if !exploded[bomb.ID] {
			keptBombs = append(keptBombs, bomb)
		}
	}
	engine.bombs = keptBombs
	return events
}

func (engine *Engine) bombIndexByID(bombID uint32) int {
	for index := range engine.bombs {
		if engine.bombs[index].ID == bombID {
			return index
		}
	}
	return -1
}

func (engine *Engine) blastCells(bomb Bomb, scheduled map[uint32]bool, queue *[]uint32) ([]Cell, []Event) {
	result := []Cell{bomb.Cell}
	events := engine.destroyBlastObjectsAtCell(bomb.Cell, bomb.OwnerID, bomb.ID)
	directions := [...]Cell{{Row: -1}, {Col: 1}, {Row: 1}, {Col: -1}}
	for _, direction := range directions {
		for distance := int16(1); distance <= int16(bomb.Power); distance++ {
			cell := Cell{Row: bomb.Cell.Row + direction.Row*distance, Col: bomb.Cell.Col + direction.Col*distance}
			tile, ok := engine.grid.Cell(cell)
			if !ok {
				break
			}
			if tile.Kind == CellBreakable {
				result = append(result, cell)
				index := int(cell.Row)*int(engine.grid.Width) + int(cell.Col)
				if tile.Durability > 1 {
					engine.grid.Cells[index].Durability--
					events = append(events, Event{Kind: EventCellDamaged, TimeMS: engine.elapsedMS, PlayerID: bomb.OwnerID, BombID: bomb.ID, Cell: cell, ObjectID: tile.MapElementID})
				} else {
					engine.grid.Cells[index] = Tile{Kind: CellOpen, FlamePassable: true}
					events = append(events, Event{Kind: EventCellDestroyed, TimeMS: engine.elapsedMS, PlayerID: bomb.OwnerID, BombID: bomb.ID, Cell: cell, ObjectID: tile.MapElementID})
					if event, revealed := engine.revealPickupAt(cell, bomb.OwnerID, bomb.ID); revealed {
						events = append(events, event)
					}
				}
				break
			}
			if !tile.FlamePassable {
				break
			}
			result = append(result, cell)
			events = append(events, engine.destroyBlastObjectsAtCell(cell, bomb.OwnerID, bomb.ID)...)
			otherBomb := false
			for otherIndex := range engine.bombs {
				otherBombObject := &engine.bombs[otherIndex]
				otherID := otherBombObject.ID
				if otherBombObject.Cell != cell || otherID == bomb.ID {
					continue
				}
				otherBomb = true
				// Native flame contact reduces the remaining fuse to an immediate
				// value even during flight, but state 3 still blocks explosion until
				// landing. Preserve that two-clock behavior instead of exploding a
				// thrown bomb in mid-air.
				if otherBombObject.FlightUntilMS > engine.elapsedMS {
					if otherBombObject.ExplodeAtMS > engine.elapsedMS {
						otherBombObject.ExplodeAtMS = engine.elapsedMS
					}
					continue
				}
				if !scheduled[otherID] {
					scheduled[otherID] = true
					*queue = append(*queue, otherID)
				}
			}
			if otherBomb {
				break
			}
		}
	}
	return result, events
}

func (engine *Engine) addFlame(cell Cell, ownerID uint16) {
	expires := saturatingAdd(engine.elapsedMS, engine.rules.FlameDurationMS)
	for index := range engine.flames {
		if engine.flames[index].Cell == cell {
			engine.flames[index].ImpactAtMS = engine.elapsedMS
			if engine.flames[index].ExpiresAtMS < expires {
				engine.flames[index].ExpiresAtMS = expires
			}
			engine.flames[index].OwnerID = ownerID
			return
		}
	}
	engine.flames = append(engine.flames, Flame{Cell: cell, OwnerID: ownerID, ImpactAtMS: engine.elapsedMS, ExpiresAtMS: expires})
	sort.Slice(engine.flames, func(i, j int) bool {
		if engine.flames[i].Cell.Row != engine.flames[j].Cell.Row {
			return engine.flames[i].Cell.Row < engine.flames[j].Cell.Row
		}
		return engine.flames[i].Cell.Col < engine.flames[j].Cell.Col
	})
}

func (engine *Engine) expireFlames() {
	kept := engine.flames[:0]
	for _, flame := range engine.flames {
		if flame.ExpiresAtMS > engine.elapsedMS {
			kept = append(kept, flame)
		}
	}
	engine.flames = kept
}
