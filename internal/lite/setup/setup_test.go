package setup_test

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/lite/owner"
	ownerdomain "github.com/mizan-erp/mizan/internal/lite/owner/domain"
	ownerdb "github.com/mizan-erp/mizan/internal/lite/owner/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/owner/ownertest"
	"github.com/mizan-erp/mizan/internal/lite/settings"
	settingsdomain "github.com/mizan-erp/mizan/internal/lite/settings/domain"
	settingsdb "github.com/mizan-erp/mizan/internal/lite/settings/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/setup"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// Real services over a real database: first run's one property is atomicity, which only a real transaction
// can show.
func build(t *testing.T) (*setup.Service, *settings.Service, *owner.Service, *database.Store) {
	t.Helper()
	db := litetest.OpenMigrated(t)
	clk := clock.NewFixed(time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC))
	st := settings.NewService(db, settingsdb.NewStore(db), clk, litetest.Logger())
	ow := owner.NewService(db, ownerdb.NewStore(db), ownertest.Hasher(), clk, rand.Reader, litetest.Logger(), ownertest.NewPolicy())
	return setup.NewService(db, st, ow, &rates{}), st, ow, db
}

func TestFirstRunCompletesEverythingTogether(t *testing.T) {
	ctx := context.Background()
	svc, st, ow, _ := build(t)

	if done, err := svc.Complete(ctx); err != nil || done {
		t.Fatalf("a fresh installation reports first run done: %v %v", done, err)
	}
	code, err := svc.Run(ctx, setup.Input{ShopName: " بقالية المونة ", Locale: "en", PIN: "246813", Rate: "15000"})
	if err != nil || len(ownerdomain.CanonicalRecoveryCode(code)) != ownerdomain.RecoveryLength {
		t.Fatalf("Run = %q, %v", code, err)
	}
	got, _ := st.Get(ctx)
	if got.ShopName != "بقالية المونة" || got.Locale != settingsdomain.English {
		t.Fatalf("settings = %+v", got)
	}
	if ok, _ := ow.IsSetUp(ctx); !ok {
		t.Fatal("no owner credential")
	}
	if done, _ := svc.Complete(ctx); !done {
		t.Fatal("first run is not reported complete")
	}
	if _, err := svc.Run(ctx, setup.Input{ShopName: "x", Locale: "ar", PIN: "739251", Rate: "15000"}); errs.CodeOf(err) != setup.CodeAlreadyComplete {
		t.Fatalf("a second first run = %v", err)
	}
}

func TestAWeakPINLeavesNoShopNameBehind(t *testing.T) {
	ctx := context.Background()
	svc, st, ow, _ := build(t)
	_, err := svc.Run(ctx, setup.Input{ShopName: "المونة", Locale: "en", PIN: "123456", Rate: "15000"})
	if errs.CodeOf(err) != ownerdomain.CodePINWeak {
		t.Fatalf("err = %v", err)
	}
	got, _ := st.Get(ctx)
	if got.ShopName != "" || got.Locale != settingsdomain.Arabic {
		t.Fatalf("a refused first run left settings written: %+v", got)
	}
	if ok, _ := ow.IsSetUp(ctx); ok {
		t.Fatal("a refused first run left a credential")
	}
}

func TestAMissingShopNameLeavesNoPIN(t *testing.T) {
	ctx := context.Background()
	svc, _, ow, _ := build(t)
	if _, err := svc.Run(ctx, setup.Input{ShopName: "  ", Locale: "ar", PIN: "246813", Rate: "15000"}); errs.CodeOf(err) != settingsdomain.CodeShopNameRequired {
		t.Fatalf("err = %v", err)
	}
	if ok, _ := ow.IsSetUp(ctx); ok {
		t.Fatal("a first run refused for its shop name created a credential")
	}
}

func TestCompleteReadsTheDataNotAFlag(t *testing.T) {
	ctx := context.Background()
	svc, st, ow, _ := build(t)

	// A credential with no shop name — a first run interrupted in an older build, say — is not complete.
	if _, err := ow.SetUp(ctx, "246813"); err != nil {
		t.Fatal(err)
	}
	if done, _ := svc.Complete(ctx); done {
		t.Fatal("complete with no shop name")
	}
	name := "المونة"
	if _, err := st.Update(ctx, settingsdomain.Update{ShopName: &name}); err != nil {
		t.Fatal(err)
	}
	if done, _ := svc.Complete(ctx); !done {
		t.Fatal("not complete with both facts present")
	}
}

// TestAShopNameWithoutAPINIsNotComplete is the other half of reading the data. Found by a drill: the test
// above could not tell a status that ignored the owner credential from one that read it, because it only
// ever set the credential first.
func TestAShopNameWithoutAPINIsNotComplete(t *testing.T) {
	ctx := context.Background()
	svc, st, _, _ := build(t)
	name := "المونة"
	if _, err := st.Update(ctx, settingsdomain.Update{ShopName: &name}); err != nil {
		t.Fatal(err)
	}
	if done, _ := svc.Complete(ctx); done {
		t.Fatal("complete with a shop name and no owner PIN")
	}
}

// rates records the opening rate in memory, or refuses one it is told to.
type rates struct {
	recorded string
	refuse   error
}

func (r *rates) RecordFirstRun(_ context.Context, rate string) error {
	if r.refuse != nil {
		return r.refuse
	}
	r.recorded = rate
	return nil
}

func TestARefusedRateLeavesNoShopBehind(t *testing.T) {
	ctx := context.Background()
	db := litetest.OpenMigrated(t)
	clk := clock.NewFixed(time.Date(2026, 9, 14, 6, 0, 0, 0, time.UTC))
	st := settings.NewService(db, settingsdb.NewStore(db), clk, litetest.Logger())
	ow := owner.NewService(db, ownerdb.NewStore(db), ownertest.Hasher(), clk, rand.Reader, litetest.Logger(), ownertest.NewPolicy())
	refusal := errs.Validation("lite.fx.rate_decimals", "a rate takes at most four decimals")
	svc := setup.NewService(db, st, ow, &rates{refuse: refusal})

	if _, err := svc.Run(ctx, setup.Input{ShopName: "المونة", Locale: "ar", PIN: "246813", Rate: "0.0000667"}); !errors.Is(err, refusal) {
		t.Fatalf("err = %v", err)
	}
	if done, _ := svc.Complete(ctx); done {
		t.Fatal("first run completed with its rate refused")
	}
	if current, _ := st.Get(ctx); current.ShopName != "" {
		t.Fatal("a shop name survived a refused rate")
	}
	if setUp, _ := ow.IsSetUp(ctx); setUp {
		t.Fatal("a PIN survived a refused rate")
	}
}

func TestAnOpeningRateIsRecordedWithTheShop(t *testing.T) {
	ctx := context.Background()
	db := litetest.OpenMigrated(t)
	clk := clock.NewFixed(time.Date(2026, 9, 14, 6, 0, 0, 0, time.UTC))
	st := settings.NewService(db, settingsdb.NewStore(db), clk, litetest.Logger())
	ow := owner.NewService(db, ownerdb.NewStore(db), ownertest.Hasher(), clk, rand.Reader, litetest.Logger(), ownertest.NewPolicy())
	r := &rates{}
	if _, err := setup.NewService(db, st, ow, r).Run(ctx, setup.Input{ShopName: "المونة", Locale: "ar", PIN: "246813", Rate: "١٤٨٠٠"}); err != nil {
		t.Fatal(err)
	}
	if r.recorded != "١٤٨٠٠" {
		t.Fatalf("the rate reached fx as %q", r.recorded)
	}
}
