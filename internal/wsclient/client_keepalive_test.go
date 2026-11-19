package wsclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-shared-go/models"
	"github.com/gorilla/websocket"
)

const welcome = `{"type":"SESSION_READY","payload":{"message":"ready","sessionId":"s-1"}}`

// keepaliveServer upgrades, sends a welcome event like the backend, then runs serve.
func keepaliveServer(t *testing.T, serve func(conn *websocket.Conn)) string {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if err := conn.WriteMessage(websocket.TextMessage, []byte(welcome)); err != nil {
			return
		}
		serve(conn)
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func waitFor(t *testing.T, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func TestClient_AnswersServerPingsAndStaysConnected(t *testing.T) {
	var pongs atomic.Int32
	url := keepaliveServer(t, func(conn *websocket.Conn) {
		// Like the backend: a read deadline extended only by pongs.
		_ = conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
		conn.SetPongHandler(func(string) error {
			pongs.Add(1)
			return conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
		})
		go func() {
			for i := 0; i < 8; i++ {
				time.Sleep(50 * time.Millisecond)
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second)); err != nil {
					return
				}
			}
		}()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})

	client := NewClient(url, StaticToken("tok"))
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()

	time.Sleep(450 * time.Millisecond) // three pong windows
	if got := pongs.Load(); got < 5 {
		t.Fatalf("pongs = %d; want the client to answer every ping", got)
	}
	if !client.IsConnected() {
		t.Fatal("IsConnected() = false while the server keeps the connection open")
	}
}

func TestClient_IsConnectedFalseAfterServerCloses(t *testing.T) {
	url := keepaliveServer(t, func(conn *websocket.Conn) {
		time.Sleep(50 * time.Millisecond)
		_ = conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseGoingAway, "bye"), time.Now().Add(time.Second))
	})

	client := NewClient(url, StaticToken("tok"))
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()

	if !waitFor(t, func() bool { return !client.IsConnected() }) {
		t.Fatal("IsConnected() still true after the server closed the connection")
	}
}

func TestSendSyncRequest_SkipsServerEvents(t *testing.T) {
	url := keepaliveServer(t, func(conn *websocket.Conn) {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var req models.SyncRequest
			if json.Unmarshal(msg, &req) != nil {
				continue
			}
			resp, _ := json.Marshal(models.SyncResponse{Success: true, Accepted: len(req.Readings)})
			_ = conn.WriteMessage(websocket.TextMessage, resp)
		}
	})

	client := NewClient(url, StaticToken("tok"))
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()
	time.Sleep(50 * time.Millisecond) // let the welcome event arrive first

	req := models.SyncRequest{
		GatewayID: "gw-1",
		CompanyID: "amg",
		Readings: []models.Reading{
			{EPC: "E2801160600002", AntennaID: "ant-1", RSSI: -50, Timestamp: time.Now(), GatewayID: "gw-1"},
			{EPC: "E2801160600003", AntennaID: "ant-1", RSSI: -51, Timestamp: time.Now(), GatewayID: "gw-1"},
		},
		Timestamp: time.Now(),
	}
	resp, err := client.SendSyncRequest(req)
	if err != nil {
		t.Fatalf("SendSyncRequest: %v", err)
	}
	if !resp.Success || resp.Accepted != 2 {
		t.Fatalf("response = %+v; want the sync response, not the welcome event", resp)
	}
}
