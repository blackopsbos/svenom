package memfd

import (
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// ExecuteInMemory menjalankan binary ELF langsung dari RAM tanpa menyentuh disk.
// binary: byte lengkap dari file ELF.
// args: argumen yang akan diberikan ke proses baru (argv).
// env: environment variables, jika nil akan mewarisi env parent.
func ExecuteInMemory(binary []byte, args []string, env []string) error {
	// Buat memfd dengan nama "ghost_exec"
	fd, err := unix.MemfdCreate("ghost_exec", unix.MFD_CLOEXEC)
	if err != nil {
		return fmt.Errorf("memfd_create: %w", err)
	}
	defer unix.Close(fd)

	// Tulis binary ke memfd
	if _, err := unix.Write(fd, binary); err != nil {
		return fmt.Errorf("write to memfd: %w", err)
	}

	// Reset offset ke awal file
	if _, err := unix.Seek(fd, 0, 0); err != nil {
		return fmt.Errorf("lseek: %w", err)
	}

	// Siapkan path untuk fexecve (harus dalam /proc/self/fd/<fd>)
	fdPath := fmt.Sprintf("/proc/self/fd/%d", fd)

	// Jika env nil, gunakan environment proses saat ini
	if env == nil {
		env = os.Environ()
	}

	// Eksekusi binary dari memfd, proses sekarang akan digantikan
	return syscall.Exec(fdPath, args, env)
}
