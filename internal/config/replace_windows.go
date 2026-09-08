//go:build windows

package config

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

func replaceConfigFile(source, destination string) error {
	// Go's Windows readers do not share deletion. Give an in-progress config
	// read time to close its handle before replacing the snapshot.
	for attempt := 0; ; attempt++ {
		err := os.Rename(source, destination)
		if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) || attempt >= 100 {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
}
