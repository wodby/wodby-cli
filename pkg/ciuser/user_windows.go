//go:build windows

package ciuser

func currentHostUser() (string, error) {
	return "", nil
}

func workspaceOwner(string) (string, error) {
	return "", nil
}

func CanChownDirectories() bool {
	return false
}
