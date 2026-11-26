package confirm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/cache"
	"github.com/amg-rfid/amg-rfid-gateway/internal/httpclient"
	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
)

func newStore(t *testing.T) *localstore.LocalStore {
	t.Helper()
	c, err := cache.NewSQLite(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return localstore.New(c.GetDB())
}

func link(t *testing.T, store *localstore.LocalStore, uii string) {
	t.Helper()
	k := localstore.Kanban{Code: "K1", Description: "Burlete", StandardPack: 10, Unit: "PZ"}
	item := localstore.SyncDataItem{ID: 1, ToolID: 1, UII: uii, SKU: "S", Name: "N", Active: true, Kanban: &k}
	if err := store.ApplyKanbanLinks([]localstore.SyncDataItem{item}); err != nil {
		t.Fatalf("link: %v", err)
	}
}

type fakeVPS struct {
	*httptest.Server
	confirms atomic.Int32
	last     atomic.Value
	// during, when set, runs inside each confirmation POST before it is
	// answered.
	during func()
}

func newFakeVPS(t *testing.T, status int) *fakeVPS {
	t.Helper()
	f := &fakeVPS{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		var sent map[string]any
		json.NewDecoder(r.Body).Decode(&sent)
		f.last.Store(sent)
		f.confirms.Add(1)
		if f.during != nil {
			f.during()
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(`{"status":"OK"}`))
	}))
	t.Cleanup(f.Close)
	return f
}

type row struct {
	id, antenna, occurredAt string
	synced, auto            bool
}

