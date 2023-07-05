package utils

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type CheckFunc func() (bool, error)

func WaitFor(check CheckFunc, interval time.Duration, timeout time.Duration) error {
	ticker := time.NewTicker(interval)
	timer := time.NewTimer(timeout)

	for {
		select {
		case <-timer.C:
			return errors.New("timed out")
		case <-ticker.C:
			status, err := check()
			if err != nil {
				return err
			}

			if status {
				ticker.Stop()
				return nil
			}
		}
	}
}

// BuildTag builds a tag for an image from provided custom registry and a service's image slug.
//
// We assume customRegistry specified as "my-private-registry.com/project"
// serviceSlug usually comes with wodby docker registry that includes org name and unique image name slug.
func BuildTag(customRegistry string, serviceSlug, buildNumber string) string {
	parts := strings.Split(serviceSlug, "/")
	return fmt.Sprintf("%s/%s:%s", customRegistry, parts[len(parts)-1], buildNumber)
}
