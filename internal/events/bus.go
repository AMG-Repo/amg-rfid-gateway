// Package events provides a pub/sub event bus for tag detection events.
package events

import (
	"sync"
	"time"
)

// TagDetected is published when an antenna detects a tag.
type TagDetected struct {
	EPC       string    `json:"epc"`
	RSSI      int       `json:"rssi"`
	AntennaID string    `json:"antenna_id"`
	Timestamp time.Time `json:"timestamp"`
}

// AutoConfirmed is published when the gateway confirmed a tag pass by itself
// (sent to the VPS or queued locally).
type AutoConfirmed struct {
	UII            string `json:"uii"`
	Action         string `json:"action"`
	ConfirmationID string `json:"confirmation_id"`
}

// EventBus provides pub/sub for tag detection events.
// Uses buffered channels to prevent slow subscribers from blocking publishers.
type EventBus struct {
	subscribers []chan TagDetected
	autoSubs    []chan AutoConfirmed
	mu          sync.RWMutex
	buffer      int
}

// NewEventBus creates a new event bus with the specified buffer size per subscriber.
func NewEventBus(buffer int) *EventBus {
	if buffer <= 0 {
		buffer = 1000
	}
	return &EventBus{
		subscribers: make([]chan TagDetected, 0),
		buffer:      buffer,
	}
}

// Subscribe creates a new buffered channel and adds it to subscribers.
// Returns the channel for receiving events.
func (b *EventBus) Subscribe() chan TagDetected {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan TagDetected, b.buffer)
	b.subscribers = append(b.subscribers, ch)
	return ch
}

// Unsubscribe removes a channel from subscribers and closes it.
func (b *EventBus) Unsubscribe(ch chan TagDetected) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i, subscriber := range b.subscribers {
		if subscriber == ch {
			// Remove from slice
			b.subscribers = append(b.subscribers[:i], b.subscribers[i+1:]...)
			close(ch)
			return
		}
	}
}

// Publish sends an event to all subscribers.
// Uses non-blocking send - drops events for slow subscribers (buffer full).
// Includes panic recovery to handle closed channels during concurrent unsubscribe.
func (b *EventBus) Publish(event TagDetected) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, ch := range b.subscribers {
		func() {
			defer func() {
				// Recover from panic if channel is closed during send
				if r := recover(); r != nil {
					// Channel was closed, ignore and continue
				}
			}()

			select {
			case ch <- event:
				// Event delivered
			default:
				// Channel full, drop event for this subscriber
				// This prevents slow subscribers from blocking the publisher
			}
		}()
	}
}

// Close closes all subscriber channels and clears the subscriber list.
func (b *EventBus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, ch := range b.subscribers {
		close(ch)
	}
	b.subscribers = make([]chan TagDetected, 0)
	for _, ch := range b.autoSubs {
		close(ch)
	}
	b.autoSubs = nil
}

// SubscribeAutoConfirmed creates a buffered channel receiving AutoConfirmed events.
func (b *EventBus) SubscribeAutoConfirmed() chan AutoConfirmed {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan AutoConfirmed, b.buffer)
	b.autoSubs = append(b.autoSubs, ch)
	return ch
}

// UnsubscribeAutoConfirmed removes and closes an AutoConfirmed channel; a
// channel already removed is ignored.
func (b *EventBus) UnsubscribeAutoConfirmed(ch chan AutoConfirmed) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i, subscriber := range b.autoSubs {
		if subscriber == ch {
			b.autoSubs = append(b.autoSubs[:i], b.autoSubs[i+1:]...)
			close(ch)
			return
		}
	}
}

// PublishAutoConfirmed sends an AutoConfirmed event to its subscribers,
// dropping it for subscribers whose buffer is full.
func (b *EventBus) PublishAutoConfirmed(event AutoConfirmed) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, ch := range b.autoSubs {
		select {
		case ch <- event:
		default:
		}
	}
}
