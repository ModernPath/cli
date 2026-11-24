//go:build unix

package cmd

import (
	"os"
	"syscall"
)

func openPlanningImage(path string) (*os.File, error) {
	// The caller validates the opened descriptor before reading. Nonblocking
	// open lets it reject a named pipe even if nobody has opened its write end.
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
