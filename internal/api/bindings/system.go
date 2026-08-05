package bindings

import (
	"github.com/mizan-erp/mizan/internal/api/envelope"
	"github.com/mizan-erp/mizan/internal/api/policy"
	"github.com/mizan-erp/mizan/internal/buildinfo"
	"github.com/mizan-erp/mizan/internal/modules/identity"
)

// systemPolicies declares what System's methods require.
func systemPolicies() map[string]policy.Policy {
	return map[string]policy.Policy{
		// Any signed-in user may see the build version — it is on the About screen, and
		// hiding it helps nobody. It still requires a SESSION.
		"Health": policy.Requires(identity.PermUserView),
	}
}

// System exposes build and runtime metadata.
type System struct{ graph }

// Health returns build metadata.
//
// Guarded like every other graph-backed binding even though buildinfo needs no graph: an
// ungated method would report a healthy application while the database was mid-restore, which
// is exactly the wrong answer to "is Mizan working?". Boot.Status is the binding that reports
// startup state.
func (s *System) Health() envelope.Result[buildinfo.Info] {
	if _, _, err := s.guard("Health"); err != nil {
		return envelope.Fail[buildinfo.Info](err)
	}
	return envelope.Ok(buildinfo.Current())
}
