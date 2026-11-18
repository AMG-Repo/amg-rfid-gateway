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

// AddManager adds an antenna manager to the provider. A manager for an antenna
// ID already present (a reconnect) replaces the previous one in place.
func (p *AntennaManagerProvider) AddManager(manager *AntennaManager) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, existing := range p.managers {
		if existing.config.ID == manager.config.ID {
			p.managers[i] = manager
			return
		}
	}
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
