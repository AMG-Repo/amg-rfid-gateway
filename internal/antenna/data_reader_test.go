package antenna

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

// mockConn is a mock net.Conn for testing
type mockConn struct {
	readData  [][]byte
	readIndex int
	readError error
	writeData []byte
	closed    bool
}

func (m *mockConn) Read(p []byte) (n int, err error) {
	if m.readError != nil {
		return 0, m.readError
	}
	if m.readIndex >= len(m.readData) {
		return 0, io.EOF
	}
	data := m.readData[m.readIndex]
	m.readIndex++
	n = copy(p, data)
	return n, nil
}

func (m *mockConn) Write(p []byte) (n int, err error) {
	m.writeData = append(m.writeData, p...)
	return len(p), nil
}

func (m *mockConn) Close() error {
	m.closed = true
	return nil
}

func (m *mockConn) LocalAddr() net.Addr                { return nil }
func (m *mockConn) RemoteAddr() net.Addr               { return nil }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

// mockClientWithConn is a mock client that returns a specific connection
type mockClientWithConn struct {
	mockClient
	conn net.Conn
}

func (m *mockClientWithConn) getConn() net.Conn {
	return m.conn
}

// === Task 3.4: Data Reader Goroutine Tests ===

// TestDataReader_ReadsPackets verifies the data reader reads packets from connection
func TestDataReader_ReadsPackets(t *testing.T) {
	// Create a mock connection with test data
	uiiPacket := []byte{
		0x7c, 0xff, 0xff, 0x20, 0x02, 0x10,
		0x00, 0x30, 0x00,
		0xE2, 0x00, 0x34, 0x11, 0xB8, 0x02, 0x01, 0x13, 0x83, 0x25, 0x85, 0x66,
		0xC9,
		0x00, // Checksum (will be calculated)
	}
	// Calculate correct checksum
	sum := 0
	for _, b := range uiiPacket[:len(uiiPacket)-1] {
		sum += int(b)
	}
	uiiPacket[len(uiiPacket)-1] = byte((^sum + 1) & 0xff)

	mockConn := &mockConn{
		readData: [][]byte{uiiPacket},
	}

	manager, _, mockCache, cancel := newTestAntennaManager()
	defer cancel()

	ctx, cancelCtx := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelCtx()

	// Start data reader
	manager.wg.Add(1)
	go manager.dataReader(ctx, mockConn)

	// Wait for packet to be processed
	time.Sleep(200 * time.Millisecond)

	// Cancel context to stop reader
	cancelCtx()
	manager.wg.Wait()

	// Verify reading was stored
	mockCache.mu.Lock()
	if len(mockCache.stored) != 1 {
		t.Errorf("expected 1 reading stored, got %d", len(mockCache.stored))
	}
	mockCache.mu.Unlock()

	// Verify connection was NOT closed by dataReader (caller manages lifecycle)
	if mockConn.closed {
		t.Error("dataReader should NOT close connection — caller manages lifecycle")
	}
}

// TestDataReader_UpdatesLastPacketTime verifies last packet time is updated
func TestDataReader_UpdatesLastPacketTime(t *testing.T) {
	uiiPacket := []byte{
		0x7c, 0xff, 0xff, 0x20, 0x02, 0x10,
		0x00, 0x30, 0x00,
		0xE2, 0x00, 0x34, 0x11, 0xB8, 0x02, 0x01, 0x13, 0x83, 0x25, 0x85, 0x66,
		0xC9,
		0x00,
	}
	sum := 0
	for _, b := range uiiPacket[:len(uiiPacket)-1] {
		sum += int(b)
	}
	uiiPacket[len(uiiPacket)-1] = byte((^sum + 1) & 0xff)

	mockConn := &mockConn{
		readData: [][]byte{uiiPacket},
	}

	manager, _, _, cancel := newTestAntennaManager()
	defer cancel()

	// Set old last packet time
	manager.mu.Lock()
	oldTime := time.Now().Add(-10 * time.Second)
	manager.lastPacketTime = oldTime
	manager.mu.Unlock()

	ctx, cancelCtx := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelCtx()

	manager.wg.Add(1)
	go manager.dataReader(ctx, mockConn)

	time.Sleep(200 * time.Millisecond)
	cancelCtx()
	manager.wg.Wait()

	// Verify last packet time was updated
	manager.mu.RLock()
	if !manager.lastPacketTime.After(oldTime) {
		t.Error("expected lastPacketTime to be updated")
	}
	manager.mu.RUnlock()
}

// TestDataReader_ValidatesChecksum verifies checksum validation
func TestDataReader_ValidatesChecksum(t *testing.T) {
	// Packet with invalid checksum
	invalidPacket := []byte{
		0x7c, 0xff, 0xff, 0x20, 0x02, 0x10,
		0x00, 0x30, 0x00,
		0xE2, 0x00, 0x34, 0x11, 0xB8, 0x02, 0x01, 0x13, 0x83, 0x25, 0x85, 0x66,
		0xC9,
		0xFF, // Invalid checksum
	}

	mockConn := &mockConn{
		readData: [][]byte{invalidPacket},
	}

	manager, _, mockCache, cancel := newTestAntennaManager()
	defer cancel()

	ctx, cancelCtx := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelCtx()

	manager.wg.Add(1)
	go manager.dataReader(ctx, mockConn)

	time.Sleep(200 * time.Millisecond)
	cancelCtx()
	manager.wg.Wait()

	// Invalid checksum packets must be ignored for persistence.
	mockCache.mu.Lock()
	if len(mockCache.stored) != 0 {
		t.Errorf("expected 0 readings for invalid checksum packet, got %d", len(mockCache.stored))
	}
	mockCache.mu.Unlock()
}

