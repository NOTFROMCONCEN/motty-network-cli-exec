package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// The window's wheel travels through scrollWindow, whose page routing hands
// the event to the page's scroll areas — and the settings declare theirs with
// offsets the page keeps between paints. An older regression let the page's
// areas take the wheel while the settings kept declaring the app's offset on
// top of it, so every notch was thrown away before the next paint: the wheel
// and the scrollbar both went dead. These tests drive the live entry point
// (scrollWindow, as config.OnScroll does) over every category and window size.
func TestTheWindowWheelScrollsEverySettingsCategory(t *testing.T) {
	sizes := []gfx.Size{
		{Width: 1000, Height: 520},
		{Width: 1280, Height: 720},
		{Width: 1920, Height: 1080},
	}
	for _, size := range sizes {
		state := newTestState(t, testOptions(t))
		state.Click(hitSettings)
		for category := settingsCategory(0); category < settingsCategoryCount; category++ {
			state.selectSettingsCategory(category)
			var canvas gfx.Canvas
			state.Paint(&canvas, size)
			if state.settingsContent != state.settingsContent {
				t.Fatalf("%v category %d: settingsContent is NaN", size, category)
			}
			if state.settingsContent <= size.Height {
				continue // nothing to scroll
			}
			before := state.settingsScroll
			// A wheel notch, not a trackpad's precise delta, over the page's
			// scrolling column: right of the category list, half way down.
			bounds := state.settingsWorkspaceBounds(gfx.Rect{Width: size.Width, Height: size.Height})
			state.scrollWindow(gfx.ScrollEvent{
				DeltaY: 120, Precise: false, Viewport: size,
				X: bounds.X + bounds.Width*0.6, Y: bounds.Y + bounds.Height*0.5,
			})
			// The page reads the settled offset back on the next paint, as a
			// live window always paints one.
			var again gfx.Canvas
			state.Paint(&again, size)
			if state.settingsScroll != state.settingsScroll {
				t.Fatalf("%v category %d: settingsScroll became NaN", size, category)
			}
			if state.settingsScroll <= before {
				t.Fatalf("%v category %d: the wheel did not move the page: %.0f -> %.0f (content %.0f pageView %.0f)",
					size, category, before, state.settingsScroll, state.settingsContent, state.settings.pageView)
			}
			// And the paint agrees with the app's offset: painting again moves
			// nothing, as the wheel and the declaration must not fight.
			var third gfx.Canvas
			state.Paint(&third, size)
			if state.settingsScroll != state.settingsScroll {
				t.Fatalf("%v category %d: settingsScroll became NaN", size, category)
			}
			scrolled := state.settingsScroll
			var fourth gfx.Canvas
			state.Paint(&fourth, size)
			if state.settingsScroll != scrolled {
				t.Fatalf("%v category %d: the paint moved the page from %.0f to %.0f",
					size, category, scrolled, state.settingsScroll)
			}
		}
	}
}
