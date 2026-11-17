package workbench

import "testing"

// The menu bar speaks the application's language, shows the shortcuts the user
// kept, and disables what needs a session when there is none.
func TestMenuBarFollowsTheShortcutTableAndSession(t *testing.T) {
	state := newTestState(t, Options{})
	menus := state.MenuBar()
	if len(menus) != 3 || menus[0].Title == "" || menus[1].Title == "" || menus[2].Title == "" {
		t.Fatalf("menus = %+v", menus)
	}
	newWindow := menus[0].Items[0]
	if newWindow.Title == "" || newWindow.OnSelect == nil || newWindow.Disabled {
		t.Fatalf("New Window = %+v", newWindow)
	}
	edit := menus[1]
	if !edit.Items[0].Disabled || !edit.Items[1].Disabled || !edit.Items[3].Disabled {
		t.Fatalf("Copy, Paste and Find must need a session: %+v", edit.Items)
	}
	if menus[2].Items[1].Disabled {
		t.Fatalf("the assistant needs no session: %+v", menus[2].Items[1])
	}
	// A separator carries no title and no action.
	if !edit.Items[2].Separator || edit.Items[2].OnSelect != nil {
		t.Fatalf("separator = %+v", edit.Items[2])
	}
}

// A native menu runs the entry outside the window's event handlers. The Viva
// pin takes OnSelect as a plain func with no result channel, so the action's
// own effect is the observable: Find opens the bar.
func TestMenuBarFindOpensTheSearch(t *testing.T) {
	state, _, _, _ := liveSession(t)
	state.MenuBar()[1].Items[3].OnSelect()
	if session := &state.sessions[0]; !session.term.find.open {
		t.Fatal("the menu action did not open the bar")
	}
}
