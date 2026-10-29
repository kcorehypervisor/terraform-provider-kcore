package provider

import "testing"

func TestInstallWaitsForController(t *testing.T) {
	if !installWaitsForController(true) {
		t.Fatal("a controller install must wait until that controller accepts RPCs")
	}
	if installWaitsForController(false) {
		t.Fatal("an agent-only install has no local controller to wait for")
	}
}
