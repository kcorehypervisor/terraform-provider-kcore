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
		url     string
		wantErr string
	}{
		{name: "nvidia with manager", vendor: "nvidia", url: "https://x/m.run"},
		{name: "nvidia without manager", vendor: "nvidia", wantErr: "manager_url is required"},
		{name: "intel split", vendor: "intel", numVfs: 2},
		{name: "amd without count", vendor: "amd", wantErr: "num_vfs is required"},
		{name: "intel with manager", vendor: "intel", numVfs: 2, url: "https://x/m.run", wantErr: "do not use them"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sha, file := "", ""
			if tc.url != "" {
				sha, file = strings.Repeat("a", 64), "m.run"
			}
			err := validateVgpuHost(tc.vendor, tc.numVfs, tc.url, sha, file)
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
