package config

import (
	"sync"
	"testing"
)

// TestHolder_GetReturnsInitialConfig verifies that Get returns the initial config.
func TestHolder_GetReturnsInitialConfig(t *testing.T) {
	// Arrange: create a config
	cfg := &GatewayConfig{
		GatewayID: "test-gateway",
		CompanyID: "test-company",
	}

	// Act: create holder and get config
	holder := NewHolder(cfg)
	got := holder.Get()

	// Assert: verify we got the same config
	if got != cfg {
		t.Errorf("Get() = %v, want %v", got, cfg)
	}
	if got.GatewayID != "test-gateway" {
		t.Errorf("GatewayID = %s, want test-gateway", got.GatewayID)
	}
}

// TestHolder_SwapUpdatesConfig verifies that Swap updates the config atomically.
func TestHolder_SwapUpdatesConfig(t *testing.T) {
	// Arrange: create initial config and holder
	initialCfg := &GatewayConfig{
		GatewayID: "initial-gateway",
		CompanyID: "initial-company",
	}
	holder := NewHolder(initialCfg)

	newCfg := &GatewayConfig{
		GatewayID: "new-gateway",
		CompanyID: "new-company",
	}

	// Act: swap config
	holder.Swap(newCfg)
	got := holder.Get()

	// Assert: verify we got the new config
	if got != newCfg {
		t.Errorf("Get() after Swap = %v, want %v", got, newCfg)
	}
	if got.GatewayID != "new-gateway" {
		t.Errorf("GatewayID after Swap = %s, want new-gateway", got.GatewayID)
	}
}

// TestHolder_ConcurrentAccess verifies thread-safe concurrent access.
func TestHolder_ConcurrentAccess(t *testing.T) {
	// Arrange: create holder
	cfg := &GatewayConfig{GatewayID: "initial"}
	holder := NewHolder(cfg)

	// Act: run concurrent goroutines doing Get and Swap
	var wg sync.WaitGroup
	numGoroutines := 100
	numIterations := 100

	// Writers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				newCfg := &GatewayConfig{GatewayID: "writer-%d-%d"}
				holder.Swap(newCfg)
			}
		}(i)
	}

	// Readers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				_ = holder.Get()
			}
		}(i)
	}

	// Wait for all goroutines
	wg.Wait()

	// Assert: if we got here without race detector complaining, we're good
	// Just verify we can still Get after all operations
	_ = holder.Get()
}
