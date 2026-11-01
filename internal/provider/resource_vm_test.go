package provider

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestVgpuDiffSuppressesRequestedProfile(t *testing.T) {
	if !vgpuDiffSuppress("vgpus.0", "nvidia0/0=A2-4C", "A2-4C", nil) {
		t.Fatal("A2-4C should match the stored slice nvidia0/0=A2-4C")
	}
	if vgpuDiffSuppress("vgpus.0", "nvidia0/0=A2-4C", "nvidia0/1=A2-4C", nil) {
		t.Fatal("an explicit slice selector must stay visible to Terraform")
	}
	if vgpuDiffSuppress("vgpus.0", "nvidia0/0=A2-4C", "A2-8C", nil) {
		t.Fatal("a different profile must stay visible")
	}
}

func TestVgpuPlanKeepsRequestedProfile(t *testing.T) {
	diff, err := resourceVM().Diff(context.Background(), vgpuInstanceState("nvidia0/0=A2-4C"), vgpuResourceConfig("A2-4C"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if attr, ok := diff.Attributes["vgpus.0"]; ok {
		t.Fatalf("stored slice should not plan an update, got %#v", attr)
	}
	for key, attr := range diff.Attributes {
		if strings.HasPrefix(key, "pci_devices") || strings.HasPrefix(key, "disk") {
			t.Fatalf("controller-assigned %s should stay, got %#v", key, attr)
		}
	}

	diff, err = resourceVM().Diff(context.Background(), vgpuInstanceState("nvidia0/0=A2-4C"), vgpuResourceConfig("A2-8C"), nil)
	if err != nil {
		t.Fatal(err)
	}
	attr, ok := diff.Attributes["vgpus.0"]
	if !ok || attr.New != "A2-8C" {
		t.Fatalf("a different profile must plan an update, got %#v", diff.Attributes)
	}
}

func TestSSHPublicKeyIgnoresTrailingNewline(t *testing.T) {
	state := &terraform.InstanceState{
		ID: "vgpu-lab",
		Attributes: map[string]string{
			"id":         "vgpu-lab",
			"name":       "vgpu-lab",
			"public_key": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIE48yfJysZSE0SuFuzdBkSWOx0sDncgBwLEdcNnhXNft",
		},
	}
	conf := terraform.NewResourceConfigRaw(map[string]interface{}{
		"name":       "vgpu-lab",
		"public_key": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIE48yfJysZSE0SuFuzdBkSWOx0sDncgBwLEdcNnhXNft\n",
	})
	diff, err := resourceSSHKey().Diff(context.Background(), state, conf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if diff != nil {
		if attr, ok := diff.Attributes["public_key"]; ok {
			t.Fatalf("trailing newline should not replace the key, got %#v", attr)
		}
	}
}

func vgpuInstanceState(stored string) *terraform.InstanceState {
	return &terraform.InstanceState{
		ID: "vm-4a2a3cd7",
		Attributes: map[string]string{
			"id":                    "vm-4a2a3cd7",
			"name":                  "vgpu-01",
			"cpu":                   "4",
			"memory_bytes":          "8589934592",
			"storage_backend":       "filesystem",
			"storage_size_bytes":    "42949672960",
			"image_url":             "https://example.invalid/noble.img",
			"image_sha256":          "04e8f2461a16a44e78a370284471f29616cdd9baf1893df25807b71f985b6c94",
			"image_format":          "qcow2",
			"desired_state":         "running",
			"vgpus.#":               "1",
			"vgpus.0":               stored,
			"pci_devices.#":         "1",
			"pci_devices.0":         "0000:61:00.4",
			"disk.#":                "1",
			"disk.0.name":           "boot",
			"disk.0.backend_handle": "/var/lib/kcore/images/04e8f2461a16-noble-server-cloudimg-amd64.img",
			"disk.0.bus":            "virtio",
			"disk.0.device":         "disk",
		},
	}
}

func vgpuResourceConfig(requested string) *terraform.ResourceConfig {
	return terraform.NewResourceConfigRaw(map[string]interface{}{
		"name":               "vgpu-01",
		"cpu":                4,
		"memory_bytes":       8589934592,
		"storage_backend":    "filesystem",
		"storage_size_bytes": 42949672960,
		"image_url":          "https://example.invalid/noble.img",
		"image_sha256":       "04e8f2461a16a44e78a370284471f29616cdd9baf1893df25807b71f985b6c94",
		"image_format":       "qcow2",
		"desired_state":      "running",
		"vgpus":              []interface{}{requested},
	})
}

func TestAccKcoreVM_basic(t *testing.T) {
	resourceName := "kcore_vm.test"

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviders,
		CheckDestroy:      testAccCheckKcoreVMDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccKcoreVMConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckKcoreVMExists(resourceName),
					resource.TestCheckResourceAttr(resourceName, "name", "test-vm"),
					resource.TestCheckResourceAttr(resourceName, "cpu", "2"),
					resource.TestCheckResourceAttr(resourceName, "memory_bytes", "4294967296"),
				),
			},
		},
	})
}

func testAccCheckKcoreVMExists(n string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No VM ID is set")
		}

		// Optionally, you could make a GetVM call here to verify it exists

		return nil
	}
}

func testAccCheckKcoreVMDestroy(s *terraform.State) error {
	// Check that all VMs have been destroyed
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "kcore_vm" {
			continue
		}

		// Optionally, verify the VM no longer exists via API call
	}

	return nil
}

func testAccKcoreVMConfig_basic() string {
	return `
resource "kcore_vm" "test" {
  name         = "test-vm"
  cpu          = 2
  memory_bytes = 4294967296
  image_url    = "https://example.com/images/nixos-base.raw"
  image_sha256 = "abc123def456789012345678901234567890123456789012345678901234abcd"

  storage_backend    = "filesystem"
  storage_size_bytes = 10737418240

  disk {
    name           = "root"
    backend_handle = "/tmp/test-vm-root.qcow2"
    bus            = "virtio"
    device         = "disk"
  }

  nic {
    network = "default"
    model   = "virtio"
  }
}
`
}
