package rag

import "testing"

func TestHNSWSettingsDefaultsAndNormalization(t *testing.T) {
	settings := DefaultSettings("p1")
	if settings.HNSWM != DefaultHNSWM ||
		settings.HNSWEfConstruction != DefaultHNSWEfConstruction ||
		settings.HNSWEfSearch != DefaultHNSWEfSearch ||
		settings.HNSWTombstoneRebuildRatio != DefaultHNSWTombstoneRebuildRatio ||
		settings.HNSWMemoryLimitMB != DefaultHNSWMemoryLimitMB {
		t.Fatalf("unexpected defaults: %#v", settings)
	}
	if err := settings.Validate(); err != nil {
		t.Fatalf("defaults must validate: %v", err)
	}

	var legacy Settings
	legacy.ProjectID = "p1"
	legacy.Normalize()
	if legacy.HNSWM != DefaultHNSWM || legacy.HNSWEfSearch != DefaultHNSWEfSearch {
		t.Fatalf("legacy settings not normalized: %#v", legacy)
	}
}

func TestHNSWSettingsRejectOutOfBounds(t *testing.T) {
	settings := DefaultSettings("p1")
	settings.HNSWM = MaxHNSWM + 1
	if err := settings.Validate(); err == nil {
		t.Fatal("expected hnsw_m validation error")
	}

	settings = DefaultSettings("p1")
	settings.HNSWEfConstruction = settings.HNSWM - 1
	if err := settings.Validate(); err == nil {
		t.Fatal("expected efConstruction below M to be rejected")
	}

	settings = DefaultSettings("p1")
	settings.HNSWTombstoneRebuildRatio = 0.9
	if err := settings.Validate(); err == nil {
		t.Fatal("expected tombstone ratio validation error")
	}
}
