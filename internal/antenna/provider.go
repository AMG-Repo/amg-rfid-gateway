// Package antenna provides antenna management with intelligent read loop and heartbeat.
package antenna

import "sync"

// AntennaManagerProvider implements the AntennaProvider interface for health.Monitor
type AntennaManagerProvider struct {
	mu       sync.RWMutex
	managers []*AntennaManager
}

// NewAntennaManagerProvider creates a new provider.
func NewAntennaManagerProvider() *AntennaManagerProvider {
	return &AntennaManagerProvider{
		managers: make([]*AntennaManager, 0),
	}
}

// AddManager adds an antenna manager to the provider.
func (p *AntennaManagerProvider) AddManager(manager *AntennaManager) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.managers = append(p.managers, manager)
}

// GetAntennaStatuses returns the current status of all antennas.
func (p *AntennaManagerProvider) GetAntennaStatuses() []AntennaStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	statuses := make([]AntennaStatus, 0, len(p.managers))
	for _, manager := range p.managers {
		statuses = append(statuses, manager.GetStatus())
	}
	return statuses
}
