//go:build windows

package run

func defaultCurrentHostUser() (string, error) {
	return "", nil
}
