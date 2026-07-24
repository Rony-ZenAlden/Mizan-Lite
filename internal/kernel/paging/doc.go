// Package paging provides the shared read-side shapes: Page, Sort, Filter, and
// PageResult[T]. Every list/query endpoint returns these, so the frontend has one
// pagination and filtering contract across all modules.
//
// See docs/architecture/ARCHITECTURE_v1.md §27 (read models).
package paging
