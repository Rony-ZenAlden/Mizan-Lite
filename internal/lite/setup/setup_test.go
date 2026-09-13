package setup_test

import (
	"context"
	"crypto/rand"
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
	ow := owner.NewService(db, ownerdb.NewStore(db), ownertest.Hasher(), clk, rand.Reader, litetest.Logger())
	return setup.NewService(db, st, ow), st, ow, db
}

func TestFirstRunCompletesEverythingTogether(t *testing.T) {
	ctx := context.Background()
	svc, st, ow, _ := build(t)

	if done, err := svc.Complete(ctx); err != nil || done {
		t.Fatalf("a fresh installation reports first run done: %v %v", done, err)
	}
	code, err := svc.Run(ctx, setup.Input{ShopName: " بقالية المونة ", Locale: "en", PIN: "246813"})
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
	if _, err := svc.Run(ctx, setup.Input{ShopName: "x", Locale: "ar", PIN: "739251"}); errs.CodeOf(err) != setup.CodeAlreadyComplete {
		t.Fatalf("a second first run = %v", err)
	}
}

func TestAWeakPINLeavesNoShopNameBehind(t *testing.T) {
	ctx := context.Background()
	svc, st, ow, _ := build(t)
	_, err := svc.Run(ctx, setup.Input{ShopName: "المونة", Locale: "en", PIN: "123456"})
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
	if _, err := svc.Run(ctx, setup.Input{ShopName: "  ", Locale: "ar", PIN: "246813"}); errs.CodeOf(err) != settingsdomain.CodeShopNameRequired {
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
