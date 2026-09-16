package owner_test

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/litetest"
	"github.com/mizan-erp/mizan/internal/lite/owner"
	"github.com/mizan-erp/mizan/internal/lite/owner/domain"
	"github.com/mizan-erp/mizan/internal/lite/owner/infra/sqlite"
	"github.com/mizan-erp/mizan/internal/lite/owner/ownertest"
	"github.com/mizan-erp/mizan/internal/platform/crypto"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

const pin = "246813"

type fixture struct {
	svc   *owner.Service
	store owner.Store
	clk   *clock.Fixed
}

func fake(t *testing.T) fixture {
	t.Helper()
	clk := clock.NewFixed(time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC))
	store := ownertest.NewFake()
	return fixture{svc: owner.NewService(litetest.Immediate{}, store, ownertest.Hasher(), clk, rand.Reader, litetest.Logger()), store: store, clk: clk}
}

func (f fixture) setUp(t *testing.T) string {
	t.Helper()
	code, err := f.svc.SetUp(context.Background(), pin)
	if err != nil {
		t.Fatalf("SetUp: %v", err)
	}
	return code
}

func (f fixture) events(t *testing.T) []owner.Event {
	t.Helper()
	events, err := f.svc.Events(context.Background(), 500)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func count(events []owner.Event, kind domain.EventKind) int {
	n := 0
	for _, e := range events {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func act() owner.Act {
	subject, _ := id.New()
	return owner.Act{Action: "catalog.price.change", SubjectID: subject, Before: "USD 3.25", After: "USD 2.00"}
}

// reserved is an act that still asks for the PIN (owner.ReservedActs, 2026-09-16): restoring over the shop's books. The
// elevation window is tested through it, because act() above is now allowed at the counter without owner mode.
func reserved() owner.Act {
	subject, _ := id.New()
	return owner.Act{Action: "backups.restore", SubjectID: subject, After: "backup-2026-09-16.mizanbak"}
}

func TestSetUp(t *testing.T) {
	ctx := context.Background()
	f := fake(t)
	if ok, _ := f.svc.IsSetUp(ctx); ok {
		t.Fatal("set up before first run")
	}
	code := f.setUp(t)
	if len(domain.CanonicalRecoveryCode(code)) != domain.RecoveryLength {
		t.Fatalf("recovery code = %q", code)
	}
	if ok, _ := f.svc.IsSetUp(ctx); !ok {
		t.Fatal("not set up after SetUp")
	}
	if _, err := f.svc.SetUp(ctx, "739251"); errs.CodeOf(err) != domain.CodeAlreadySetUp {
		t.Fatalf("a second SetUp = %v", err)
	}
	if count(f.events(t), domain.EventPINSet) != 1 {
		t.Fatal("pin_set was not recorded once")
	}
}

func TestAWeakPINIsRefusedAndNothingIsStored(t *testing.T) {
	f := fake(t)
	if _, err := f.svc.SetUp(context.Background(), "123456"); errs.CodeOf(err) != domain.CodePINWeak {
		t.Fatalf("err = %v", err)
	}
	if ok, _ := f.svc.IsSetUp(context.Background()); ok {
		t.Fatal("a refused PIN created a credential")
	}
}

func TestElevationAndItsWindow(t *testing.T) {
	ctx := context.Background()
	f := fake(t)
	f.setUp(t)

	if err := f.svc.Require(ctx, reserved()); errs.CodeOf(err) != domain.CodeRequired {
		t.Fatalf("a reserved act outside owner mode = %v", err)
	}
	status, err := f.svc.Elevate(ctx, pin)
	if err != nil || status.ElevatedFor != domain.ElevationWindow {
		t.Fatalf("Elevate = %+v, %v", status, err)
	}
	if err := f.svc.Require(ctx, reserved()); err != nil {
		t.Fatalf("a reserved act in owner mode was refused: %v", err)
	}
	if count(f.events(t), domain.EventGuardedAct) != 1 {
		t.Fatal("the act was not recorded")
	}
}

func TestElevationExpiresTwoMinutesAfterEntryNotAfterActivity(t *testing.T) {
	ctx := context.Background()
	f := fake(t)
	f.setUp(t)
	if _, err := f.svc.Elevate(ctx, pin); err != nil {
		t.Fatal(err)
	}
	f.clk.Advance(90 * time.Second)
	if err := f.svc.Require(ctx, reserved()); err != nil {
		t.Fatalf("refused inside the window: %v", err)
	}
	// The act above must NOT have extended the window: 90 s + 31 s is past two minutes from entry.
	f.clk.Advance(31 * time.Second)
	if err := f.svc.Require(ctx, reserved()); errs.CodeOf(err) != domain.CodeRequired {
		t.Fatalf("owner mode outlived its window, extended by activity: %v", err)
	}
	if s, _ := f.svc.Status(ctx); s.ElevatedFor != 0 {
		t.Fatalf("status still elevated: %+v", s)
	}
}

func TestLockEndsOwnerModeAndIsRecordedOnlyWhenItEndsSomething(t *testing.T) {
	ctx := context.Background()
	f := fake(t)
	f.setUp(t)
	if _, err := f.svc.EndElevation(ctx); err != nil {
		t.Fatal(err)
	}
	if count(f.events(t), domain.EventElevationEnded) != 0 {
		t.Fatal("locking while not in owner mode recorded an event")
	}
	if _, err := f.svc.Elevate(ctx, pin); err != nil {
		t.Fatal(err)
	}
	status, err := f.svc.EndElevation(ctx)
	if err != nil || status.ElevatedFor != 0 {
		t.Fatalf("EndElevation = %+v, %v", status, err)
	}
	if err := f.svc.Require(ctx, reserved()); errs.CodeOf(err) != domain.CodeRequired {
		t.Fatal("a reserved act was allowed after Lock")
	}
	if count(f.events(t), domain.EventElevationEnded) != 1 {
		t.Fatal("ending owner mode was not recorded")
	}
}

func remaining(t *testing.T, err error) string {
	t.Helper()
	typed, ok := errs.AsError(err)
	if !ok {
		t.Fatalf("not typed: %v", err)
	}
	return typed.Params["remaining"]
}

func TestLockoutEngagesOnTheFifthFailureDoublesAndResets(t *testing.T) {
	ctx := context.Background()
	f := fake(t)
	f.setUp(t)

	for i, want := range []string{"3", "2", "1", "0"} {
		_, err := f.svc.Elevate(ctx, "739251")
		if errs.CodeOf(err) != domain.CodeWrongPIN || remaining(t, err) != want {
			t.Fatalf("failure %d = %v (remaining %q), want %s", i+1, err, remaining(t, err), want)
		}
	}
	_, err := f.svc.Elevate(ctx, "739251")
	if errs.CodeOf(err) != domain.CodeLocked {
		t.Fatalf("the fifth failure did not lock: %v", err)
	}
	if s, _ := f.svc.Status(ctx); s.LockedFor != 30*time.Second {
		t.Fatalf("locked for %v, want 30s", s.LockedFor)
	}

	// While locked, even the RIGHT PIN is refused — and not counted.
	if _, err := f.svc.Elevate(ctx, pin); errs.CodeOf(err) != domain.CodeLocked {
		t.Fatalf("the right PIN got through a lock: %v", err)
	}

	f.clk.Advance(30 * time.Second)
	if _, err := f.svc.Elevate(ctx, "739251"); errs.CodeOf(err) != domain.CodeLocked {
		t.Fatalf("the sixth failure = %v", err)
	}
	if s, _ := f.svc.Status(ctx); s.LockedFor != time.Minute {
		t.Fatalf("the wait did not double: %v", s.LockedFor)
	}

	f.clk.Advance(time.Minute)
	if _, err := f.svc.Elevate(ctx, pin); err != nil {
		t.Fatalf("the right PIN after the wait: %v", err)
	}
	if _, err := f.svc.EndElevation(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Elevate(ctx, "739251"); remaining(t, err) != "3" {
		t.Fatalf("a success did not reset the counter: %v", err)
	}
	events := f.events(t)
	if count(events, domain.EventElevationFailed) != 7 || count(events, domain.EventLockedOut) != 2 {
		t.Fatalf("failed=%d locked=%d", count(events, domain.EventElevationFailed), count(events, domain.EventLockedOut))
	}
}

func TestAPINTypedInArabicDigitsVerifies(t *testing.T) {
	f := fake(t)
	f.setUp(t)
	if _, err := f.svc.Elevate(context.Background(), "٢٤٦٨١٣"); err != nil {
		t.Fatalf("the PIN in Arabic-Indic digits was refused: %v", err)
	}
}

func TestChangePIN(t *testing.T) {
	ctx := context.Background()
	f := fake(t)
	f.setUp(t)

	if err := f.svc.ChangePIN(ctx, "739251", "581937"); errs.CodeOf(err) != domain.CodeWrongPIN {
		t.Fatalf("a wrong current PIN = %v", err)
	}
	// A weak NEW PIN is a typing matter: refused before an attempt is spent.
	if err := f.svc.ChangePIN(ctx, pin, "111111"); errs.CodeOf(err) != domain.CodePINWeak {
		t.Fatalf("a weak new PIN = %v", err)
	}
	if _, err := f.svc.Elevate(ctx, "000001"); remaining(t, err) != "2" {
		t.Fatalf("the weak new PIN consumed an attempt: %v", err)
	}
	f.clk.Advance(time.Hour)

	if err := f.svc.ChangePIN(ctx, pin, "581937"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Elevate(ctx, pin); errs.CodeOf(err) != domain.CodeWrongPIN {
		t.Fatalf("the old PIN still works: %v", err)
	}
	if _, err := f.svc.Elevate(ctx, "581937"); err != nil {
		t.Fatalf("the new PIN does not: %v", err)
	}
	if count(f.events(t), domain.EventPINChanged) != 1 {
		t.Fatal("the change was not recorded")
	}
}

func TestRecoveryCodeIsSingleUse(t *testing.T) {
	ctx := context.Background()
	f := fake(t)
	code := f.setUp(t)

	// Copied from paper: lower case, no dashes.
	typed := strings.ToLower(strings.ReplaceAll(code, "-", ""))
	fresh, err := f.svc.Recover(ctx, typed, "581937")
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if fresh == code || len(domain.CanonicalRecoveryCode(fresh)) != domain.RecoveryLength {
		t.Fatalf("new code = %q (old %q)", fresh, code)
	}
	if _, err := f.svc.Elevate(ctx, "581937"); err != nil {
		t.Fatalf("the recovered PIN does not work: %v", err)
	}
	if _, err := f.svc.Recover(ctx, code, "739251"); errs.CodeOf(err) != domain.CodeWrongRecoveryCode {
		t.Fatalf("a spent recovery code worked again: %v", err)
	}
	if _, err := f.svc.Recover(ctx, fresh, "739251"); err != nil {
		t.Fatalf("the new recovery code does not work: %v", err)
	}
}

func TestRecoveryIsThrottledLikeThePIN(t *testing.T) {
	ctx := context.Background()
	f := fake(t)
	f.setUp(t)
	for i := 0; i < 4; i++ {
		if _, err := f.svc.Recover(ctx, "AAAA-AAAA-AAAA-AAAA", "581937"); errs.CodeOf(err) != domain.CodeWrongRecoveryCode {
			t.Fatalf("attempt %d = %v", i+1, err)
		}
	}
	if _, err := f.svc.Recover(ctx, "AAAA-AAAA-AAAA-AAAA", "581937"); errs.CodeOf(err) != domain.CodeLocked {
		t.Fatalf("guessing recovery codes never locked: %v", err)
	}
	// One counter for both doors: the PIN is locked too.
	if _, err := f.svc.Elevate(ctx, pin); errs.CodeOf(err) != domain.CodeLocked {
		t.Fatalf("the PIN door stayed open during a recovery lockout: %v", err)
	}
}

func TestNothingWorksBeforeFirstRun(t *testing.T) {
	f := fake(t)
	if _, err := f.svc.Elevate(context.Background(), pin); errs.CodeOf(err) != domain.CodeNotSetUp {
		t.Fatalf("err = %v", err)
	}
}

func TestNoEventEverHoldsACredential(t *testing.T) {
	ctx := context.Background()
	f := fake(t)
	code := f.setUp(t)
	_, _ = f.svc.Elevate(ctx, "739251")
	_, _ = f.svc.Elevate(ctx, pin)
	_ = f.svc.Require(ctx, act())
	_ = f.svc.ChangePIN(ctx, pin, "581937")
	fresh, _ := f.svc.Recover(ctx, code, "905173")

	creds, _, _ := f.store.Credentials(ctx)
	forbidden := []string{pin, "739251", "581937", "905173", code, domain.CanonicalRecoveryCode(code), fresh, "$argon2", creds.PINHash, creds.RecoveryHash}
	for _, e := range f.events(t) {
		for _, field := range []string{e.Action, e.Before, e.After} {
			for _, secret := range forbidden {
				if secret != "" && strings.Contains(field, secret) {
					t.Fatalf("event %s carries a credential: %q", e.Kind, field)
				}
			}
		}
	}
}

func TestAWeakerStoredHashIsUpgradedOnSuccessAndStillVerifies(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFixed(time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC))
	store := ownertest.NewFake()
	weak := crypto.NewArgon2id(crypto.Params{Memory: 32, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	if _, err := owner.NewService(litetest.Immediate{}, store, weak, clk, rand.Reader, litetest.Logger()).SetUp(ctx, pin); err != nil {
		t.Fatal(err)
	}
	before, _, _ := store.Credentials(ctx)

	svc := owner.NewService(litetest.Immediate{}, store, ownertest.Hasher(), clk, rand.Reader, litetest.Logger())
	if _, err := svc.Elevate(ctx, "٢٤٦٨١٣"); err != nil { // typed in Arabic-Indic digits
		t.Fatal(err)
	}
	after, _, _ := store.Credentials(ctx)
	if after.PINHash == before.PINHash {
		t.Fatal("the weaker hash was not upgraded")
	}
	if _, err := svc.EndElevation(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Elevate(ctx, pin); err != nil {
		t.Fatalf("the upgraded hash no longer verifies the PIN: %v", err)
	}
}

// ── against the real database ─────────────────────────────────────────────────────────────────────

func onDatabase(t *testing.T, db *database.Store, clk clock.Clock) *owner.Service {
	t.Helper()
	return owner.NewService(db, sqlite.NewStore(db), ownertest.Hasher(), clk, rand.Reader, litetest.Logger())
}

// TestAWrongPINIsCountedEvenThoughItFails pins the commit-then-refuse shape of the attempt. A refusal
// returned from inside the transaction would roll back the counter with it, and lockout would never engage.
// Only a real transaction can show this; the fake's Immediate cannot roll anything back.
func TestAWrongPINIsCountedEvenThoughItFails(t *testing.T) {
	ctx := context.Background()
	db := litetest.OpenMigrated(t)
	clk := clock.NewFixed(time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC))
	svc := onDatabase(t, db, clk)
	if _, err := svc.SetUp(ctx, pin); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		_, _ = svc.Elevate(ctx, "739251")
	}
	if s, _ := svc.Status(ctx); s.LockedFor != 30*time.Second {
		t.Fatalf("five wrong PINs against the database left LockedFor = %v", s.LockedFor)
	}
}

func TestLockoutSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	db := litetest.OpenMigrated(t)
	clk := clock.NewFixed(time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC))
	first := onDatabase(t, db, clk)
	if _, err := first.SetUp(ctx, pin); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		_, _ = first.Elevate(ctx, "739251")
	}

	// A new service over the same database is what a relaunch is.
	restarted := onDatabase(t, db, clk)
	if _, err := restarted.Elevate(ctx, pin); errs.CodeOf(err) != domain.CodeLocked {
		t.Fatalf("relaunching reset the lockout: %v", err)
	}
}

func TestOwnerModeDoesNotSurviveRestart(t *testing.T) {
	ctx := context.Background()
	db := litetest.OpenMigrated(t)
	clk := clock.NewFixed(time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC))
	first := onDatabase(t, db, clk)
	if _, err := first.SetUp(ctx, pin); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Elevate(ctx, pin); err != nil {
		t.Fatal(err)
	}
	if s, _ := onDatabase(t, db, clk).Status(ctx); s.ElevatedFor != 0 {
		t.Fatal("owner mode was persisted; it must live in memory only")
	}
}

func TestARolledBackActLeavesNoRecordOfIt(t *testing.T) {
	ctx := context.Background()
	db := litetest.OpenMigrated(t)
	svc := onDatabase(t, db, clock.System())
	if _, err := svc.SetUp(ctx, pin); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Elevate(ctx, pin); err != nil {
		t.Fatal(err)
	}

	failed := errors.New("the write after the guard failed")
	err := db.Do(ctx, func(ctx context.Context) error {
		if err := svc.Require(ctx, act()); err != nil {
			return err
		}
		return failed
	})
	if !errors.Is(err, failed) {
		t.Fatalf("err = %v", err)
	}
	events, _ := svc.Events(ctx, 100)
	if count(events, domain.EventGuardedAct) != 0 {
		t.Fatal("an act that rolled back is recorded as having happened")
	}
}

func TestAFailedSetUpLeavesNoCredential(t *testing.T) {
	ctx := context.Background()
	db := litetest.OpenMigrated(t)
	svc := owner.NewService(db, sqlite.NewStore(db), ownertest.Hasher(), clock.System(), exhausted{}, litetest.Logger())
	if _, err := svc.SetUp(ctx, pin); err == nil {
		t.Fatal("SetUp succeeded with no randomness for the recovery code")
	}
	if ok, _ := svc.IsSetUp(ctx); ok {
		t.Fatal("a failed SetUp left a credential")
	}
}

type exhausted struct{}

func (exhausted) Read([]byte) (int, error) { return 0, errors.New("no entropy") }

// TestOwnerModeIsNotGrantedWhenTheAttemptFailsToSave pins the second defect found reviewing this service
// before its tests existed: owner mode was switched on INSIDE the attempt's transaction, so a credential
// write that then failed still left the application elevated.
func TestOwnerModeIsNotGrantedWhenTheAttemptFailsToSave(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFixed(time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC))
	store := ownertest.NewFake()
	svc := owner.NewService(litetest.Immediate{}, store, ownertest.Hasher(), clk, rand.Reader, litetest.Logger())
	if _, err := svc.SetUp(ctx, pin); err != nil {
		t.Fatal(err)
	}
	store.FailUpdates()
	if _, err := svc.Elevate(ctx, pin); !errors.Is(err, ownertest.ErrInjected) {
		t.Fatalf("Elevate over a failing store = %v", err)
	}
	if err := svc.Require(ctx, reserved()); errs.CodeOf(err) != domain.CodeRequired {
		t.Fatalf("owner mode was granted although the attempt did not save: %v", err)
	}
}

