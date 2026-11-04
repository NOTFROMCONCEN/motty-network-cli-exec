package workbench

import (
	"motty/internal/ai/gate"
	"motty/internal/netcli"
)

// How an assistant command runs in a terminal is a property of the session's
// execution environment, not of the connection: an SSH host can speak a device
// CLI and a Telnet host can speak a login shell. The mode is stored per host
// and decided once per command, at the one place commands are typed.

// execMode selects the backend beginExec types a command into.
type execMode uint8

const (
	// shellMode keeps the existing behavior: the POSIX capture wrapper, the
	// nonce marker, the real exit status.
	shellMode execMode = iota
	// cliMode sends one raw command line to a network device CLI and watches
	// for the device prompt to come back.
	cliMode
)

// resolveExecMode decides a host's mode. The user's setting wins; without one,
// the transports that usually face a device console (Telnet, a serial line)
// default to the network CLI and everything else stays a shell, so existing
// SSH and local terminals keep their behavior after an upgrade.
func resolveExecMode(host Host) execMode {
	switch host.ExecMode {
	case gate.ExecModeShell:
		return shellMode
	case gate.ExecModeNetworkCLI:
		return cliMode
	}
	if host.Protocol == protocolTelnet || host.Protocol == protocolSerial {
		return cliMode
	}
	return shellMode
}

// sessionExecMode is the mode of one session's terminal. A local terminal is
// always a shell, whatever the pseudo-host's fields say.
func (session *Session) sessionExecMode() execMode {
	if session.isLocal() || session.host.isDesktop() {
		return shellMode
	}
	return resolveExecMode(session.host)
}

// execModeName is how the mode is named in list_sessions and in saved state.
func (session *Session) execModeName() string {
	if session.sessionExecMode() == cliMode {
		return gate.ExecModeNetworkCLI
	}
	return gate.ExecModeShell
}

// cliPlatformName is the network CLI platform of a session, "" on a shell
// session. An unknown saved id reads as the generic table.
func (session *Session) cliPlatformName() string {
	if session.sessionExecMode() != cliMode {
		return ""
	}
	if p := netcli.Table(session.host.CLIPlatform); p != nil {
		return p.ID
	}
	return ""
}

// cliPlatform is the network CLI platform table of a session's host.
func (host *Host) cliPlatform() *netcli.Platform { return netcli.Table(host.CLIPlatform) }
