// Package clock abstracts time. Domain and application code take a Clock rather than
// calling time.Now(), so behaviour is deterministic under test. System and Fixed
// implementations are provided in Step 0.2.
//
// See docs/architecture/ARCHITECTURE_v1.md §7.5.
package clock
