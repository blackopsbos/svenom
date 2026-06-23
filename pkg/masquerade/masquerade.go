package masquerade

import (
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

func DisguiseAs(name string, fakeArgv string) error {
	comm := [16]byte{}
	copy(comm[:], name)
	if _, _, errno := unix.Syscall6(unix.SYS_PRCTL, unix.PR_SET_NAME, uintptr(unsafe.Pointer(&comm[0])), 0, 0, 0, 0); errno != 0 {
		return errno
	}

	f, err := os.OpenFile("/proc/self/cmdline", os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	defer f.Close()

	var data []byte
	if fakeArgv == "" {
		data = []byte(name + "\x00")
	} else {
		data = []byte(name + "\x00" + fakeArgv + "\x00")
	}
	_, err = f.Write(data)
	return err
}