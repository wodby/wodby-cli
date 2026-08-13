//go:build !windows

package ciuser

import "os"

// CanChownDirectories reports whether newly created cache directories can be
// reassigned to the mapped container identity.
func CanChownDirectories() bool {
	return os.Geteuid() == 0
}
