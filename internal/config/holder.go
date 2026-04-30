package config

import "sync/atomic"

// Holder provides thread-safe config access with atomic swap.
type Holder struct {
	v atomic.Value // stores *GatewayConfig
}

// NewHolder creates a new config holder with the given config.
func NewHolder(cfg *GatewayConfig) *Holder {
	h := &Holder{}
	h.v.Store(cfg)
	return h
}

// Get returns the current config.
func (h *Holder) Get() *GatewayConfig {
	return h.v.Load().(*GatewayConfig)
}

// Swap updates the config atomically.
func (h *Holder) Swap(cfg *GatewayConfig) {
	h.v.Store(cfg)
}
