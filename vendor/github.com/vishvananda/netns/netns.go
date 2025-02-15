// Package netns allows ultra-simple network namespace handling. NsHandles
// can be retrieved and set. Note that the current namespace is thread
// local so actions that set and reset namespaces should use LockOSThread
// to make sure the namespace doesn't change due to a goroutine switch.
// It is best to close NsHandles when you are done with them. This can be
// accomplished via a `defer ns.Close()` on the handle. Changing namespaces
// requires elevated privileges, so in most cases this code needs to be run
// as root.
package netns

import (
	"fmt"
	"syscall"
	"os"
	"runtime"
)

const (
	selfNsFile  = "/proc/self/ns/net"
	netNsRunDir = "/var/run/netns/"
)
// NsHandle is a handle to a network namespace. It can be cast directly
// to an int and used as a file descriptor.
type NsHandle int

type handle interface {
	close() error
	fd() int
}

// Equal determines if two network handles refer to the same network
// namespace. This is done by comparing the device and inode that the
// file descriptors point to.
func (ns NsHandle) Equal(other NsHandle) bool {
	if ns == other {
		return true
	}
	var s1, s2 syscall.Stat_t
	if err := syscall.Fstat(int(ns), &s1); err != nil {
		return false
	}
	if err := syscall.Fstat(int(other), &s2); err != nil {
		return false
	}
	return (s1.Dev == s2.Dev) && (s1.Ino == s2.Ino)
}

// String shows the file descriptor number and its dev and inode.
func (ns NsHandle) String() string {
	var s syscall.Stat_t
	if ns == -1 {
		return "NS(None)"
	}
	if err := syscall.Fstat(int(ns), &s); err != nil {
		return fmt.Sprintf("NS(%d: unknown)", ns)
	}
	return fmt.Sprintf("NS(%d: %d, %d)", ns, s.Dev, s.Ino)
}

// UniqueId returns a string which uniquely identifies the namespace
// associated with the network handle.
func (ns NsHandle) UniqueId() string {
	var s syscall.Stat_t
	if ns == -1 {
		return "NS(none)"
	}
	if err := syscall.Fstat(int(ns), &s); err != nil {
		return "NS(unknown)"
	}
	return fmt.Sprintf("NS(%d:%d)", s.Dev, s.Ino)
}

// IsOpen returns true if Close() has not been called.
func (ns NsHandle) IsOpen() bool {
	return ns != -1
}

// Close closes the NsHandle and resets its file descriptor to -1.
// It is not safe to use an NsHandle after Close() is called.
func (ns *NsHandle) Close() error {
	if err := syscall.Close(int(*ns)); err != nil {
		return err
	}
	(*ns) = -1
	return nil
}

// None gets an empty (closed) NsHandle.
func None() NsHandle {
	return NsHandle(-1)
}

func Do(nsName string, cb Callback) error {
	// If destNS is empty, the function is called in the caller's namespace
	if nsName == "" {
		return cb()
	}
	
	// Get the file descriptor to the current namespace
	currNsFd, err := getNs(selfNsFile)
	if os.IsNotExist(err) {
		return fmt.Errorf("File descriptor to current namespace does not exist: %s", err)
	} else if err != nil {
		return fmt.Errorf("Failed to open %s: %s", selfNsFile, err)
	}
	defer currNsFd.close()
	
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	
	// Jump to the new network namespace
	if err := setNsByName(nsName); err != nil {
		return fmt.Errorf("Failed to set the namespace to %s: %s", nsName, err)
	}
	
	// Call the given function
	cbErr := cb()
	
	// Come back to the original namespace
	if err = setNs(currNsFd); err != nil {
		return fmt.Errorf("Failed to return to the original namespace: %s (callback returned %v)",
			err, cbErr)
	}
	
	return cbErr
}

type Callback func() error

type nsHandle int

func setNsByName(nsName string) error {
	netPath := netNsRunDir + nsName
	handle, err := getNs(netPath)
	if err != nil {
		return fmt.Errorf("Failed to getNs: %s", err)
	}
	err = setNs(handle)
	handle.close()
	if err != nil {
		return fmt.Errorf("Failed to setNs: %s", err)
	}
	return nil
}