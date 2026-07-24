// Package locale defines Locale (ar, en) and TextDirection (RTL, LTR) with a fallback
// chain (ar → en → key). Locale is carried in the app context and changeable at
// runtime with no restart.
//
// See docs/architecture/ARCHITECTURE_v1.md §22.
package locale
