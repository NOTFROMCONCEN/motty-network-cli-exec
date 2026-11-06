package workbench

import (
	"fmt"
	"log"
	"path/filepath"

	"motty/internal/netcli"
)

// netcliPlatformPath is the user platform file beside the state store. It is
// read before the saved hosts are decoded: a host may name a table this file
// adds, and an unknown id must not make the host look damaged.
func netcliPlatformPath(statePath string) string {
	return filepath.Join(filepath.Dir(statePath), "netcli-platforms.yaml")
}

// loadPlatformTables installs the user's tables. A missing file is normal; a
// broken one leaves the built-in tables in place and says so once.
func (state *State) loadPlatformTables() {
	count, err := netcli.LoadFile(netcliPlatformPath(state.options.StatePath))
	if err != nil {
		log.Printf("motty: network platform tables: %v", err)
		state.showActionWarning(state.messages.Tools.CLIPlatformFileTitle, state.messages.Tools.CLIPlatformFileFailed)
		return
	}
	if count > 0 {
		log.Printf("motty: loaded %d user network platform table(s)", count)
	}
}

// reloadPlatformTables re-reads the file after the user edited it. Removing a
// table an open host names is safe: the generic table answers until it is back.
func (state *State) reloadPlatformTables() {
	title := state.messages.Tools.CLIPlatformFileTitle
	count, err := netcli.LoadFile(netcliPlatformPath(state.options.StatePath))
	if err != nil {
		log.Printf("motty: network platform tables: %v", err)
		state.showActionWarning(title, state.messages.Tools.CLIPlatformFileFailed)
		return
	}
	state.showActionInfo(title, fmt.Sprintf(state.messages.Tools.CLIPlatformReloadedFmt, count))
}