// TestDataReader_ExitsOnReadError verifies reader exits on read error
func TestDataReader_ExitsOnReadError(t *testing.T) {
	mockConn := &mockConn{
		readError: io.EOF,
	}

	manager, _, _, cancel := newTestAntennaManager()
	defer cancel()

	ctx := context.Background()

	manager.wg.Add(1)
	go manager.dataReader(ctx, mockConn)

	// Wait for reader to exit
	done := make(chan struct{})
	go func() {
		manager.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Reader exited as expected
	case <-time.After(2 * time.Second):
		t.Error("data reader should exit on read error")
	}
}

// TestDataReader_ExitsOnContextCancel verifies reader exits on context cancellation
func TestDataReader_ExitsOnContextCancel(t *testing.T) {
	// Connection that blocks forever
	mockConn := &mockConn{
		readData: [][]byte{}, // Empty - will block
	}

	manager, _, _, cancel := newTestAntennaManager()
	defer cancel()

	ctx, cancelCtx := context.WithCancel(context.Background())

	manager.wg.Add(1)
	go manager.dataReader(ctx, mockConn)

	// Give time for reader to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context
	cancelCtx()

	// Wait for reader to exit
	done := make(chan struct{})
	go func() {
		manager.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Reader exited as expected
	case <-time.After(2 * time.Second):
		t.Error("data reader should exit on context cancellation")
	}
}

// TestDataReader_MultiplePackets verifies reader handles multiple packets
func TestDataReader_MultiplePackets(t *testing.T) {
	uiiPacket1 := []byte{
		0x7c, 0xff, 0xff, 0x20, 0x02, 0x10,
		0x00, 0x30, 0x00,
		0xE2, 0x00, 0x34, 0x11, 0xB8, 0x02, 0x01, 0x13, 0x83, 0x25, 0x85, 0x66,
		0xC9,
		0x00,
	}
	sum := 0
	for _, b := range uiiPacket1[:len(uiiPacket1)-1] {
		sum += int(b)
	}
	uiiPacket1[len(uiiPacket1)-1] = byte((^sum + 1) & 0xff)

	// Second packet with different EPC
	uiiPacket2 := []byte{
		0x7c, 0xff, 0xff, 0x20, 0x02, 0x10,
		0x00, 0x30, 0x00,
		0xE2, 0x00, 0x34, 0x11, 0xB8, 0x02, 0x01, 0x13, 0x83, 0x25, 0x85, 0x67, // Different last byte
		0xC8, // Different RSSI
		0x00,
	}
	sum = 0
	for _, b := range uiiPacket2[:len(uiiPacket2)-1] {
		sum += int(b)
	}
	uiiPacket2[len(uiiPacket2)-1] = byte((^sum + 1) & 0xff)

	mockConn := &mockConn{
		readData: [][]byte{uiiPacket1, uiiPacket2},
	}

	manager, _, mockCache, cancel := newTestAntennaManager()
	defer cancel()

	ctx, cancelCtx := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelCtx()

	manager.wg.Add(1)
	go manager.dataReader(ctx, mockConn)

	time.Sleep(300 * time.Millisecond)
	cancelCtx()
	manager.wg.Wait()

	// Verify both readings were stored
	mockCache.mu.Lock()
	if len(mockCache.stored) != 2 {
		t.Errorf("expected 2 readings stored, got %d", len(mockCache.stored))
	}
	mockCache.mu.Unlock()

	// Verify reading count
	manager.mu.RLock()
	if manager.readingCount != 2 {
		t.Errorf("expected readingCount 2, got %d", manager.readingCount)
	}
	manager.mu.RUnlock()
}

// TestDataReader_CallsHandlePacket verifies HandlePacket is called for each packet
func TestDataReader_CallsHandlePacket(t *testing.T) {
	ackPacket := []byte{0x7c, 0xff, 0xff, 0x20, 0x00, 0x00}
	sum := 0
	for _, b := range ackPacket {
		sum += int(b)
	}
	ackPacket = append(ackPacket, byte((^sum+1)&0xff))

	mockConn := &mockConn{
		readData: [][]byte{ackPacket},
	}

	manager, mockClient, _, cancel := newTestAntennaManager()
	defer cancel()

	// Set old last packet time
	manager.mu.Lock()
	manager.lastPacketTime = time.Now().Add(-10 * time.Second)
	manager.mu.Unlock()

	ctx, cancelCtx := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelCtx()

	manager.wg.Add(1)
	go manager.dataReader(ctx, mockConn)

	time.Sleep(200 * time.Millisecond)
	cancelCtx()
	manager.wg.Wait()

	// Verify client last activity was updated (HandlePacket was called)
	mockClient.mu.Lock()
	if mockClient.lastActivity.IsZero() {
		t.Error("expected client lastActivity to be updated (HandlePacket was called)")
	}
	mockClient.mu.Unlock()
}
