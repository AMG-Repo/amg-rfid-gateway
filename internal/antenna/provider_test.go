package antenna

import (
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestAntennaManagerProvider_New(t *testing.T) {
	provider := NewAntennaManagerProvider()
	assert.NotNil(t, provider)
	assert.Empty(t, provider.managers)
}

func TestAntennaManagerProvider_AddManager(t *testing.T) {
	provider := NewAntennaManagerProvider()

	// Create a simple mock
	mockClient := &simpleMockCommandSender{}
	antCfg := config.AntennaConfig{ID: "test-ant-01"}
	gwCfg := &config.GatewayConfig{}

	manager := NewAntennaManager(mockClient, antCfg, gwCfg, nil)
	provider.AddManager(manager)

	assert.Len(t, provider.managers, 1)
}

func TestAntennaManagerProvider_GetAntennaStatuses_Empty(t *testing.T) {
	provider := NewAntennaManagerProvider()
	statuses := provider.GetAntennaStatuses()

	assert.Empty(t, statuses)
}

func TestAntennaManagerProvider_GetAntennaStatuses_WithManagers(t *testing.T) {
	provider := NewAntennaManagerProvider()

	// Create and add managers
	mockClient := &simpleMockCommandSender{connected: true}

	antCfg1 := config.AntennaConfig{ID: "ant-01"}
	antCfg2 := config.AntennaConfig{ID: "ant-02"}
	gwCfg := &config.GatewayConfig{}

	manager1 := NewAntennaManager(mockClient, antCfg1, gwCfg, nil)
	manager2 := NewAntennaManager(mockClient, antCfg2, gwCfg, nil)

	provider.AddManager(manager1)
	provider.AddManager(manager2)

	statuses := provider.GetAntennaStatuses()

	assert.Len(t, statuses, 2)
	assert.Equal(t, "ant-01", statuses[0].ID)
	assert.Equal(t, "ant-02", statuses[1].ID)
}

// simpleMockCommandSender is a simple mock for testing
type simpleMockCommandSender struct {
	connected bool
}

func (m *simpleMockCommandSender) SendCommand(data []byte) error {
	return nil
}

func (m *simpleMockCommandSender) GetLastActivity() time.Time {
	return time.Now()
}

func (m *simpleMockCommandSender) SetLastActivity(t time.Time) {
}

func (m *simpleMockCommandSender) IsConnected() bool {
	return m.connected
}
