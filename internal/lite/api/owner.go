package api

import (
	"context"
	"math"
	"time"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
	"github.com/mizan-erp/mizan/internal/lite/owner"
)

// Owner is the owner's PIN, owner mode and history.
type Owner struct{ core *core }

// OwnerStatusDTO is what the header and the owner screen show. Durations cross as whole seconds rather than
// timestamps, so a countdown never depends on the webview's clock agreeing with Go's.
type OwnerStatusDTO struct {
	SetUp           bool `json:"setUp"`
	LockedSeconds   int  `json:"lockedSeconds"`
	ElevatedSeconds int  `json:"elevatedSeconds"`
}

func seconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int(math.Ceil(d.Seconds()))
}

func toOwnerStatusDTO(s owner.Status) OwnerStatusDTO {
	return OwnerStatusDTO{SetUp: s.SetUp, LockedSeconds: seconds(s.LockedFor), ElevatedSeconds: seconds(s.ElevatedFor)}
}

func (o *Owner) status(method string, fn func(ctx context.Context, app *bootstrap.App) (owner.Status, error)) envelope.Result[OwnerStatusDTO] {
	return call(o.core, method, func(ctx context.Context, app *bootstrap.App) (OwnerStatusDTO, error) {
		s, err := fn(ctx, app)
		return toOwnerStatusDTO(s), err
	})
}

// Status reports set-up, lockout and owner mode.
func (o *Owner) Status() envelope.Result[OwnerStatusDTO] {
	return o.status("Owner.Status", func(ctx context.Context, app *bootstrap.App) (owner.Status, error) {
		return app.Owner.Status(ctx)
	})
}

// PINInput carries a PIN.
type PINInput struct {
	PIN string `json:"pin"`
}

// Elevate enters owner mode if the PIN is right.
func (o *Owner) Elevate(in PINInput) envelope.Result[OwnerStatusDTO] {
	return o.status("Owner.Elevate", func(ctx context.Context, app *bootstrap.App) (owner.Status, error) {
		return app.Owner.Elevate(ctx, in.PIN)
	})
}

// EndElevation leaves owner mode — the header's Lock button.
func (o *Owner) EndElevation() envelope.Result[OwnerStatusDTO] {
	return o.status("Owner.EndElevation", func(ctx context.Context, app *bootstrap.App) (owner.Status, error) {
		return app.Owner.EndElevation(ctx)
	})
}

// ChangePINInput replaces the PIN.
type ChangePINInput struct {
	CurrentPIN string `json:"currentPin"`
	NewPIN     string `json:"newPin"`
}

// ChangePIN replaces the owner's PIN.
func (o *Owner) ChangePIN(in ChangePINInput) envelope.Result[OwnerStatusDTO] {
	return o.status("Owner.ChangePIN", func(ctx context.Context, app *bootstrap.App) (owner.Status, error) {
		if err := app.Owner.ChangePIN(ctx, in.CurrentPIN, in.NewPIN); err != nil {
			return owner.Status{}, err
		}
		return app.Owner.Status(ctx)
	})
}

// RecoverInput sets a new PIN with the recovery code.
type RecoverInput struct {
	RecoveryCode string `json:"recoveryCode"`
	NewPIN       string `json:"newPin"`
}

// RecoverResultDTO carries the NEW recovery code; the one used is spent.
type RecoverResultDTO struct {
	RecoveryCode string `json:"recoveryCode"`
}

// Recover sets a new PIN with the recovery code.
func (o *Owner) Recover(in RecoverInput) envelope.Result[RecoverResultDTO] {
	return call(o.core, "Owner.Recover", func(ctx context.Context, app *bootstrap.App) (RecoverResultDTO, error) {
		code, err := app.Owner.Recover(ctx, in.RecoveryCode, in.NewPIN)
		return RecoverResultDTO{RecoveryCode: code}, err
	})
}

// OwnerEventDTO is one entry in the owner's history.
type OwnerEventDTO struct {
	ID         string `json:"id"`
	OccurredAt string `json:"occurredAt"` // UTC, the portable CHAR(24) form
	Kind       string `json:"kind"`
	Action     string `json:"action"`
	SubjectID  string `json:"subjectId"`
	Before     string `json:"before"`
	After      string `json:"after"`
}

// Events returns the owner's history, newest first.
func (o *Owner) Events(limit int) envelope.Result[[]OwnerEventDTO] {
	return call(o.core, "Owner.Events", func(ctx context.Context, app *bootstrap.App) ([]OwnerEventDTO, error) {
		events, err := app.Owner.Events(ctx, limit)
		if err != nil {
			return nil, err
		}
		out := make([]OwnerEventDTO, 0, len(events))
		for _, e := range events {
			dto := OwnerEventDTO{
				ID: e.ID.String(), OccurredAt: clock.Format(e.OccurredAt), Kind: string(e.Kind),
				Action: e.Action, Before: e.Before, After: e.After,
			}
			if !e.SubjectID.IsZero() {
				dto.SubjectID = e.SubjectID.String()
			}
			out = append(out, dto)
		}
		return out, nil
	})
}
