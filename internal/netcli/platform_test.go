package netcli

import (
	"slices"
	"testing"

	"motty/internal/ai/risk"
)

func TestAnglePrompt(t *testing.T) {
	tests := []struct {
		line string
		host string
		ok   bool
	}{
		{"<Huawei>", "Huawei", true},
		{"<Quidway>", "Quidway", true},
		{"[Huawei]", "Huawei", true},
		{"[~Huawei]", "Huawei", true}, // Comware uncommitted marker
		{"[*Huawei]", "Huawei", true}, // unsaved marker
		{"[Huawei-GigabitEthernet0/0/1]", "Huawei", true},
		{"<Huawei_1>", "Huawei_1", true},
		{" <Huawei> ", "", false}, // the tap trims before matching, raw lines fail
		{"<Huawei> display", "", false},
		{"Huawei>", "", false},
		{"[Huawei", "", false},
		{"<>", "", false},
		{"<Huawei display>", "", false},
		{"", "", false},
	}
	for _, tc := range tests {
		host, ok := anglePrompt(tc.line)
		if ok != tc.ok || ok && host != tc.host {
			t.Errorf("anglePrompt(%q) = (%q, %v); want (%q, %v)", tc.line, host, ok, tc.host, tc.ok)
		}
	}
}

func TestHashPrompt(t *testing.T) {
	tests := []struct {
		line string
		host string
		ok   bool
	}{
		{"Router>", "Router", true},
		{"Router#", "Router", true},
		{"Router(config)#", "Router", true},
		{"Router(config-if)#", "Router", true},
		{"R1.SW1#", "R1.SW1", true},
		{"Router#show run", "", false},
		{"secret 7 0822455D0A16#", "", false}, // ends in # but the host has spaces
		{"(config)#", "", false},
		{"#", "", false},
		{"Router", "", false},
		{"", "", false},
	}
	for _, tc := range tests {
		host, ok := hashPrompt(tc.line)
		if ok != tc.ok || ok && host != tc.host {
			t.Errorf("hashPrompt(%q) = (%q, %v); want (%q, %v)", tc.line, host, ok, tc.host, tc.ok)
		}
	}
}

func TestPlatformTableAndNames(t *testing.T) {
	if _, ok := Lookup("huawei_vrp"); !ok {
		t.Fatal("huawei_vrp missing")
	}
	if _, ok := Lookup("nope"); ok {
		t.Fatal("unknown platform found")
	}
	if Table("nope") == nil || Table("") == nil {
		t.Fatal("unknown ids must fall back to the generic table")
	}
	if !Table("huawei_vrp").Shaped() || Table(GenericID).Shaped() {
		t.Fatal("shaped-ness of the tables is wrong")
	}
	if Table("huawei_vrp").FactsCommand() != "display version" || Table("cisco_ios").InterfaceCommand() != "show ip interface brief" {
		t.Fatal("the built-in read commands are missing")
	}
	if Table(GenericID).FactsCommand() != "" || Table(GenericID).ConfigCommand() != "" {
		t.Fatal("the generic table claimed a read command")
	}
	ids := IDs()
	if ids[0] != GenericID || len(ids) != 5 {
		t.Fatalf("ids %v", ids)
	}
	for _, id := range ids {
		if !slices.Contains(ids, Table(id).ID) && id != GenericID {
			t.Fatalf("table %q not listed", id)
		}
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name       string
		platform   string
		command    string
		want       risk.Level
		wantReason string
	}{
		{"huawei display", "huawei_vrp", "display version", risk.Low, ""},
		{"huawei ping", "huawei_vrp", "ping 10.0.0.1", risk.Low, ""},
		{"huawei config read", "huawei_vrp", "display current-configuration", risk.Medium, "credentials"},
		{"huawei system-view", "huawei_vrp", "system-view", risk.High, ""},
		{"huawei save", "huawei_vrp", "save", risk.High, ""},
		{"huawei unknown", "huawei_vrp", "magic restart", risk.High, "not a recognized read-only"},
		{"huawei reboot", "huawei_vrp", "reboot fast", risk.Critical, ""},
		{"comware display", "h3c_comware", "display device", risk.Low, ""},
		{"comware config read", "h3c_comware", "display startup-configuration", risk.Medium, ""},
		{"cisco show", "cisco_ios", "show version", risk.Low, ""},
		{"cisco config read", "cisco_ios", "show running-config", risk.Medium, ""},
		{"cisco configure", "cisco_ios", "configure terminal", risk.High, ""},
		{"cisco conf t", "cisco_ios", "conf t", risk.High, ""},
		{"cisco reload", "cisco_ios", "reload", risk.Critical, ""},
		{"ruijie show", "ruijie_rgos", "show clock", risk.Low, ""},
		{"ruijie reboot", "ruijie_rgos", "reboot", risk.Critical, ""},
		{"generic show", GenericID, "show version", risk.High, "not a recognized read-only"},
		{"generic reboot", GenericID, "reboot", risk.Critical, ""},
		{"empty", "huawei_vrp", "   ", risk.High, "empty"},
		{"multiline", "huawei_vrp", "display version\nsystem-view", risk.High, "one command line"},
		{"tab", "huawei_vrp", "display\tversion", risk.High, "one command line"},
		{"case", "cisco_ios", "SHOW VERSION", risk.Low, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.platform, tc.command)
			if got.Level != tc.want {
				t.Fatalf("level %v; want %v (%v)", got.Level, tc.want, got.Reasons)
			}
			if tc.wantReason != "" && len(got.Reasons) > 0 && !containsFold(got.Reasons[0], tc.wantReason) {
				t.Fatalf("reason %q; want it to mention %q", got.Reasons[0], tc.wantReason)
			}
		})
	}
}

func containsFold(s, sub string) bool {
	return len(s) >= len(sub) && stringsIndexFold(s, sub) >= 0
}

func stringsIndexFold(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if hasPrefixFold(s[i:], sub) {
			return i
		}
	}
	return -1
}

func TestLooksLikeLoginPrompt(t *testing.T) {
	login := []string{"Username:", "username: ", "Password:", "Password: ", "login:", "Login: ",
		"User Name:", "localhost login:", "Passcode:", "Enter password:", "Account:"}
	for _, line := range login {
		if !LooksLikeLoginPrompt(line) {
			t.Errorf("LooksLikeLoginPrompt(%q) = false; want true", line)
		}
	}
	cli := []string{"<Huawei>", "[R1]", "Router#", "device>", "", "display version",
		"  ---- More ----", "Building configuration...", "12:00:00"}
	for _, line := range cli {
		if LooksLikeLoginPrompt(line) {
			t.Errorf("LooksLikeLoginPrompt(%q) = true; want false", line)
		}
	}
}
