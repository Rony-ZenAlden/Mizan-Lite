package envelope_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/kernel/errs"
)

func TestOkCarriesData(t *testing.T) {
	res := envelope.Ok([]string{"a", "b"})
	if !res.OK || len(res.Data) != 2 || res.Error != nil {
		t.Fatalf("Ok = %+v", res)
	}
}

func TestFailMapsCodeAndParams(t *testing.T) {
	err := errs.Conflict("migrate.database_too_new", "developer-facing text").
		WithParam("current", "5").WithParam("target", "3")

	res := envelope.Fail[string](err)
	if res.OK || res.Error == nil {
		t.Fatalf("Fail = %+v", res)
	}
	if res.Error.Code != "migrate.database_too_new" {
		t.Errorf("Code = %q", res.Error.Code)
	}
	if res.Error.MessageKey != res.Error.Code {
		t.Errorf("MessageKey %q should mirror Code %q", res.Error.MessageKey, res.Error.Code)
	}
	if res.Error.Params["current"] != "5" || res.Error.Params["target"] != "3" {
		t.Errorf("Params = %v", res.Error.Params)
	}
}

func TestNoEnglishProseCrossesTheBoundary(t *testing.T) {
	// The rule this package exists to enforce (§5.4, §22.2). Translated text coming from the
	// backend would leave every already-rendered error stuck in the old language after a
	// switch — which is what makes "no restart on language change" only half true.
	err := errs.Validation("config.invalid_value",
		"this developer-facing sentence must never reach a customer").
		WithField("amount", "config.invalid_value", "another English sentence")

	encoded, marshalErr := json.Marshal(envelope.Fail[string](err))
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	body := string(encoded)
	for _, prose := range []string{
		"this developer-facing sentence must never reach a customer",
		"another English sentence",
	} {
		if strings.Contains(body, prose) {
			t.Errorf("developer prose crossed the API boundary:\n%s", body)
		}
	}
	if !strings.Contains(body, "config.invalid_value") {
		t.Errorf("the code did not cross the boundary:\n%s", body)
	}
}

func TestUntypedErrorBecomesAGenericCodeWithoutItsMessage(t *testing.T) {
	// An unexpected error's text is a Go string written for a developer. Leaking it would put
	// untranslated — and possibly sensitive — English in front of a customer. The real text
	// goes to the log.
	err := errors.New("sql: connection refused to /Users/someone/private/path.db")

	res := envelope.Fail[int](err)
	if res.Error == nil || res.Error.Code != envelope.CodeInternal {
		t.Fatalf("Error = %+v, want the generic internal code", res.Error)
	}
	encoded, _ := json.Marshal(res)
	if strings.Contains(string(encoded), "private/path.db") {
		t.Errorf("an internal error leaked its message:\n%s", encoded)
	}
}

func TestFieldErrorsSurvive(t *testing.T) {
	err := errs.Validation("config.invalid_value", "dev text").
		WithField("email", "validation.required", "dev text").
		WithField("age", "validation.range", "dev text")

	res := envelope.Fail[string](err)
	if len(res.Error.Fields) != 2 {
		t.Fatalf("fields = %d, want 2", len(res.Error.Fields))
	}
	for _, f := range res.Error.Fields {
		if f.Field == "" || f.Code == "" || f.MessageKey != f.Code {
			t.Errorf("field error is incomplete: %+v", f)
		}
	}
}

func TestNilErrorProducesNoAPIError(t *testing.T) {
	if got := envelope.FromError(nil); got != nil {
		t.Errorf("FromError(nil) = %+v, want nil", got)
	}
}

func TestSuccessJSONOmitsTheErrorField(t *testing.T) {
	// The frontend branches on `ok`; a null error field on every success is noise on a bridge
	// that serialises every call.
	encoded, err := json.Marshal(envelope.Ok(42))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "error") {
		t.Errorf("success payload carries an error field: %s", encoded)
	}
}
