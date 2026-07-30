package config

import (
	"context"
	"log/slog"

	"github.com/mizan-erp/mizan/internal/kernel/event"
	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// SettingChangedEvent is raised when a setting or feature flag is written.
//
// It goes on the in-process DOMAIN bus, never the outbox (Step 0.6, D7). A settings change
// is in-process reactivity — "the language menu changed, re-render" — whose value expires in
// milliseconds. Giving it durable at-least-once delivery and retries would be machinery in
// service of nothing.
type SettingChangedEvent struct {
	Key     string `json:"key"`
	Scope   Scope  `json:"scope"`
	ScopeID id.ID  `json:"scope_id"`
}

func (SettingChangedEvent) EventType() string     { return "config.setting_changed" }
func (SettingChangedEvent) AggregateType() string { return "setting" }
func (e SettingChangedEvent) AggregateID() id.ID  { return e.ScopeID }

// Publisher is the slice of the event bus this package needs.
//
// Declared here rather than importing platform/eventbus so that config depends only on the
// kernel: the configuration registry must not acquire a dependency on the bus implementation
// merely to announce a change.
type Publisher interface {
	Publish(ctx context.Context, e event.Event) error
}

// busNotifier is the real ChangeNotifier, replacing Step 0.5's NoNotifier.
type busNotifier struct {
	pub Publisher
	log *slog.Logger
}

// NewBusNotifier returns a ChangeNotifier that announces changes on the domain bus.
//
// This is what makes "changing the language takes effect without a restart"
// (ARCHITECTURE_v1 §16.3) real rather than aspirational.
func NewBusNotifier(pub Publisher, log *slog.Logger) ChangeNotifier {
	if log == nil {
		log = slog.Default()
	}
	return &busNotifier{pub: pub, log: log}
}

// SettingChanged publishes the change.
//
// The port returns nothing, so a subscriber's failure cannot fail the write that caused it —
// deliberately. A UI component failing to refresh must not roll back a valid settings change;
// the failure is logged and the stored value stands.
func (n *busNotifier) SettingChanged(ctx context.Context, key string, scope Scope, scopeID id.ID) {
	if n.pub == nil {
		return
	}
	err := n.pub.Publish(ctx, SettingChangedEvent{Key: key, Scope: scope, ScopeID: scopeID})
	if err != nil {
		n.log.WarnContext(ctx, "a setting-changed subscriber failed; the change itself stands",
			slog.String("key", key), slog.Any("error", err))
	}
}
