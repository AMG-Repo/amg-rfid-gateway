package localstore

import (
	"testing"
	"time"
)

func TestUpsertUnassignedTag_FirstReadCreatesRow(t *testing.T) {
	store := setupKanbanStore(t)
	seen := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)

	if err := store.UpsertUnassignedTag("A000000000000003", "ant-1", seen, 1); err != nil {
		t.Fatalf("UpsertUnassignedTag failed: %v", err)
	}

	tags, err := store.ListUnassignedTags()
	if err != nil {
		t.Fatalf("ListUnassignedTags failed: %v", err)
	}
	if len(tags) != 1 {
		t.Fatalf("expected 1 unassigned tag, got %d", len(tags))
	}
	got := tags[0]
	if got.EPC != "A000000000000003" || got.AntennaID != "ant-1" || got.ReadCount != 1 ||
		!got.FirstSeen.Equal(seen) || !got.LastSeen.Equal(seen) {
		t.Errorf("unexpected row: %+v", got)
	}
}

func TestUpsertUnassignedTag_RepeatReadsUpdateInPlace(t *testing.T) {
	store := setupKanbanStore(t)
	first := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	later := first.Add(5 * time.Minute)

	if err := store.UpsertUnassignedTag("A04", "ant-1", first, 2); err != nil {
		t.Fatalf("first upsert failed: %v", err)
	}
	if err := store.UpsertUnassignedTag("A04", "ant-2", later, 3); err != nil {
		t.Fatalf("second upsert failed: %v", err)
	}

	tags, _ := store.ListUnassignedTags()
	if len(tags) != 1 {
		t.Fatalf("expected a single row per EPC, got %d", len(tags))
	}
	got := tags[0]
	if got.ReadCount != 5 {
		t.Errorf("expected read_count 5, got %d", got.ReadCount)
	}
	if !got.FirstSeen.Equal(first) || !got.LastSeen.Equal(later) {
		t.Errorf("expected first_seen kept and last_seen advanced, got first=%v last=%v", got.FirstSeen, got.LastSeen)
	}
	if got.AntennaID != "ant-2" {
		t.Errorf("expected latest antenna ant-2, got %s", got.AntennaID)
	}
}

func TestListUnassignedTags_MostRecentFirstAndNeverNil(t *testing.T) {
	store := setupKanbanStore(t)

	empty, err := store.ListUnassignedTags()
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("expected empty non-nil list, got (%v, %v)", empty, err)
	}

	base := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	store.UpsertUnassignedTag("OLD", "a", base, 1)
	store.UpsertUnassignedTag("NEW", "a", base.Add(time.Minute), 1)

	tags, _ := store.ListUnassignedTags()
	if len(tags) != 2 || tags[0].EPC != "NEW" || tags[1].EPC != "OLD" {
		t.Errorf("expected NEW before OLD, got %+v", tags)
	}
}

func TestListUnassignedTags_HidesTagsLinkedByLaterSync(t *testing.T) {
	store := setupKanbanStore(t)
	k := fordKanban()
	store.UpsertUnassignedTag("A0000001", "a", time.Now(), 1)
	store.UpsertUnassignedTag("A0000002", "a", time.Now(), 1)

	// Another operator/gateway assigned A0000001 on the VPS; the next sync links it.
	if err := store.ApplyKanbanLinks([]SyncDataItem{syncItem(1, "A0000001", true, &k)}); err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	tags, _ := store.ListUnassignedTags()
	if len(tags) != 1 || tags[0].EPC != "A0000002" {
		t.Errorf("expected only A0000002 to remain unassigned, got %+v", tags)
	}
}

func TestDeleteUnassignedTag(t *testing.T) {
	store := setupKanbanStore(t)
	store.UpsertUnassignedTag("A1", "a", time.Now(), 1)
	store.UpsertUnassignedTag("A2", "a", time.Now(), 1)

	if err := store.DeleteUnassignedTag("A1"); err != nil {
		t.Fatalf("DeleteUnassignedTag failed: %v", err)
	}
	if err := store.DeleteUnassignedTag("MISSING"); err != nil {
		t.Errorf("deleting a missing EPC must not fail, got %v", err)
	}

	tags, _ := store.ListUnassignedTags()
	if len(tags) != 1 || tags[0].EPC != "A2" {
		t.Errorf("expected only A2 left, got %+v", tags)
	}
}

func TestPurgeUnassignedTagsBefore_DropsOnlyStaleRows(t *testing.T) {
	store := setupKanbanStore(t)
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	store.UpsertUnassignedTag("A000000000000003", "ant-1", now.Add(-25*time.Hour), 1)
	store.UpsertUnassignedTag("A000000000000004", "ant-1", now.Add(-23*time.Hour), 1)

	removed, err := store.PurgeUnassignedTagsBefore(now.Add(-UnassignedTagTTL))
	if err != nil {
		t.Fatalf("PurgeUnassignedTagsBefore failed: %v", err)
	}
	if removed != 1 {
		t.Errorf("expected 1 stale row removed, got %d", removed)
	}
	tags, _ := store.ListUnassignedTags()
	if len(tags) != 1 || tags[0].EPC != "A000000000000004" {
		t.Errorf("expected only the recent tag to remain, got %+v", tags)
	}
}

func TestUnassignedTagTTLIs24Hours(t *testing.T) {
	if UnassignedTagTTL != 24*time.Hour {
		t.Errorf("UnassignedTagTTL = %v, want 24h", UnassignedTagTTL)
	}
}
