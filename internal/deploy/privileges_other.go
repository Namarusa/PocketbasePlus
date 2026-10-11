//go:build !linux

package deploy

// DropPrivileges only applies to Linux (the Docker image).
func DropPrivileges(dataDir string) error { return nil }