func TestEveryGuardedReadIsOpenAndRecordsNothing(t *testing.T) {
	ctx := context.Background()
	f := fake(t)
	f.setUp(t)
	before := len(f.events(t))
	// Since 2026-09-16 the figures are open at the counter: no PIN to read a cost, a profit or the drawer.
	for range 2 {
		if !f.svc.Allowed(ctx) {
			t.Fatal("a guarded read was refused outside owner mode")
		}
	}
	if _, err := f.svc.Elevate(ctx, pin); err != nil {
		t.Fatal(err)
	}
	if !f.svc.Allowed(ctx) {
		t.Fatal("a guarded read was refused in owner mode")
	}
	// The elevation itself, and not one event more: looking is not an act.
	if got := len(f.events(t)); got != before+1 {
		t.Fatalf("events = %d, want %d — a guarded read was recorded in the owner's history", got, before+1)
	}
}

// TestOnlyRestoreAndBringingInABackupStillAskForThePIN pins the owner's decision of 2026-09-16: a single-computer shop
// with no login should not be stopped for a PIN to void a receipt or read a report. What stays reserved is the pair that
// replaces the books wholesale. Every act is recorded either way.
func TestOnlyRestoreAndBringingInABackupStillAskForThePIN(t *testing.T) {
	ctx := context.Background()
	f := fake(t)
	f.setUp(t)
	opened := []string{"sales.sale.void", "sales.discount", "customers.write_off", "customers.refund",
		"customers.opening", "customers.reverse", "stock.adjust.lower", "stock.cost.correct", "stock.receipt.reverse",
		"cashbook.withdrawal", "cashbook.expense", "cashbook.reverse", "catalog.price.change", "catalog.product.deactivate",
		"catalog.import", "fx.rate.set", "fx.mode.set", "printers.settings", "sales.cash_note.set",
		"backups.outside_folder", "backups.save_copy", "support.database"}
	for _, action := range opened {
		if err := f.svc.Require(ctx, owner.Act{Action: action}); err != nil {
			t.Fatalf("%s asked for the PIN: %v", action, err)
		}
	}
	if got := count(f.events(t), domain.EventGuardedAct); got != len(opened) {
		t.Fatalf("acts recorded = %d, want %d — the history is what survives the gate", got, len(opened))
	}
	for _, action := range []string{"backups.restore", "backups.import"} {
		if err := f.svc.Require(ctx, owner.Act{Action: action}); errs.CodeOf(err) != domain.CodeRequired {
			t.Fatalf("%s was allowed without the PIN: %v", action, err)
		}
	}
}
