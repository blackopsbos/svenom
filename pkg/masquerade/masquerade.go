package masquerade

import (
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// DisguiseAs mengubah nama proses (comm) dan cmdline agar tampak seperti proses lain.
// name: nama pendek (maks 15 byte) yang akan terlihat di /proc/pid/comm.
// fakeArgv: string yang akan menggantikan seluruh cmdline (terlihat di ps aux).
//           Jika kosong, akan menggunakan name.
func DisguiseAs(name string, fakeArgv string) error {
	// Ubah nama proses via prctl (PR_SET_NAME)
	// Linux hanya menyimpan 15 byte terakhir + null, jadi kita potong
	comm := [16]byte{}
	copy(comm[:], name)
	if _, _, errno := unix.Syscall6(unix.SYS_PRCTL, unix.PR_SET_NAME, uintptr(unsafe.Pointer(&comm[0])), 0, 0, 0, 0); errno != 0 {
		return errno
	}

	// Overwrite argv[0] dan seluruh cmdline jika kernel mendukung penuh
	// Kita ganti argv melalui modifikasi langsung memori os.Args,
	// namun karena os.Args adalah slice, cara paling portabel adalah
	// menimpa elemen pertama dan menambahkan sisanya dengan string kosong.
	// Untuk teknik lebih dalam (overwrite /proc/pid/cmdline), 
	// kita bisa langsung menulis ke /proc/self/cmdline.
	if err := overwriteCmdline(name, fakeArgv); err != nil {
		// Tidak fatal jika gagal, karena comm sudah berubah
		return err
	}
	return nil
}

// overwriteCmdline menulis string baru ke /proc/self/cmdline
// Ini membutuhkan akses tulis ke /proc/self/cmdline (umumnya bisa).
func overwriteCmdline(name, fakeArgv string) error {
	// Buka /proc/self/cmdline untuk ditulis
	f, err := os.OpenFile("/proc/self/cmdline", os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	defer f.Close()

	// Format baru: argv[0] name\0fakeArgv\0... (biasanya cmdline dipisahkan null)
	// Kita akan menulis name + \0 + fakeArgv + \0
	var data []byte
	if fakeArgv == "" {
		data = []byte(name + "\x00")
	} else {
		data = []byte(name + "\x00" + fakeArgv + "\x00")
	}
	_, err = f.Write(data)
	return err
}
