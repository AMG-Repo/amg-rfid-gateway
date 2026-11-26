package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/amg-rfid/amg-rfid-gateway/internal/events"
)

// syncPendingText is the chip shown for a confirmation still queued for the VPS.
const syncPendingText = "Guardada en el gateway · se enviará al VPS"

var assetRef = regexp.MustCompile(`(?:src|href)="([^"]+)"`)

func fetchBody(t *testing.T, srv *httptest.Server, path string) (int, string) {
	t.Helper()
	res, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return res.StatusCode, string(body)
}

// TestPanelServesLanesDesign checks that "/" serves the lanes panel (design 09),
// that every asset it references is embedded, and that nothing loads from a
// third-party host (the gateway runs on a plant LAN, possibly without internet).
func TestPanelServesLanesDesign(t *testing.T) {
	bus := events.NewEventBus(10)
	defer bus.Close()
	srv := httptest.NewServer(NewServer("127.0.0.1", 0, bus, nil, nil, nil, "test-company").routes())
	defer srv.Close()

	code, index := fetchBody(t, srv, "/")
	if code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", code)
	}
	for _, want := range []string{
		"Por confirmar",
		"Estado del sistema",
		"Tema de color",
		"Historial de hoy",
		"Modo de confirmación",
		"Automático",
		`dataset.theme`,
		`amg-theme`,
	} {
		if !strings.Contains(index, want) {
			t.Errorf("index.html missing %q", want)
		}
	}
	if strings.Contains(index, "htmx") {
		t.Error("index.html still references htmx")
	}

	refs := assetRef.FindAllStringSubmatch(index, -1)
	if len(refs) == 0 {
		t.Fatal("index.html references no assets")
	}
	assets := map[string]string{}
	for _, m := range refs {
		ref := m[1]
		if strings.Contains(ref, "://") || strings.HasPrefix(ref, "//") {
			t.Errorf("external asset %q: the panel must not load from other hosts", ref)
			continue
		}
		code, body := fetchBody(t, srv, ref)
		if code != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", ref, code)
		}
		assets[ref] = body
	}
	for _, ref := range []string{"/style.css", "/app.js"} {
		if _, ok := assets[ref]; !ok {
			t.Errorf("index.html does not reference %s", ref)
		}
	}

	js := assets["/app.js"]
	for _, want := range []string{
		"Entradas al almacén",
		"Salidas del almacén",
		"Requieren tu atención",
		"amgWebAuthToken",
		"/api/confirm",
		"/api/tags/assign",
		"/api/confirmations/requeue",
		"Reintentar",
		"/api/auto-confirm",
		"auto_confirmed",
		"confirmada automáticamente",
		syncPendingText,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js missing %q", want)
		}
	}
	// Reads are rendered with text nodes only; markup strings would reopen XSS.
	if strings.Contains(js, "innerHTML") {
		t.Error("app.js must not use innerHTML")
	}
	if strings.Contains(js, "window.prompt") {
		t.Error("app.js must use the inline token form, not window.prompt")
	}
}
