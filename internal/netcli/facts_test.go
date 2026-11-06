package netcli

import "testing"

func TestParseFacts(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		output   string
		want     Facts
	}{
		{
			name: "huawei", platform: "huawei_vrp",
			output: "Huawei Versatile Routing Platform Software\n" +
				"VRP (R) software, Version 5.170 (S5720 V200R019C10SPC500)\n" +
				"HUAWEI S5720-28X-SI-AC Routing Switch uptime is 123 days, 4 hours, 5 minutes\n",
			want: Facts{Platform: "huawei_vrp", Version: "5.170", Model: "S5720-28X-SI-AC", Uptime: "123 days, 4 hours, 5 minutes"},
		},
		{
			name: "cisco", platform: "cisco_ios",
			output: "Cisco IOS Software, C2960X Software (C2960X-UNIVERSALK9-M), Version 15.2(2)E9, RELEASE SOFTWARE (fc1)\n" +
				"Router uptime is 1 year, 2 weeks, 3 days\n" +
				"cisco WS-C2960X-48FPS-L (APM86XXX) processor\n",
			want: Facts{Platform: "cisco_ios", Version: "15.2(2)E9", Model: "WS-C2960X-48FPS-L", Uptime: "1 year, 2 weeks, 3 days"},
		},
		{
			name: "h3c", platform: "h3c_comware",
			output: "H3C Comware Software, Version 7.1.070, Release 3506P11\n" +
				"H3C S5130S-28P-EI uptime is 0 weeks, 2 days, 3 hours\n",
			want: Facts{Platform: "h3c_comware", Version: "7.1.070", Model: "S5130S-28P-EI", Uptime: "0 weeks, 2 days, 3 hours"},
		},
		{
			name: "ruijie", platform: "ruijie_rgos",
			output: "Ruijie RGOS Software, Version 11.4(1)B12P1\n" +
				"Ruijie S5750-24GT4XS uptime is 12 weeks, 3 days\n",
			want: Facts{Platform: "ruijie_rgos", Version: "11.4(1)B12P1", Model: "S5750-24GT4XS", Uptime: "12 weeks, 3 days"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseFacts(tc.platform, tc.output); got != tc.want {
				t.Fatalf("ParseFacts = %+v; want %+v", got, tc.want)
			}
		})
	}
}
