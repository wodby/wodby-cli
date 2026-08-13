//go:build windows

package ciuser

func CanChownDirectories() bool {
	return false
}
