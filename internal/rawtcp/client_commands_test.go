package rawtcp

import (
	"bytes"
	"net"
	"testing"
	"time"
)

// === Task 3.1: Command Sending Methods Tests ===

// TestClient_SendCommand verifies raw command sending
func TestClient_SendCommand(t *testing.T) {
	// Create mock TCP server that receives and echoes commands
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)

	// Channel to capture received data
	receivedData := make(chan []byte, 1)

	// Start mock server
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read the command
		buf := make([]byte, 256)
		n, _ := conn.Read(buf)
		if n > 0 {
			receivedData <- buf[:n]
		}
	}()

	client := NewClient("127.0.0.1", addr.Port)

	// Connect
	err = client.Connect()
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Disconnect()

	// Send a command
	command := []byte{0x7c, 0xff, 0xff, 0x20, 0x00, 0x00, 0x66}
	err = client.SendCommand(command)
	if err != nil {
		t.Fatalf("SendCommand failed: %v", err)
	}

	// Verify data was received by server
	select {
	case data := <-receivedData:
		if !bytes.Equal(data, command) {
			t.Errorf("expected command %X, got %X", command, data)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timeout waiting for command to be received")
	}
}

// TestClient_SendCommand_NotConnected verifies error when not connected
func TestClient_SendCommand_NotConnected(t *testing.T) {
	client := NewClient("127.0.0.1", 12345)

	command := []byte{0x7c, 0xff, 0xff, 0x20, 0x00, 0x00, 0x66}
	err := client.SendCommand(command)
	if err == nil {
		t.Error("expected error when sending command while not connected")
	}
}

// TestClient_SendReadUII verifies Read UII command format
func TestClient_SendReadUII(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	receivedData := make(chan []byte, 1)

	go func() {
		conn, _ := listener.Accept()
		if conn != nil {
			defer conn.Close()
			buf := make([]byte, 256)
			n, _ := conn.Read(buf)
			if n > 0 {
				receivedData <- buf[:n]
			}
		}
	}()

	client := NewClient("127.0.0.1", addr.Port)
	err = client.Connect()
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Disconnect()

	err = client.SendReadUII()
	if err != nil {
		t.Fatalf("SendReadUII failed: %v", err)
	}

	select {
	case data := <-receivedData:
		// Expected: [0x7c, 0xff, 0xff, 0x20, 0x00, 0x00, checksum]
		expected := []byte{0x7c, 0xff, 0xff, 0x20, 0x00, 0x00, 0x66}
		if !bytes.Equal(data, expected) {
			t.Errorf("expected Read UII command %X, got %X", expected, data)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timeout waiting for Read UII command")
	}
}

// TestClient_SendHeartbeat verifies Heartbeat command format
func TestClient_SendHeartbeat(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	receivedData := make(chan []byte, 1)

	go func() {
		conn, _ := listener.Accept()
		if conn != nil {
			defer conn.Close()
			buf := make([]byte, 256)
			n, _ := conn.Read(buf)
			if n > 0 {
				receivedData <- buf[:n]
			}
		}
	}()

	client := NewClient("127.0.0.1", addr.Port)
	err = client.Connect()
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Disconnect()

	err = client.SendHeartbeat()
	if err != nil {
		t.Fatalf("SendHeartbeat failed: %v", err)
	}

	select {
	case data := <-receivedData:
		// Expected: [0x7c, 0xff, 0xff, 0x01, 0x00, 0x00, checksum]
		// Checksum: 0x7c + 0xff + 0xff + 0x01 + 0x00 + 0x00 = 0x27b
		// ~0x27b + 1 = 0xfffffd85 & 0xff = 0x85
		expected := []byte{0x7c, 0xff, 0xff, 0x01, 0x00, 0x00, 0x85}
		if !bytes.Equal(data, expected) {
			t.Errorf("expected Heartbeat command %X, got %X", expected, data)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timeout waiting for Heartbeat command")
	}
}

// TestClient_CalculateCommandChecksum verifies checksum calculation
func TestClient_CalculateCommandChecksum(t *testing.T) {
	tests := []struct {
		name     string
		command  []byte
		expected byte
	}{
		{
			name:     "Read UII command",
			command:  []byte{0x7c, 0xff, 0xff, 0x20, 0x00, 0x00},
			expected: 0x66, // Calculated: sum=0x29a, checksum=0x66
		},
		{
			name:     "Heartbeat command",
			command:  []byte{0x7c, 0xff, 0xff, 0x01, 0x00, 0x00},
			expected: 0x85, // Calculated: sum=0x27b, checksum=0x85
		},
		{
			name:     "Simple command",
			command:  []byte{0x01, 0x02, 0x03},
			expected: 0xfa, // Calculated: sum=0x06, checksum=0xfa
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checksum := calculateCommandChecksum(tt.command)
			if checksum != tt.expected {
				t.Errorf("expected checksum 0x%02x, got 0x%02x", tt.expected, checksum)
			}

			// Verify: sum + checksum should wrap to 0
			sum := 0
			for _, b := range tt.command {
				sum += int(b)
			}
			sum += int(checksum)
			if sum&0xff != 0 {
				t.Errorf("checksum verification failed: sum & 0xff = %d", sum&0xff)
			}
		})
	}
}

// TestClient_LastActivity verifies last activity tracking
func TestClient_LastActivity(t *testing.T) {
	client := NewClient("192.168.1.100", 8080)

	// Initially zero
	activity := client.GetLastActivity()
	if !activity.IsZero() {
		t.Error("expected initial last activity to be zero")
	}

	// Set activity
	now := time.Now()
	client.SetLastActivity(now)

	activity = client.GetLastActivity()
	if !activity.Equal(now) {
		t.Error("expected last activity to be set")
	}
}

// TestClient_Write verifies raw byte writing
func TestClient_Write(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	receivedData := make(chan []byte, 1)

	go func() {
		conn, _ := listener.Accept()
		if conn != nil {
			defer conn.Close()
			buf := make([]byte, 256)
			n, _ := conn.Read(buf)
			if n > 0 {
				receivedData <- buf[:n]
			}
		}
	}()

	client := NewClient("127.0.0.1", addr.Port)
	err = client.Connect()
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Disconnect()

	// Write raw data
	data := []byte{0x01, 0x02, 0x03, 0x04}
	err = client.Write(data)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	select {
	case received := <-receivedData:
		if !bytes.Equal(received, data) {
			t.Errorf("expected %X, got %X", data, received)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timeout waiting for data")
	}
}

// TestClient_Write_NotConnected verifies error when writing while not connected
func TestClient_Write_NotConnected(t *testing.T) {
	client := NewClient("127.0.0.1", 12345)

	data := []byte{0x01, 0x02, 0x03}
	err := client.Write(data)
	if err == nil {
		t.Error("expected error when writing while not connected")
	}
}

// TestClient_SatisfiesCommandSenderInterface verifies the client satisfies the antenna.CommandSender interface
func TestClient_SatisfiesCommandSenderInterface(t *testing.T) {
	// This test verifies compile-time compatibility with the CommandSender interface
	client := NewClient("192.168.1.100", 8080)

	// The interface requires:
	// - SendCommand(data []byte) error
	// - GetLastActivity() time.Time
	// - SetLastActivity(t time.Time)
	// - IsConnected() bool

	// These should all compile and work
	_ = client.SendCommand
	_ = client.GetLastActivity
	_ = client.SetLastActivity
	_ = client.IsConnected
}
