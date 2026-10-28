package antenna

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/chainway"
	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
)

func sentCommands(c *mockClient) [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([][]byte(nil), c.commandsSent...)
}

func countFrame(cmds [][]byte, frame []byte) int {
	n := 0
	for _, c := range cmds {
		if bytes.Equal(c, frame) {
			n++
		}
	}
	return n
}

func newChainwayLifecycleManager(t *testing.T) (*AntennaManager, *mockClient) {
	t.Helper()
	manager, client, _, _ := newTestAntennaManager()
	manager.config.Protocol = config.ProtocolChainway
	// Make the generic heartbeat fire fast so its absence is observable.
	manager.gatewayConfig.HeartbeatInterval = 20 * time.Millisecond
	manager.gatewayConfig.HeartbeatSilenceThreshold = 10 * time.Millisecond
	return manager, client
}

func waitDone(t *testing.T, m *AntennaManager) {
	t.Helper()
	select {
	case <-m.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("manager did not finish")
	}
}

func TestChainwayStartSendsOnlyStartInventory(t *testing.T) {
	manager, client := newChainwayLifecycleManager(t)
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Past the generic 2s ReadUII delay and many heartbeat intervals.
	time.Sleep(2200 * time.Millisecond)

	cmds := sentCommands(client)
	if len(cmds) != 1 || !bytes.Equal(cmds[0], chainway.StartInventoryFrame()) {
		t.Fatalf("commands = % X, want only start-inventory", cmds)
	}
	_ = manager.Stop()
}

func TestChainwayStopSendsStopInventoryOnce(t *testing.T) {
	manager, client := newChainwayLifecycleManager(t)
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)

	if err := manager.Stop(); err != nil {
		t.Fatal(err)
	}
	_ = manager.Stop()

	cmds := sentCommands(client)
	if countFrame(cmds, chainway.StopInventoryFrame()) != 1 || !bytes.Equal(cmds[len(cmds)-1], chainway.StopInventoryFrame()) {
		t.Fatalf("commands = % X, want stop-inventory sent once, last", cmds)
	}
}

func TestChainwayParentCancelSendsStopBeforeDone(t *testing.T) {
	manager, client := newChainwayLifecycleManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)

	cancel()
	waitDone(t, manager)

	// Done is what main.go waits on before Disconnect: stop must already be sent.
	if countFrame(sentCommands(client), chainway.StopInventoryFrame()) != 1 {
		t.Fatalf("commands = % X, want stop-inventory before Done", sentCommands(client))
	}
}

func TestChainwayStartWriteErrorEndsSession(t *testing.T) {
	manager, client := newChainwayLifecycleManager(t)
	client.writeError = errors.New("broken pipe")
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitDone(t, manager)
}

func TestChainwayConnectionCloseEndsSession(t *testing.T) {
	manager, client := newChainwayLifecycleManager(t)
	local, remote := net.Pipe()
	manager.SetConn(local)
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)

	remote.Close()
	waitDone(t, manager)
	_ = manager.Stop()

	if countFrame(sentCommands(client), chainway.StartInventoryFrame()) != 1 {
		t.Fatalf("commands = % X, want start-inventory once", sentCommands(client))
	}
}

func TestGenericStopSendsNoChainwayFrames(t *testing.T) {
	manager, client, _, _ := newTestAntennaManager()
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := manager.Stop(); err != nil {
		t.Fatal(err)
	}

	cmds := sentCommands(client)
	if countFrame(cmds, chainway.StartInventoryFrame())+countFrame(cmds, chainway.StopInventoryFrame()) != 0 {
		t.Fatalf("generic sent chainway frames: % X", cmds)
	}
}
