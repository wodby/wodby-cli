//go:build windows

package run

func defaultCurrentHostUser() (string, error) {
	return "", nil
}

func defaultWorkspaceOwner(string) (string, error) {
	return "", nil
}

func canChownCacheDirectories() bool {
	return false
}
