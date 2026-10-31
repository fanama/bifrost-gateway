package config

import (
	"errors"
	"testing"

	"bridge-gateway/domain/errs"
)

func TestMarkActiveKeepsOneActivePerProject(t *testing.T) {
	items := []ChatConfig{
		{ID: "a", ProjectID: "p1", Active: true},
		{ID: "b", ProjectID: "p1"},
		{ID: "c", ProjectID: "p2", Active: true},
	}

	if err := MarkActive(items, "p1", "b"); err != nil {
		t.Fatalf("MarkActive: %v", err)
	}
	if items[0].Active || !items[1].Active {
		t.Errorf("p1 selection = [%v %v], want [false true]", items[0].Active, items[1].Active)
	}
	if !items[2].Active {
		t.Error("another project's active configuration must stay untouched")
	}
}

func TestMarkActiveRejectsUnknownIDWithoutMutating(t *testing.T) {
	items := []ChatConfig{
		{ID: "a", ProjectID: "p1", Active: true},
		{ID: "b", ProjectID: "p1"},
	}

	err := MarkActive(items, "p1", "absent")
	if !errors.Is(err, errs.ErrConfigNotFound) {
		t.Fatalf("MarkActive = %v, want ErrConfigNotFound", err)
	}
	if !items[0].Active || items[1].Active {
		t.Errorf("failed MarkActive must not change the selection, got [%v %v]", items[0].Active, items[1].Active)
	}
}

func TestMarkActiveRejectsConfigurationFromAnotherProject(t *testing.T) {
	items := []ChatConfig{
		{ID: "a", ProjectID: "p1", Active: true},
		{ID: "b", ProjectID: "p2", Active: true},
	}

	if err := MarkActive(items, "p1", "b"); !errors.Is(err, errs.ErrConfigNotFound) {
		t.Fatalf("MarkActive = %v, want ErrConfigNotFound", err)
	}
	if !items[0].Active || !items[1].Active {
		t.Errorf("cross-project activation must leave both selections untouched")
	}
}