func rows(t *testing.T, store *localstore.LocalStore, uii string) []row {
	t.Helper()
	entries, err := store.ListConfirmationsBetween(time.Now().Add(-48*time.Hour), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var out []row
	for _, e := range entries {
		if e.UII == uii {
			out = append(out, row{antenna: e.AntennaID, synced: e.Synced, auto: e.Auto})
		}
	}
	return out
}

func TestConfirm_OnlineSendsGivenInstantAndRecordsAutoSynced(t *testing.T) {
	store := newStore(t)
	link(t, store, "E1")
	vps := newFakeVPS(t, http.StatusOK)
	svc := &Service{Store: store, VPS: httpclient.NewVPSClient(vps.URL, 2*time.Second, httpclient.StaticToken("tok")), CompanyID: "c"}
	at := time.Date(2026, 11, 26, 10, 0, 0, 0, time.FixedZone("x", -6*3600))

	res, err := svc.Confirm(context.Background(), Request{UII: "E1", Action: "salida", AntennaID: "ant-1", OccurredAt: at, Auto: true})
	if err != nil || res.Mode != ModeOnline {
		t.Fatalf("Confirm = %+v, %v; want online", res, err)
	}
	sent, _ := vps.last.Load().(map[string]any)
	if sent["confirmation_id"] != res.ConfirmationID || sent["occurred_at"] != at.UTC().Format(time.RFC3339Nano) || sent["antenna_id"] != "ant-1" {
		t.Errorf("sent %v, result id %s", sent, res.ConfirmationID)
	}
	entries, _ := store.ListConfirmationsBetween(at.Add(-time.Hour), at.Add(time.Hour))
	if len(entries) != 1 || !entries[0].Synced || !entries[0].Auto || entries[0].Action != "salida" {
		t.Errorf("recorded %+v, want one synced auto salida", entries)
	}
}

func TestConfirm_OfflineQueuesWithAutoFlag(t *testing.T) {
	store := newStore(t)
	link(t, store, "E1")
	svc := &Service{Store: store, CompanyID: "c"}

	res, err := svc.Confirm(context.Background(), Request{UII: "E1", Action: "entrada", Auto: true})
	if err != nil || res.Mode != ModeOffline || res.ConfirmationID == "" {
		t.Fatalf("Confirm = %+v, %v; want offline with id", res, err)
	}
	queue, _ := store.GetUnsyncedConfirmations(10)
	if len(queue) != 1 || queue[0].ConfirmationID != res.ConfirmationID || queue[0].AntennaID != "manual" {
		t.Errorf("queue = %+v", queue)
	}
	if r := rows(t, store, "E1"); len(r) != 1 || !r[0].auto {
		t.Errorf("rows = %+v, want one auto row", r)
	}
}

func TestConfirm_RejectsTagWithoutKanban(t *testing.T) {
	store := newStore(t)
	svc := &Service{Store: store}
	if _, err := svc.Confirm(context.Background(), Request{UII: "E9", Action: "entrada"}); !errors.Is(err, ErrNotAssigned) {
		t.Errorf("err = %v, want ErrNotAssigned", err)
	}
	if q, _ := store.GetUnsyncedConfirmations(10); len(q) != 0 {
		t.Errorf("queued %d, want 0", len(q))
	}
}

func TestConfirm_NoStoreCannotVerify(t *testing.T) {
	svc := &Service{}
	if _, err := svc.Confirm(context.Background(), Request{UII: "E1", Action: "entrada"}); !errors.Is(err, ErrCannotVerify) {
		t.Errorf("err = %v, want ErrCannotVerify", err)
	}
}

func TestConfirm_QueueFull(t *testing.T) {
	store := newStore(t)
	link(t, store, "E1")
	svc := &Service{Store: store, MaxPending: 1}
	if _, err := svc.Confirm(context.Background(), Request{UII: "E1", Action: "entrada"}); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := svc.Confirm(context.Background(), Request{UII: "E1", Action: "salida"}); !errors.Is(err, localstore.ErrQueueFull) {
		t.Errorf("err = %v, want ErrQueueFull", err)
	}
}

// fakeClaimer records manual claims and refuses them unless configured.
type fakeClaimer struct {
	manual bool
	calls  []string
}

func (f *fakeClaimer) ClaimManual(uii string) bool {
	f.calls = append(f.calls, "manual:"+uii)
	return f.manual
}

// errClaimer refuses manual claims with a reason.
type errClaimer struct{ err error }

func (c errClaimer) ClaimManual(string) bool     { return c.err == nil }
func (c errClaimer) ClaimManualErr(string) error { return c.err }

func TestConfirm_RefusedClaimWritesNothing(t *testing.T) {
	store := newStore(t)
	link(t, store, "E1")
	vps := newFakeVPS(t, http.StatusOK)
	c := &fakeClaimer{}
	svc := &Service{Store: store, VPS: httpclient.NewVPSClient(vps.URL, 2*time.Second, httpclient.StaticToken("tok")), CompanyID: "c", Claimer: c}

	if _, err := svc.Confirm(context.Background(), Request{UII: "E1", Action: "salida"}); !errors.Is(err, ErrAutoInProgress) {
		t.Errorf("manual err = %v, want ErrAutoInProgress", err)
	}
	svc.Claimer = errClaimer{ErrAlreadyAutoConfirmed}
	if _, err := svc.Confirm(context.Background(), Request{UII: "E1", Action: "salida"}); !errors.Is(err, ErrAlreadyAutoConfirmed) {
		t.Errorf("manual err = %v, want ErrAlreadyAutoConfirmed", err)
	}
	if n := vps.confirms.Load(); n != 0 {
		t.Errorf("VPS confirms = %d, want 0", n)
	}
	if q, _ := store.GetUnsyncedConfirmations(10); len(q) != 0 {
		t.Errorf("queue = %+v, want none", q)
	}
	if got := strings.Join(c.calls, ","); got != "manual:E1" {
		t.Errorf("calls = %s", got)
	}
}

func TestConfirm_AutoRequestIsNotClaimedAgain(t *testing.T) {
	store := newStore(t)
	link(t, store, "E1")
	c := &fakeClaimer{}
	svc := &Service{Store: store, CompanyID: "c", Claimer: c}

	if _, err := svc.Confirm(context.Background(), Request{UII: "E1", Action: "salida", Auto: true, PassID: 1}); err != nil {
		t.Fatalf("auto: %v", err)
	}
	if len(c.calls) != 0 {
		t.Errorf("calls = %v, want none: the auto-confirmer claims", c.calls)
	}
}

func TestConfirm_UnassignedTagIsNotClaimed(t *testing.T) {
	c := &fakeClaimer{manual: true}
	svc := &Service{Store: newStore(t), Claimer: c}
	if _, err := svc.Confirm(context.Background(), Request{UII: "E9", Action: "entrada"}); !errors.Is(err, ErrNotAssigned) {
		t.Fatalf("err = %v", err)
	}
	if len(c.calls) != 0 {
		t.Errorf("calls = %v, want none", c.calls)
	}
}

func onlineService(t *testing.T, status int) (*Service, *localstore.LocalStore, *fakeVPS) {
	t.Helper()
	store := newStore(t)
	link(t, store, "E1")
	vps := newFakeVPS(t, status)
	svc := &Service{Store: store, VPS: httpclient.NewVPSClient(vps.URL, 2*time.Second, httpclient.StaticToken("tok")), CompanyID: "c", MaxPending: 1}
	return svc, store, vps
}

func TestConfirm_OnlineSuccessRecordsOneSyncedRowWithSentID(t *testing.T) {
	svc, store, vps := onlineService(t, http.StatusOK)
	res, err := svc.Confirm(context.Background(), Request{UII: "E1", Action: "salida"})
	if err != nil || res.Mode != ModeOnline {
		t.Fatalf("Confirm = %+v, %v; want online", res, err)
	}
	sent, _ := vps.last.Load().(map[string]any)
	if sent["confirmation_id"] != res.ConfirmationID {
		t.Errorf("sent id %v, result id %s", sent["confirmation_id"], res.ConfirmationID)
	}
	if r := rows(t, store, "E1"); len(r) != 1 || !r[0].synced {
		t.Errorf("rows = %+v, want one synced row", r)
	}
	if q, _ := store.GetUnsyncedConfirmations(10); len(q) != 0 {
		t.Errorf("unsynced = %+v, want none", q)
	}
}

func TestConfirm_OnlineFailureLeavesOneDueRowWithSentID(t *testing.T) {
	svc, store, vps := onlineService(t, http.StatusInternalServerError)
	res, err := svc.Confirm(context.Background(), Request{UII: "E1", Action: "salida"})
	if err != nil || res.Mode != ModeOffline {
		t.Fatalf("Confirm = %+v, %v; want offline", res, err)
	}
	sent, _ := vps.last.Load().(map[string]any)
	q, _ := store.GetRetryableConfirmations(10, 5)
	if len(q) != 1 || q[0].ConfirmationID != res.ConfirmationID || sent["confirmation_id"] != res.ConfirmationID {
		t.Errorf("retryable = %+v, sent %v, result id %s; want one due row with the sent id", q, sent["confirmation_id"], res.ConfirmationID)
	}
	if r := rows(t, store, "E1"); len(r) != 1 {
		t.Errorf("rows = %+v, want one", r)
	}
}

// A slow successful online send takes no queue slot: an offline confirm made
// while it is in flight, with the queue one below the cap, is still queued.
func TestConfirm_OnlineSendInFlightTakesNoQueueSlot(t *testing.T) {
	svc, store, vps := onlineService(t, http.StatusOK)
	svc.MaxPending = 2
	link(t, store, "E2")
	if _, err := store.CreateConfirmationWithIDAuto("E0", "salida", "manual", "q-1", time.Now(), 0, false); err != nil {
		t.Fatal(err)
	}
	offline := &Service{Store: store, CompanyID: "c", MaxPending: 2}
	var offlineErr error
	vps.during = func() {
		_, offlineErr = offline.Confirm(context.Background(), Request{UII: "E2", Action: "entrada"})
	}
	if _, err := svc.Confirm(context.Background(), Request{UII: "E1", Action: "salida"}); err != nil {
		t.Fatalf("online Confirm: %v", err)
	}
	if offlineErr != nil {
		t.Errorf("offline confirm during the send: %v, want queued", offlineErr)
	}
}
