package bindings

import (
	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/buildinfo"
)

// System exposes build and runtime metadata.
type System struct{ graph }

// Health returns build metadata.
//
// Guarded like every other graph-backed binding even though buildinfo needs no graph: an
// ungated method would report a healthy application while the database was mid-restore, which
// is exactly the wrong answer to "is Mizan working?". Boot.Status is the binding that reports
// startup state.
func (s *System) Health() envelope.Result[buildinfo.Info] {
	if _, ok := s.resolve(); !ok {
		return envelope.Fail[buildinfo.Info](notReady())
	}
	return envelope.Ok(buildinfo.Current())
}
