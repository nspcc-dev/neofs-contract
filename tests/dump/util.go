package dump

import (
	"fmt"
	"os"
)

// checkFileNotExists checks that there is no file at the specified path.
func checkFileNotExists(p string) error {
	_, err := os.Stat(p)
	if !os.IsNotExist(err) {
		if err == nil {
			err = os.ErrExist
		}
		return fmt.Errorf("file '%s' absence check failed: %w", p, err)
	}
	return nil
}
