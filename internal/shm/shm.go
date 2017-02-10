// Package shm implements SysV shared memory on Linux.
package shm

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

const IPC_CREAT = 00001000 /* create if key is nonexistent */
const IPC_EXCL = 00002000  /* fail if key exists */
const IPC_RMID = 0         /* remove resource */
const IPC_SET = 1          /* set ipc_perm options */
const IPC_STAT = 2         /* get ipc_perm options */
const IPC_INFO = 3         /* see ipcs */

type Segment struct {
	ID   int
	Size int
}

type shmid_ds struct {
	_ struct {
		key  int
		uid  uint
		gid  uint
		cuid uint
		cgid uint
		mode uint
		seq  uint16
	}
	Size int
	_    [256]byte
	_    [2]uintptr
}

func shmget(size int, flags int, perm int) (int, error) {
	r1, _, err := unix.Syscall(unix.SYS_SHMGET, uintptr(0), uintptr(size), uintptr(flags|perm))
	if err != 0 {
		return 0, err
	}
	return int(r1), nil
}

func shmctl(id int, cmd int, buf *shmid_ds) error {
	_, _, err := unix.Syscall(unix.SYS_SHMCTL, uintptr(id), uintptr(cmd), uintptr(unsafe.Pointer(buf)))
	if err != 0 {
		return err
	}
	return nil
}

func shmsize(id int) (int, error) {
	ds := new(shmid_ds)
	err := shmctl(id, IPC_STAT, ds)
	if err != nil {
		return 0, err
	}
	return ds.Size, nil
}

func shmdt(addr unsafe.Pointer) error {
	_, _, err := unix.Syscall(unix.SYS_SHMDT, uintptr(addr), uintptr(0), uintptr(0))
	if err != 0 {
		return err
	}
	return nil
}

func shmat(id int, addr unsafe.Pointer, flags int) (unsafe.Pointer, error) {
	r1, _, err := unix.Syscall(unix.SYS_SHMAT, uintptr(id), uintptr(addr), uintptr(flags))
	if err != 0 {
		return nil, err
	}
	return unsafe.Pointer(r1), nil
}

func Create(size int) (*Segment, error) {
	return OpenSegment(size, (IPC_CREAT | IPC_EXCL), 0600)
}

func Open(id int) (*Segment, error) {
	sz, err := shmsize(id)
	if err != nil {
		return nil, err
	}
	return &Segment{
		ID:   id,
		Size: sz,
	}, nil
}

func OpenSegment(size int, flags int, perms os.FileMode) (*Segment, error) {
	shmid, err := shmget(size, flags, int(perms))
	if err != nil {
		return nil, err
	}
	realSize, err := shmsize(shmid)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve SHM size: %s", err)
	}
	return &Segment{
		ID:   shmid,
		Size: realSize,
	}, nil
}

func DestroySegment(id int) error {
	return shmctl(id, IPC_RMID, nil)
}

func (self *Segment) Attach() (unsafe.Pointer, error) {
	addr, err := shmat(self.ID, nil, 0)
	if err != nil {
		return nil, err
	}
	return addr, nil
}

func (self *Segment) Detach(addr unsafe.Pointer) error {
	return shmdt(addr)
}

func (self *Segment) Destroy() error {
	return DestroySegment(self.ID)
}
