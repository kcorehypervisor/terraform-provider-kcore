package provider

import (
	"strings"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
)

func parseStorageBackend(s string) pb.StorageBackendType {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "filesystem", "file":
		return pb.StorageBackendType_STORAGE_BACKEND_TYPE_FILESYSTEM
	case "lvm":
		return pb.StorageBackendType_STORAGE_BACKEND_TYPE_LVM
	case "zfs":
		return pb.StorageBackendType_STORAGE_BACKEND_TYPE_ZFS
	default:
		return pb.StorageBackendType_STORAGE_BACKEND_TYPE_UNSPECIFIED
	}
}

func normalizeStorageBackendSpec(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "file", "filesystem":
		return "filesystem"
	case "lvm":
		return "lvm"
	case "zfs":
		return "zfs"
	default:
		return strings.ToLower(strings.TrimSpace(s))
	}
}

func vmDesiredStateProto(s string) pb.VmDesiredState {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "running":
		return pb.VmDesiredState_VM_DESIRED_STATE_RUNNING
	case "stopped":
		return pb.VmDesiredState_VM_DESIRED_STATE_STOPPED
	default:
		return pb.VmDesiredState_VM_DESIRED_STATE_UNSPECIFIED
	}
}

func storageBackendTypeToString(t pb.StorageBackendType) string {
	switch t {
	case pb.StorageBackendType_STORAGE_BACKEND_TYPE_FILESYSTEM:
		return "filesystem"
	case pb.StorageBackendType_STORAGE_BACKEND_TYPE_LVM:
		return "lvm"
	case pb.StorageBackendType_STORAGE_BACKEND_TYPE_ZFS:
		return "zfs"
	default:
		return ""
	}
}

func vmDesiredStateTerraform(s pb.VmDesiredState) string {
	switch s {
	case pb.VmDesiredState_VM_DESIRED_STATE_RUNNING:
		return "running"
	case pb.VmDesiredState_VM_DESIRED_STATE_STOPPED:
		return "stopped"
	default:
		return ""
	}
}
