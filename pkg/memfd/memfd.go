package memfd

import (
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func ExecuteInMemory(binary []byte, args []string, env []string) error {
	fd, err := unix.MemfdCreate("ghost_exec", unix.MFD_CLOEXEC)
	if err != nil {
		return fmt.Errorf("memfd_create: %w", err)
	}
	defer unix.Close(fd)

	if _, err := unix.Write(fd, binary); err != nil {
		return fmt.Errorf("write to memfd: %w", err)
	}
	if _, err := unix.Seek(fd, 0, 0); err != nil {
		return fmt.Errorf("lseek: %w", err)
	}

	fdPath := fmt.Sprintf("/proc/self/fd/%d", fd)
	if env == nil {
		env = os.Environ()
	}
	return syscall.Exec(fdPath, args, env)
}