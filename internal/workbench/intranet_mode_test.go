package workbench

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/configstore"
	"motty/internal/netguard"
)

// paintedImages is the bitmaps one paint drew.
func paintedImages(state *State, width, height float32) []gfx.Bitmap {
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: width, Height: height})
	images := canvas.Images()
	bitmaps := make([]gfx.Bitmap, 0, len(images))
	for _, image := range images {
		bitmaps = append(bitmaps, image.Bitmap)
	}
	return bitmaps
}

func paintedLockup(state *State, width, height float32) bool {
	for _, bitmap := range paintedImages(state, width, height) {
		if bitmap.ID == lockupDark().ID || bitmap.ID == lockupLight().ID {
			return true
		}
	}
	return false
}

// TestTheIntranetModeToggleSitsInSettingsAndPersists walks the switch: it is a
// row on the Security page, turning it on arms the process-wide gate at once,
// it hides both logos, and it survives a restart.
func TestTheIntranetModeToggleSitsInSettingsAndPersists(t *testing.T) {
	_, options := newWorld(t)
	options.Invalidate = func() {}
	state := newTestState(t, options)
	defer netguard.SetEnabled(false)
	if state.intranetMode || netguard.Enabled() {
		t.Fatal("the intranet mode must default to off")
	}

	// With the mode off, the sidebar keeps its logo.
	if !paintedLockup(state, 1200, 900) {
		t.Fatal("the sidebar painted no logo while the mode is off")
	}

	state.Click(hitSettings)
	state.selectSettingsCategory(settingsSecurity)
	if !contains(paintedAt(state, 1400, 3400), state.messages.SettingsIntranetMode) {
		t.Fatal("the intranet mode row is missing from the Security page")
	}

	// Turning it on arms the gate and hides the sidebar's logo slot.
	state.Click(hitSettingsIntranetMode)
	if !state.intranetMode || !netguard.Enabled() {
		t.Fatal("the switch did not turn the mode on")
	}
	if paintedLockup(state, 1200, 900) {
		t.Fatal("the sidebar painted a logo while the mode is on")
	}

	// The About page keeps the wordmark hidden too, while the page itself stays.
	state.selectSettingsCategory(settingsAbout)
	if paintedLockup(state, 1400, 3400) {
		t.Fatal("the About page painted the wordmark while the mode is on")
	}
	if !contains(paintedAt(state, 1400, 3400), state.messages.About.Tagline) {
		t.Fatal("the About page lost its tagline with the logo hidden")
	}

	// The mode belongs to this computer: it is saved and comes back.
	state.save()
	restored := newTestState(t, state.options)
	if !restored.intranetMode || !netguard.Enabled() {
		t.Fatal("the restored state lost the intranet mode")
	}

	// Turning it off hands the gate back.
	restored.Click(hitSettingsIntranetMode)
	if restored.intranetMode || netguard.Enabled() {
		t.Fatal("the switch did not turn the mode off")
	}
}

// TestTheIntranetModeStaysOnThisDevice keeps the choice out of the shared
// records. Turning the mode on here must never turn it on for another device:
// each computer decides for itself whether it reaches the internet.
func TestTheIntranetModeStaysOnThisDevice(t *testing.T) {
	if syncedSettings["intranet_mode"] {
		t.Fatal("the intranet mode is a synced setting")
	}
	path := filepath.Join(t.TempDir(), "config.db")
	want := richState()
	want.IntranetMode = true
	if err := saveState(path, want); err != nil {
		t.Fatal(err)
	}
	got, report, err := loadState(path)
	if err != nil || report.outcome != loadOK {
		t.Fatalf("load: %+v, %v", report, err)
	}
	if !got.IntranetMode {
		t.Fatal("the saved mode did not come back")
	}
	store, err := configstore.Open(path, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entries, local, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if bytes.Contains(entry.Payload, []byte("intranet_mode")) {
			t.Fatalf("the mode became a shared record: %s/%s", entry.Kind, entry.ID)
		}
	}
	if !bytes.Contains(local[localKey], []byte(`"intranet_mode":true`)) {
		t.Fatalf("the mode is not in the local state: %s", local[localKey])
	}
}
