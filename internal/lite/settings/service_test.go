package settings_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/lite/settings"
	"github.com/mizan-erp/mizan/internal/lite/settings/domain"
	"github.com/mizan-erp/mizan/internal/lite/settings/settingstest"
)

func newService(store settings.Store, log *slog.Logger) *settings.Service {
	return settings.NewService(settingstest.Immediate{}, store,
		clock.NewFixed(time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)), log)
}

func TestGetOnAFreshStoreIsTheDefaults(t *testing.T) {
	got, err := newService(settingstest.NewFake(), litetest.Logger()).Get(context.Background())
	if err != nil || got != domain.Defaults() {
		t.Fatalf("Get = %+v, %v", got, err)
	}
}

func TestGetLogsADamagedRowAndStillSucceeds(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	store := settingstest.NewFake()
	store.Seed(map[string]string{domain.KeyLocale: "damaged"})

	got, err := newService(store, log).Get(context.Background())
	if err != nil {
		t.Fatalf("a damaged preference must not fail Get: %v", err)
	}
	if got.Locale != domain.Arabic {
		t.Fatalf("locale = %q, want the default", got.Locale)
	}
	if !strings.Contains(buf.String(), "a stored setting was not used") ||
		!strings.Contains(buf.String(), domain.KeyLocale) {
		t.Fatalf("the damaged row was not logged; log was:\n%s", buf.String())
	}
}

func TestGetPropagatesAStoreFailure(t *testing.T) {
	store := settingstest.NewFake()
	store.FailLoad()
	if _, err := newService(store, litetest.Logger()).Get(context.Background()); !errors.Is(err, settingstest.ErrInjected) {
		t.Fatalf("err = %v, want the store's failure", err)
	}
}

func TestUpdateWritesOnlyWhatChanged(t *testing.T) {
	ctx := context.Background()
	store := settingstest.NewFake()
	svc := newService(store, litetest.Logger())

	en := "en"
	got, err := svc.Update(ctx, domain.Update{Locale: &en})
	if err != nil || got.Locale != domain.English {
		t.Fatalf("Update = %+v, %v", got, err)
	}
	if store.SaveCalls() != 1 {
		t.Fatalf("Save called %d times, want 1", store.SaveCalls())
	}

	// The same value again is not a write.
	if _, err = svc.Update(ctx, domain.Update{Locale: &en}); err != nil {
		t.Fatal(err)
	}
	if store.SaveCalls() != 1 {
		t.Fatalf("an unchanged value was written: %d Save calls", store.SaveCalls())
	}

	reread, err := svc.Get(ctx)
	if err != nil || reread.Locale != domain.English {
		t.Fatalf("Get after Update = %+v, %v", reread, err)
	}
}

func TestAnInvalidUpdateWritesNothing(t *testing.T) {
	store := settingstest.NewFake()
	bad := "zz"
	_, err := newService(store, litetest.Logger()).Update(context.Background(), domain.Update{Locale: &bad})
	if errs.CodeOf(err) != domain.CodeInvalidLocale {
		t.Fatalf("err = %v", err)
	}
	if store.SaveCalls() != 0 {
		t.Fatalf("an invalid update reached the store: %d Save calls", store.SaveCalls())
	}
}

func TestUpdatePropagatesASaveFailure(t *testing.T) {
	store := settingstest.NewFake()
	store.FailSave()
	en := "en"
	if _, err := newService(store, litetest.Logger()).Update(context.Background(), domain.Update{Locale: &en}); !errors.Is(err, settingstest.ErrInjected) {
		t.Fatalf("err = %v, want the store's failure", err)
	}
}
