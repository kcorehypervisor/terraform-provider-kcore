package provider

import (
	"strings"
	"testing"
)

func TestValidateVgpuHost(t *testing.T) {
	cases := []struct {
		name    string
		vendor  string
		numVfs  int
		cards   []string
		url     string
		wantErr string
	}{
		{name: "nvidia with manager", vendor: "nvidia", url: "https://x/m.run"},
		{name: "nvidia without manager", vendor: "nvidia", wantErr: "manager_url is required"},
		{name: "intel split", vendor: "intel", numVfs: 2},
		{name: "amd without count", vendor: "amd", cards: []string{"0000:c3:00.0"}, wantErr: "num_vfs is required"},
		{name: "amd per card counts", vendor: "amd", cards: []string{"0000:c3:00.0=4", "0000:c4:00.0=6"}},
		{name: "amd one card uncounted", vendor: "amd", cards: []string{"0000:c3:00.0=4", "0000:c4:00.0"}, wantErr: "num_vfs is required"},
		{name: "amd bad count", vendor: "amd", cards: []string{"0000:c3:00.0=x"}, wantErr: "positive number"},
		{name: "intel with manager", vendor: "intel", numVfs: 2, url: "https://x/m.run", wantErr: "do not use them"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sha, file := "", ""
			if tc.url != "" {
				sha, file = strings.Repeat("a", 64), "m.run"
			}
			err := validateVgpuHost(tc.vendor, tc.numVfs, tc.cards, tc.url, sha, file)
			if tc.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestVgpuHostSchemaAcceptsVendor(t *testing.T) {
	res := resourceVgpuHost()
	if err := res.InternalValidate(nil, true); err != nil {
		t.Fatal(err)
	}
	vendor := res.Schema["vendor"]
	if vendor == nil || vendor.Default != "nvidia" {
		t.Fatalf("vendor should default to nvidia, got %#v", vendor)
	}
	for _, name := range []string{"manager_url", "manager_sha256", "manager_file"} {
		if res.Schema[name].Required {
			t.Fatalf("%s must be optional for intel and amd", name)
		}
	}
}

func TestSplitCardKeyTreatsABareAddressAsTheDefaultCount(t *testing.T) {
	if splitCardKey("0000:C3:00.0", 4) != splitCardKey("0000:c3:00.0=4", 4) {
		t.Fatal("a bare address and address=num_vfs must compare equal")
	}
	if splitCardKey("0000:c3:00.0=6", 4) == splitCardKey("0000:c3:00.0", 4) {
		t.Fatal("a different count must show as a change")
	}
	if splitCardKey("0000:61:00.0", 0) != "0000:61:00.0" {
		t.Fatal("an nvidia card keeps its bare address")
	}
}
