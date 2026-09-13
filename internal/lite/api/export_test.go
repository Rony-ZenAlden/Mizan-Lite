package api

import (
	"context"

	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/lite/bootstrap"
)

// CallForTest runs fn through the same guard every binding uses.
func CallForTest[T any](s *Set, method string, fn func(context.Context, *bootstrap.App) (T, error)) envelope.Result[T] {
	return call(s.core, method, fn)
}
