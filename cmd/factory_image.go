package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------- image generation (EPIC-DEC-001)

var imagePurpose string

var factoryImageCmd = &cobra.Command{
	Use:   "image <prompt>",
	Short: "Generate an image via the platform (tenant-stored; prints the URL)",
	Long: "Generates the image through the platform's governed image service:\n" +
		"the backend stores it in the tenant's storage and returns a URL; the CLI\n" +
		"never handles bytes.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}

		payload := map[string]any{"prompt": args[0]}
		if imagePurpose != "" {
			payload["purpose"] = imagePurpose
		}

		status, body, err := env.call("POST", "/api/v1/images", payload)
		if err != nil {
			return err
		}
		if status != 200 {
			return serverRefusal("", status, body)
		}

		data := dataOf(body)
		printSuccess("image generated: %s", str(data, "model"))
		fmt.Printf("  url:      %s%s\n", env.APIURL, str(data, "url"))
		fmt.Printf("  size:     %v bytes · latency: %v ms\n", data["byte_size"], data["latency_ms"])
		return nil
	},
}
