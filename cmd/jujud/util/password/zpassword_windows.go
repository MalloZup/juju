// go build mksyscall_windows.go && ./mksyscall_windows password_windows.go
// MACHINE GENERATED BY THE COMMAND ABOVE; DO NOT EDIT

package password

import "unsafe"
import "syscall"

var (
	modnetapi32 = syscall.NewLazyDLL("netapi32.dll")

	procNetUserSetInfo = modnetapi32.NewProc("NetUserSetInfo")
)

func netUserSetInfo(servername *uint16, username *uint16, level uint32, buf *netUserSetPassword, parm_err *uint16) (err error) {
	r1, _, e1 := syscall.Syscall6(procNetUserSetInfo.Addr(), 5, uintptr(unsafe.Pointer(servername)), uintptr(unsafe.Pointer(username)), uintptr(level), uintptr(unsafe.Pointer(buf)), uintptr(unsafe.Pointer(parm_err)), 0)
	if r1 != 0 {
		if e1 != 0 {
			err = error(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}
