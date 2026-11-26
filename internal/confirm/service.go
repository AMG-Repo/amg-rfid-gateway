// Package confirm registers entrada/salida movements: the kanban gate, the
// online send with its idempotency id and instant, the local queue fallback
// and the cached location update. The web panel and the auto-confirmer share it.
package confirm

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/httpclient"
	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
	"github.com/amg-rfid/amg-rfid-gateway/internal/uuidutil"
)

// Modes of an accepted confirmation.
const (
	ModeOnline  = "online"
	ModeOffline = "offline"
)

var (
	// ErrCannotVerify means the tag's kanban assignment could not be checked.
	ErrCannotVerify = errors.New("kanban assignment cannot be verified")
	// ErrNotAssigned means the tag has no kanban: no movement is registered.
	ErrNotAssigned = errors.New("tag not assigned to a kanban")
	// ErrNoStore means there is no local store to queue the confirmation.
	ErrNoStore = errors.New("no local store")
	// ErrQueueFull is localstore.ErrQueueFull: the queue cap was reached.
	ErrQueueFull = localstore.ErrQueueFull
	// ErrAutoInProgress means an automatic confirm of the same tag is in
	// flight: the manual confirm is refused, nothing is written.
	ErrAutoInProgress = errors.New("automatic confirmation in progress")
	// ErrAlreadyAutoConfirmed means the tag's pass was already confirmed
	// automatically and the tag was not read since: the manual confirm is
	// stale, nothing is written.
	ErrAlreadyAutoConfirmed = errors.New("already confirmed automatically")
)

// Claimer guards that a tag pass is confirmed at most once. Manual claims are
// synchronous and taken before anything is sent or queued; automatic
// requests are claimed by the auto-confirmer before it decides them.
type Claimer interface {
	// ClaimManual consumes the tag's current pass for a manual confirm; it
	// fails while an automatic confirm of the tag is in flight.
	ClaimManual(uii string) bool
}

// manualClaimErrer is a Claimer telling why a manual claim was refused
// (ErrAutoInProgress or ErrAlreadyAutoConfirmed).
type manualClaimErrer interface {
	ClaimManualErr(uii string) error
}

// Request is one movement to confirm. Action must already be canonical
// ("entrada" or "salida").
type Request struct {
	UII    string
	Action string
	// AntennaID defaults to "manual" when empty.
	AntennaID string
	// OccurredAt is when the movement happened; zero means now.
	OccurredAt time.Time
	// Auto marks a movement the gateway confirmed by itself.
	Auto bool
	// PassID is the tag pass an automatic confirm claimed.
	PassID uint64
}

// Result describes an accepted confirmation.
type Result struct {
	Mode           string
	ConfirmationID string
}

// Service confirms movements. VPS may be nil (always offline).
type Service struct {
	Store     *localstore.LocalStore
	VPS       *httpclient.VPSClient
	CompanyID string
	// MaxPending is the pending queue cap (0 = unlimited).
	MaxPending int
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Claimer, when set, is claimed by manual confirms before anything is
	// sent or queued; nil means no claims.
	Claimer Claimer
}

// Confirm registers the movement: online when the VPS is reachable and accepts
// it (recorded locally as synced), otherwise queued for the sync worker. Both
// carry the same confirmation id and instant, so the VPS dedupes replays.
func (s *Service) Confirm(ctx context.Context, req Request) (Result, error) {
	// Movements are only registered for tags assigned to a kanban. Checked before
	// anything is sent or queued, online or offline, and fail closed if the
	// assignment cannot be verified.
	if s.Store == nil {
		return Result{}, ErrCannotVerify
	}
	kanban, err := s.Store.GetKanbanByUII(req.UII)
	if err != nil {
		log.Printf("[Confirm] Failed to verify kanban assignment for %s: %v", req.UII, err)
		return Result{}, ErrCannotVerify
	}
	if kanban == nil {
		return Result{}, ErrNotAssigned
	}

	if s.Claimer != nil && !req.Auto {
		if c, ok := s.Claimer.(manualClaimErrer); ok {
			if err := c.ClaimManualErr(req.UII); err != nil {
				return Result{}, err
			}
		} else if !s.Claimer.ClaimManual(req.UII) {
			return Result{}, ErrAutoInProgress
		}
	}

	confirmationID := uuidutil.NewV4()
	occurredAt := req.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = s.now()
	}
	occurredAt = occurredAt.UTC()
	antennaID := req.AntennaID
	if antennaID == "" {
		antennaID = "manual"
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if CheckVPSHealth(ctx, s.VPS) {
		err := s.VPS.SendIdempotentConfirmation(s.CompanyID, req.UII, req.Action, antennaID, confirmationID, occurredAt)
		if err == nil {
			// The VPS has the confirmation, so a failed local record is logged only.
			s.applyMovementLocation(req.UII, req.Action)
			if err := s.Store.RecordSyncedConfirmationWithIDAuto(req.UII, req.Action, antennaID, confirmationID, occurredAt, req.Auto); err != nil {
				log.Printf("[Confirm] Failed to record synced confirmation for %s: %v", req.UII, err)
			}
			return Result{Mode: ModeOnline, ConfirmationID: confirmationID}, nil
		}
		log.Printf("[Confirm] VPS confirmation failed: %v, falling back to local queue", err)
	}

	if _, err := s.Store.CreateConfirmationWithIDAuto(req.UII, req.Action, antennaID, confirmationID, occurredAt, s.MaxPending, req.Auto); err != nil {
		if !errors.Is(err, localstore.ErrQueueFull) {
			log.Printf("[Confirm] Failed to create local confirmation: %v", err)
		}
		return Result{Mode: ModeOffline}, err
	}
	s.applyMovementLocation(req.UII, req.Action)
	return Result{Mode: ModeOffline, ConfirmationID: confirmationID}, nil
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// applyMovementLocation updates the cached tag location for an accepted
// confirmation; a failure is logged only, the movement itself is recorded.
func (s *Service) applyMovementLocation(uii, action string) {
	if err := s.Store.ApplyMovementLocation(uii, action); err != nil {
		log.Printf("[Confirm] Failed to update cached location of %s after %s: %v", uii, action, err)
	}
}

// CheckVPSHealth reports whether the VPS answers its health endpoint; any HTTP
// response (even an error status) means it is reachable.
func CheckVPSHealth(ctx context.Context, vps *httpclient.VPSClient) bool {
	if vps == nil {
		return false
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(timeoutCtx, http.MethodGet, vps.BaseURL()+"/health", nil)
	if err != nil {
		return false
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return true
}
