package battleenv

import (
	"reflect"
	"testing"

	"qqtang/internal/game/battleengine"
)

func TestEncodeActorMatchesBatchTensor(t *testing.T) {
	batch, err := NewBatch(testBatchConfig(1))
	if err != nil {
		t.Fatal(err)
	}
	batched, err := batch.Observe()
	if err != nil {
		t.Fatal(err)
	}
	episode := &batch.episodes[0]
	playerID := episode.playerIDs[0]
	observation, err := episode.engine.Observation(playerID)
	if err != nil {
		t.Fatal(err)
	}
	danger, err := episode.engine.DangerTimeline(batch.config.DangerHorizonMS)
	if err != nil {
		t.Fatal(err)
	}
	legal, err := episode.engine.LegalActionMask(playerID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeActor(observation, danger, legal, batch.maxHeight, batch.maxWidth)
	if err != nil {
		t.Fatal(err)
	}
	spatialCount := SpatialChannels * batch.maxHeight * batch.maxWidth
	if !reflect.DeepEqual(encoded.Spatial, batched.Spatial[:spatialCount]) {
		t.Fatal("single actor spatial encoding differs from batch encoding")
	}
	if !reflect.DeepEqual(encoded.ScalarValues, batched.Scalars[:ScalarFeatures]) {
		t.Fatal("single actor scalar encoding differs from batch encoding")
	}
	if !reflect.DeepEqual(encoded.Legal, batched.Legal[:int(battleengine.DiscreteActionCount)]) {
		t.Fatal("single actor legal encoding differs from batch encoding")
	}
}
