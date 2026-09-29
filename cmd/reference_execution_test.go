package cmd

import (
	"io"
	"testing"

	"github.com/spf13/cobra"
)

func TestCLIReferenceIsStableAfterACommandRuns(t *testing.T) {
	root := &cobra.Command{Use: "modernpath"}
	root.PersistentFlags().Bool("verbose", false, "verbose output")
	process := &cobra.Command{Use: "process"}
	process.AddCommand(&cobra.Command{Use: "prepare-inputs", RunE: func(*cobra.Command, []string) error { return nil }})
	root.AddCommand(process)
	before := renderCLIReference(root)
	root.SetArgs([]string{"process", "prepare-inputs"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if after := renderCLIReference(root); before != after {
		t.Fatal("running a command changed the installed CLI reference")
	}
}
