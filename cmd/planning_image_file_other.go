//go:build !unix

package cmd

import (
	"fmt"
	"os"
)

func openPlanningImage(path string) (*os.File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("upload needs a regular file")
	}
	return os.Open(path)
}
