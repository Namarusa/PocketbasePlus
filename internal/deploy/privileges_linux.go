package deploy

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// DropPrivileges runs the app as PUID/PGID when it starts as root with them
// set (the Docker image sets them to 65532): the data folder is given to that
// user first, so the folders a NAS (ZimaOS, CasaOS, Synology, Unraid...)
// creates as root for the volume work without a chown or "user:" setting.
// It does nothing when not root (e.g. "docker run --user"), or without PUID.
func DropPrivileges(dataDir string) error {
	if os.Getuid() != 0 || strings.TrimSpace(os.Getenv(PUIDEnv)) == "" {
		return nil
	}
	uid, err := envID(PUIDEnv)
	if err != nil {
		return err
	}
	gid := uid
	if strings.TrimSpace(os.Getenv(PGIDEnv)) != "" {
		if gid, err = envID(PGIDEnv); err != nil {
			return err
		}
	}
	if uid == 0 {
		return nil // explicitly root
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	if err := chownTree(dataDir, uid, gid); err != nil {
		// e.g. a network share refusing chown: the write check that follows
		// explains what to do
		fmt.Fprintf(os.Stderr, "warning: couldn't give %s to %d:%d: %v\n", dataDir, uid, gid, err)
	}
	if err := syscall.Setgroups(nil); err != nil {
		return fmt.Errorf("dropping root: %w", err)
	}
	if err := syscall.Setgid(gid); err != nil {
		return fmt.Errorf("dropping root: %w", err)
	}
	if err := syscall.Setuid(uid); err != nil {
		return fmt.Errorf("dropping root: %w", err)
	}
	return nil
}

func envID(name string) (int, error) {
	v := strings.TrimSpace(os.Getenv(name))
	id, err := strconv.Atoi(v)
	if err != nil || id < 0 {
		return 0, fmt.Errorf("%s must be a numeric id, got %q", name, v)
	}
	return id, nil
}

// chownTree gives dir and its content to uid:gid, touching only the entries
// owned by someone else (fast on later starts, keeps symlink targets).
func chownTree(dir string, uid, gid int) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) == uid && int(st.Gid) == gid {
			return nil
		}
		return os.Lchown(path, uid, gid)
	})
}
