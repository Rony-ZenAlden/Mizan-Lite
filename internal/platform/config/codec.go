package config

import (
	"encoding/json"
	"time"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/kernel/money"
	"github.com/mizan-erp/mizan/internal/kernel/round"
)

// Values are stored as JSON text in settings.setting_value.
//
// Every codec below decodes into a CONCRETE typed target, never into `any`. That is not a
// style preference: encoding/json decodes an untyped number into float64, so a single
// `any` target anywhere in this file would silently route money and large integers
// through binary floating point and corrupt them beyond 2^53. The typed targets make that
// impossible by construction.

// moneyWire is the on-disk shape of a Money value.
//
// The currency is stored in full — code, decimals, and rounding mode — rather than as a
// bare code, because a Money value is meaningless without them and the currency table
// does not exist until Step 0.9. A setting written today must still decode correctly if
// the currency's configuration is later edited, so the snapshot is self-contained.
type moneyWire struct {
	Amount   int64  `json:"amount"` // minor units; integer, never a decimal fraction
	Currency string `json:"currency"`
	Decimals uint8  `json:"decimals"`
	Rounding string `json:"rounding"`
}

func encodeString(v string) (string, error) { return marshal(v) }

func decodeString(s string) (string, error) {
	var out string
	err := unmarshal(s, &out)
	return out, err
}

func encodeBool(v bool) (string, error) { return marshal(v) }

func decodeBool(s string) (bool, error) {
	var out bool
	err := unmarshal(s, &out)
	return out, err
}

func encodeInt(v int64) (string, error) { return marshal(v) }

func decodeInt(s string) (int64, error) {
	var out int64
	err := unmarshal(s, &out)
	return out, err
}

// Duration is stored as an integer count of nanoseconds rather than Go's "1h30m" text, so
// a future non-Go reader of this database is not required to implement Go's duration
// grammar to understand a value.
func encodeDuration(v time.Duration) (string, error) { return marshal(int64(v)) }

func decodeDuration(s string) (time.Duration, error) {
	var ns int64
	if err := unmarshal(s, &ns); err != nil {
		return 0, err
	}
	return time.Duration(ns), nil
}

func encodeID(v id.ID) (string, error) { return marshal(v.String()) }

func decodeID(s string) (id.ID, error) {
	var raw string
	if err := unmarshal(s, &raw); err != nil {
		return "", err
	}
	if raw == "" {
		return "", nil
	}
	return id.Parse(raw)
}

func encodeMoney(v money.Money) (string, error) {
	if !v.Valid() {
		return "", errs.Validation(CodeInvalidValue, "money value has no currency")
	}
	c := v.Currency()
	return marshal(moneyWire{
		Amount:   v.Minor(),
		Currency: c.Code(),
		Decimals: c.Decimals(),
		Rounding: c.Rounding().String(),
	})
}

func decodeMoney(s string) (money.Money, error) {
	var w moneyWire
	if err := unmarshal(s, &w); err != nil {
		return money.Money{}, err
	}
	mode, ok := round.ParseMode(w.Rounding)
	if !ok {
		return money.Money{}, errs.Validation(CodeInvalidValue,
			"unknown rounding mode in stored money value").WithParam("rounding", w.Rounding)
	}
	cur, err := money.NewCurrency(w.Currency, w.Decimals, mode)
	if err != nil {
		return money.Money{}, errs.Wrap(err, errs.CategoryValidation, CodeInvalidValue,
			"stored money value has an invalid currency")
	}
	return money.FromMinor(cur, w.Amount), nil
}

func marshal(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", errs.Wrap(err, errs.CategoryValidation, CodeInvalidValue, "encoding setting value")
	}
	return string(b), nil
}

func unmarshal(s string, target any) error {
	if err := json.Unmarshal([]byte(s), target); err != nil {
		return errs.Wrap(err, errs.CategoryValidation, CodeInvalidValue, "decoding setting value")
	}
	return nil
}

// encodeAny is the dynamic path used by Set and by the generated settings UI, where the
// value arrives as `any` from outside the type system.
//
// It is strict: the Go type must match the declared ValueType exactly. int is accepted
// for TypeInt as a convenience for literals, but float is never accepted for anything —
// a float reaching a money or int setting is a bug, and silently truncating it would hide
// the bug at exactly the point it costs money.
func encodeAny(vt ValueType, v any) (string, error) {
	switch vt {
	case TypeString, TypeEnum:
		s, ok := v.(string)
		if !ok {
			return "", typeMismatch(vt, v)
		}
		return encodeString(s)
	case TypeBool:
		b, ok := v.(bool)
		if !ok {
			return "", typeMismatch(vt, v)
		}
		return encodeBool(b)
	case TypeInt:
		switch n := v.(type) {
		case int64:
			return encodeInt(n)
		case int:
			return encodeInt(int64(n))
		default:
			return "", typeMismatch(vt, v)
		}
	case TypeDuration:
		d, ok := v.(time.Duration)
		if !ok {
			return "", typeMismatch(vt, v)
		}
		return encodeDuration(d)
	case TypeID:
		i, ok := v.(id.ID)
		if !ok {
			return "", typeMismatch(vt, v)
		}
		return encodeID(i)
	case TypeMoney:
		m, ok := v.(money.Money)
		if !ok {
			return "", typeMismatch(vt, v)
		}
		return encodeMoney(m)
	case TypeJSON:
		return marshal(v)
	default:
		return "", errs.Validation(CodeInvalidValue, "unknown value type").
			WithParam("type", string(vt))
	}
}

func typeMismatch(vt ValueType, v any) error {
	return errs.Validation(CodeInvalidValue, "value does not match the declared setting type").
		WithParam("expected", string(vt)).
		WithParam("got", goTypeName(v))
}

func goTypeName(v any) string {
	switch v.(type) {
	case nil:
		return "nil"
	case string:
		return "string"
	case bool:
		return "bool"
	case int:
		return "int"
	case int64:
		return "int64"
	case float32, float64:
		return "float"
	case time.Duration:
		return "time.Duration"
	case id.ID:
		return "id.ID"
	case money.Money:
		return "money.Money"
	default:
		return "other"
	}
}
