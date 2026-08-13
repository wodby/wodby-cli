package ciuser

import "strings"

// ResolveBindUser returns the numeric identity that owns bind-mounted output.
// Root-run containerized CLIs use the workspace owner instead of root when
// the mounted checkout exposes a non-root owner.
func ResolveBindUser(context string) (string, error) {
	return resolveBindUser(context, currentHostUser, workspaceOwner)
}

func resolveBindUser(context string, current func() (string, error), owner func(string) (string, error)) (string, error) {
	user, err := current()
	if err != nil {
		return "", err
	}

	if strings.HasPrefix(user, "0:") && context != "" {
		workspaceUser, err := owner(context)
		if err != nil {
			return "", err
		}
		if workspaceUser != "" && !strings.HasPrefix(workspaceUser, "0:") {
			return workspaceUser, nil
		}
	}

	return user, nil
}
