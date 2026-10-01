package provider

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	pb "github.com/kcorehypervisor/terraform-provider-kcore/api/controller"
)

func api(meta interface{}) *apiClient {
	return meta.(*apiClient)
}

func s(d *schema.ResourceData, key string) string {
	v, _ := d.Get(key).(string)
	return v
}

func i64(d *schema.ResourceData, key string) int64 {
	v, _ := d.Get(key).(int)
	return int64(v)
}

func i32(d *schema.ResourceData, key string) int32 {
	return int32(i64(d, key))
}

func b(d *schema.ResourceData, key string) bool {
	v, _ := d.Get(key).(bool)
	return v
}

func setList(d *schema.ResourceData, key string) []string {
	raw, ok := d.GetOk(key)
	if !ok {
		return nil
	}
	set, ok := raw.(*schema.Set)
	if !ok {
		return strList(d, key)
	}
	out := make([]string, 0, set.Len())
	for _, item := range set.List() {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func strList(d *schema.ResourceData, key string) []string {
	raw, ok := d.GetOk(key)
	if !ok {
		return nil
	}
	items, _ := raw.([]interface{})
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func int32List(d *schema.ResourceData, key string) []int32 {
	raw, ok := d.GetOk(key)
	if !ok {
		return nil
	}
	items, _ := raw.([]interface{})
	out := make([]int32, 0, len(items))
	for _, item := range items {
		switch n := item.(type) {
		case int:
			out = append(out, int32(n))
		case int32:
			out = append(out, n)
		}
	}
	return out
}

func fail(ok bool, msg string) error {
	if ok {
		return nil
	}
	if msg == "" {
		msg = "controller rejected the request"
	}
	return fmt.Errorf("%s", msg)
}

func storageBackend(name string) pb.StorageBackendType {
	switch name {
	case "filesystem", "file":
		return pb.StorageBackendType_STORAGE_BACKEND_TYPE_FILESYSTEM
	case "lvm":
		return pb.StorageBackendType_STORAGE_BACKEND_TYPE_LVM
	case "zfs":
		return pb.StorageBackendType_STORAGE_BACKEND_TYPE_ZFS
	case "ceph":
		return pb.StorageBackendType_STORAGE_BACKEND_TYPE_CEPH
	default:
		return pb.StorageBackendType_STORAGE_BACKEND_TYPE_UNSPECIFIED
	}
}
