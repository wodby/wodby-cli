package utils

import (
	"errors"
	"time"
	"unicode"
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

func IsAsciiPrintable(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII || !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}
