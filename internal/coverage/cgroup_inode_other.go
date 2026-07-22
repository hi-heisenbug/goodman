//go:build !linux

package coverage

import "fmt"

func cgroupDirID(string) (uint64, error) {
	return 0, fmt.Errorf("cgroup inode lookup requires Linux")
}
