package sync

import (
	"context"
	"errors"
	stdsync "sync"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
)

// kanbanVPSClient adds the optional kanban catalog call to the base mock.
type kanbanVPSClient struct {
	mockVPSClient
	fetchKanbansFunc func(companyID string) ([]localstore.Kanban, error)
}

func (m *kanbanVPSClient) FetchKanbans(companyID string) ([]localstore.Kanban, error) {
	return m.fetchKanbansFunc(companyID)
}

// kanbanLocalStore adds the optional kanban cache calls to the base mock.
type kanbanLocalStore struct {
	mockLocalStore
	mu       stdsync.Mutex
	links    [][]localstore.SyncDataItem
	catalogs [][]localstore.Kanban
	linkErr  error
}

func (m *kanbanLocalStore) ApplyKanbanLinks(items []localstore.SyncDataItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.links = append(m.links, items)
	return m.linkErr
}

func (m *kanbanLocalStore) ReplaceKanbanCatalog(kanbans []localstore.Kanban) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.catalogs = append(m.catalogs, kanbans)
	return nil
}

func (m *kanbanLocalStore) snapshot() ([][]localstore.SyncDataItem, [][]localstore.Kanban) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([][]localstore.SyncDataItem(nil), m.links...), append([][]localstore.Kanban(nil), m.catalogs...)
}

func runOneSync(t *testing.T, vps VPSClient, store LocalStore) *ToolsSync {
	t.Helper()
	ts := NewToolsSync(vps, store, "company123", 1*time.Hour, 1*time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := ts.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(ts.Stop)
	return ts
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func TestToolsSync_KanbanLinksAndCatalogCachedOnSync(t *testing.T) {
	k := localstore.Kanban{Code: "EXP758SF M16", Description: "Burlete", StandardPack: 250, Unit: "PZ", Zones: []string{"C", "A"}}
	vps := &kanbanVPSClient{
		mockVPSClient: mockVPSClient{
			fetchSyncDataFunc: func(companyID string) (*SyncDataResponse, error) {
				return &SyncDataResponse{Status: "OK", Tools: []localstore.SyncDataItem{
					{ID: 1, ToolID: 1, UII: "A001", SKU: "S1", Name: "N1", Active: true, Kanban: &k},
					{ID: 2, ToolID: 2, UII: "A002", SKU: "S2", Name: "N2", Active: true},
				}}, nil
			},
		},
		fetchKanbansFunc: func(companyID string) ([]localstore.Kanban, error) {
			if companyID != "company123" {
				t.Errorf("expected company123, got %s", companyID)
			}
			return []localstore.Kanban{k, {Code: "TOY-1"}}, nil
		},
	}
	store := &kanbanLocalStore{}

	ts := runOneSync(t, vps, store)
	waitFor(t, "kanban catalog cached", func() bool { _, c := store.snapshot(); return len(c) == 1 })

	links, catalogs := store.snapshot()
	if len(links) != 1 || len(links[0]) != 2 {
		t.Fatalf("expected ApplyKanbanLinks once with both items, got %+v", links)
	}
	if links[0][0].Kanban == nil || links[0][0].Kanban.Code != "EXP758SF M16" || links[0][1].Kanban != nil {
		t.Errorf("expected kanban passed through untouched, got %+v / %+v", links[0][0].Kanban, links[0][1].Kanban)
	}
	if len(catalogs[0]) != 2 || catalogs[0][1].Code != "TOY-1" {
		t.Errorf("expected catalog of 2 kanbans, got %+v", catalogs[0])
	}
	if !ts.IsVPSOnline() {
		t.Error("expected VPS online after sync")
	}
}

// The cloud endpoint may not exist yet (404) or fail: tools/users sync must still succeed.
func TestToolsSync_KanbanCatalogFailureDoesNotFailSync(t *testing.T) {
	vps := &kanbanVPSClient{
		mockVPSClient: mockVPSClient{
			fetchSyncDataFunc: func(companyID string) (*SyncDataResponse, error) {
				return &SyncDataResponse{Status: "OK", Tools: []localstore.SyncDataItem{
					{ID: 1, ToolID: 1, UII: "A001", SKU: "S1", Name: "N1", Active: true},
				}}, nil
			},
		},
		fetchKanbansFunc: func(companyID string) ([]localstore.Kanban, error) {
			return nil, errors.New("unexpected status code: 404")
		},
	}
	tagsUpserted := make(chan struct{}, 1)
	store := &kanbanLocalStore{mockLocalStore: mockLocalStore{
		toolTagsUpsertedFunc: func(tags []localstore.ToolTagRecord) error { tagsUpserted <- struct{}{}; return nil },
	}}

	ts := runOneSync(t, vps, store)
	waitFor(t, "VPS online", ts.IsVPSOnline)

	select {
	case <-tagsUpserted:
	default:
		t.Error("expected tool tags upserted despite catalog failure")
	}
	links, catalogs := store.snapshot()
	if len(links) != 1 {
		t.Errorf("expected links still applied, got %d calls", len(links))
	}
	if len(catalogs) != 0 {
		t.Errorf("expected no catalog replace when the fetch failed, got %d", len(catalogs))
	}
}

// Backward compatibility: a store/client without kanban support behaves exactly as before.
func TestToolsSync_WithoutKanbanSupportBehavesAsBefore(t *testing.T) {
	vps := &mockVPSClient{
		fetchSyncDataFunc: func(companyID string) (*SyncDataResponse, error) {
			return &SyncDataResponse{Status: "OK", Tools: []localstore.SyncDataItem{
				{ID: 1, ToolID: 1, UII: "A001", SKU: "S1", Name: "N1", Active: true},
			}}, nil
		},
	}
	tagsUpserted := make(chan []localstore.ToolTagRecord, 1)
	store := &mockLocalStore{
		toolTagsUpsertedFunc: func(tags []localstore.ToolTagRecord) error { tagsUpserted <- tags; return nil },
	}

	ts := runOneSync(t, vps, store)
	waitFor(t, "VPS online", ts.IsVPSOnline)

	tags := <-tagsUpserted
	if len(tags) != 1 || tags[0].UII != "A001" || !tags[0].Active {
		t.Errorf("expected the tag upserted as before, got %+v", tags)
	}
}

func TestToolsSync_LinkFailureDoesNotMarkVPSOfflineOrSkipUsers(t *testing.T) {
	vps := &kanbanVPSClient{
		mockVPSClient: mockVPSClient{
			fetchSyncDataFunc: func(companyID string) (*SyncDataResponse, error) {
				return &SyncDataResponse{Status: "OK", Tools: []localstore.SyncDataItem{
					{ID: 1, ToolID: 1, UII: "A001", SKU: "S1", Name: "N1", Active: true},
				}}, nil
			},
		},
		fetchKanbansFunc: func(companyID string) ([]localstore.Kanban, error) { return nil, nil },
	}
	usersUpserted := make(chan struct{}, 1)
	store := &kanbanLocalStore{
		mockLocalStore: mockLocalStore{upsertUsersFunc: func(users []localstore.User) error { usersUpserted <- struct{}{}; return nil }},
		linkErr:        errors.New("disk full"),
	}

	ts := runOneSync(t, vps, store)
	waitFor(t, "VPS online", ts.IsVPSOnline)
	select {
	case <-usersUpserted:
	default:
		t.Error("expected users upserted even if kanban links failed")
	}
}
