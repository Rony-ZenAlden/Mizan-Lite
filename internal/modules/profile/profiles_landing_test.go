package profile_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mizan-erp/mizan/internal/platform/ui"
)

// TestEveryShippedProfileSetsSettingsThatExist
//
// # The defect this catches
//
// A business profile is a JSON file that writes SETTINGS. A key with a typo in it, or a value
// outside a setting's enum, is not a compile error and not a startup error — the profile applies,
// the write is rejected or ignored, and the shop silently gets the default instead of the thing
// the profile promised.
//
// For `ui.landing` that means opening on the dashboard when the profile said the till, which
// reads as "the setting does not work" rather than as "the seed file has a typo in it".
//
// Read from the shipped FILES rather than from a list, because the files are what installs.
func TestEveryShippedProfileSetsSettingsThatExist(t *testing.T) {
	dir := filepath.Join("seeds", "business_profiles")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	// A scan that found nothing would pass while checking nothing — 7.6's D162.
	if len(entries) < 3 {
		t.Fatalf("only %d profiles found in %s", len(entries), dir)
	}

	landings := map[string]bool{
		ui.LandingOverview: true, ui.LandingTill: true, ui.LandingInvoices: true,
	}

	var withLanding int
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(dir, entry.Name()))
		if readErr != nil {
			t.Fatalf("reading %s: %v", entry.Name(), readErr)
		}

		var profile struct {
			Code     string            `json:"code"`
			NameKey  string            `json:"name_key"`
			Settings map[string]any    `json:"settings"`
			Flags    map[string]any    `json:"flags"`
			Extra    map[string]any    `json:"-"`
			Names    map[string]string `json:"-"`
		}
		if err = json.Unmarshal(raw, &profile); err != nil {
			t.Errorf("%s is not valid JSON: %v", entry.Name(), err)
			continue
		}

		if profile.Code == "" {
			t.Errorf("%s has no code", entry.Name())
		}
		// The label is a KEY, so a profile is named in the user's language rather than making
		// English the source language for a list a shopkeeper reads.
		if !strings.HasPrefix(profile.NameKey, "business_profile.") {
			t.Errorf("%s has name_key %q, which the catalogue will not resolve",
				entry.Name(), profile.NameKey)
		}

		landing, ok := profile.Settings[ui.LandingSettingKey]
		if !ok {
			continue
		}
		withLanding++
		value, isString := landing.(string)
		if !isString {
			t.Errorf("%s sets %s to a %T, which is not a route", entry.Name(),
				ui.LandingSettingKey, landing)
			continue
		}
		if !landings[value] {
			t.Errorf("%s opens on %q, which is not one of the screens %s allows",
				entry.Name(), value, ui.LandingSettingKey)
		}
	}

	// The three workspaces are the point of the feature. If none of the shipped profiles sets a
	// landing, the check above passed by having nothing to check.
	if withLanding < 2 {
		t.Errorf("only %d profiles choose a landing screen; the workspaces feature is not "+
			"actually exercised by anything that ships", withLanding)
	}
}
