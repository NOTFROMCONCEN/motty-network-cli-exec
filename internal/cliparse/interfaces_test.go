package cliparse

import "testing"

func TestParseHuaweiInterfaceBrief(t *testing.T) {
	output := "Interface                   PHY     Protocol  InUti OutUti   inErrors  outErrors\n" +
		"GigabitEthernet0/0/1        up      up           0%     0%          0          0\n" +
		"GigabitEthernet0/0/2        down    down         0%     0%          0          0\n" +
		"GigabitEthernet0/0/3        *down   down         0%     0%          0          0\n" +
		"Vlanif10                    up      up           --     --          0          0\n"
	table := ParseInterfaces(output)
	if len(table.Warnings) != 0 {
		t.Fatalf("warnings = %v", table.Warnings)
	}
	if len(table.Rows) != 4 {
		t.Fatalf("rows = %d; want 4", len(table.Rows))
	}
	want := []string{"GigabitEthernet0/0/1", "up", "up", "0%", "0%", "0", "0"}
	for index, value := range want {
		if table.Rows[0][index] != value {
			t.Fatalf("row[0][%d] = %q; want %q", index, table.Rows[0][index], value)
		}
	}
	if table.Rows[2][0] != "GigabitEthernet0/0/3" {
		t.Fatalf("a down-interface marker was kept in the name: %q", table.Rows[2][0])
	}
}

func TestParseCiscoInterfaceBrief(t *testing.T) {
	output := "Interface              IP-Address      OK? Method Status                Protocol\n" +
		"GigabitEthernet0/0     10.0.0.1        YES NVRAM  up                    up\n" +
		"GigabitEthernet0/1     unassigned      YES unset  administratively down down\n"
	table := ParseInterfaces(output)
	if len(table.Rows) != 2 {
		t.Fatalf("rows = %d; want 2 (%v)", len(table.Rows), table.Warnings)
	}
	if got := table.Rows[0]; got[1] != "10.0.0.1" || got[4] != "up" || got[5] != "up" {
		t.Fatalf("first row = %v", got)
	}
	if got := table.Rows[1]; got[4] != "administratively down" || got[5] != "down" {
		t.Fatalf("admin-down row = %v", got)
	}
}

func TestParseRuijieSpacedInterfaceName(t *testing.T) {
	output := "Interface              IP-Address      Status                Protocol\n" +
		"GigabitEthernet 0/1    10.0.0.2        up                    up\n"
	table := ParseInterfaces(output)
	if len(table.Rows) != 1 {
		t.Fatalf("rows = %d; want 1 (%v)", len(table.Rows), table.Warnings)
	}
	if table.Rows[0][0] != "GigabitEthernet 0/1" {
		t.Fatalf("name = %q", table.Rows[0][0])
	}
}

func TestParseInterfacesWithoutHeader(t *testing.T) {
	table := ParseInterfaces("no table here\n")
	if len(table.Rows) != 0 || len(table.Warnings) == 0 {
		t.Fatalf("table = %+v; want a warning and no rows", table)
	}
}

func TestParseInterfacesStripsANSI(t *testing.T) {
	output := "\x1b[1mInterface\x1b[0m                   PHY     Protocol  InUti OutUti\n" +
		"\x1b[32mGigabitEthernet0/0/1\x1b[0m        up      up           0%     0%\n"
	table := ParseInterfaces(output)
	if len(table.Rows) != 1 || table.Rows[0][0] != "GigabitEthernet0/0/1" {
		t.Fatalf("table = %+v", table)
	}
}
