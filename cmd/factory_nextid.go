package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/rdd"
	"github.com/spf13/cobra"
)

var ledgerRowIDRe = regexp.MustCompile(`^\|\s*(REQ-[A-Z]+-\d+)\s*\|`)

// REQ-CROSS-140: the ledger file is not the only writer of ids. Sync is
// deliberately not a replace-set, so the server can hold rows no checkout has
// ever contained — and on RUN:2026-08-14 a pass allocated REQ-CROSS-137 from the
// ledger's maximum while 095 and 096 already existed there.
var factoryNextIDCmd = &cobra.Command{
	Use:   "next-id <CONTEXT>",
	Short: "The next free requirement id, checked against the server as well as the ledgers",
	Long: `Prints the next free REQ id for a context.

Taking the ledger's maximum and adding one is the obvious approach and it is
wrong: the server can govern ids no ledger in this checkout contains. This
consults both, and says which basis it used — an offline answer is a weaker claim
than an online one, and you cannot tell them apart from the id alone.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		context := normalizeNextIDContext(args[0])

		// The ledgers are read WITHOUT the platform env on purpose. Requiring
		// credentials here would make an unauthenticated or offline checkout
		// unable to allocate an id at all, which is worse than allocating one
		// from a weaker basis — and the criterion this implements says so.
		cfgDir, err := config.FindConfigDir()
		if err != nil || cfgDir == "" {
			return fmt.Errorf("not a ModernPath workspace — run this from one containing %s/", config.ConfigDir)
		}
		ledger, err := ledgerIDs(filepath.Dir(cfgDir))
		if err != nil {
			return err
		}

		// The server half is best-effort. No network, no auth, or a server that
		// refuses all degrade to a ledger-only answer that says it is one.
		var server []string
		if env, envErr := factoryEnvLoad(); envErr == nil {
			if ids, ferr := env.fetchIDs("/api/v1/sync/requirements?system_id=%d&limit=3000", "requirements"); ferr == nil {
				server = ids
			} else {
				fmt.Fprintf(os.Stderr, "  (server not consulted: %v)\n", ferr)
			}
		} else {
			fmt.Fprintf(os.Stderr, "  (server not consulted: %v)\n", envErr)
		}

		a := rdd.NextID(context, ledger, server)
		fmt.Println(a.Next)
		fmt.Fprintf(os.Stderr, "  basis: %s · ledger max %d · server max %d\n", a.Basis, a.LedgerMax, a.ServerMax)
		if a.ServerAhead {
			fmt.Fprintf(os.Stderr,
				"  ⚠ the server governs REQ-%s-%03d, which no ledger here contains — allocating from the file alone would have collided (see `factory reconcile`)\n",
				context, a.ServerMax)
		}
		return nil
	},
}

// ledgerIDs collects dashboard-row ids from every ledger. Detail-block headings
// are deliberately not counted: a duplicate block would otherwise raise the
// maximum on a row that does not exist.
func ledgerIDs(root string) ([]string, error) {
	dir := filepath.Join(root, "tasks")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(string(body), "\n") {
			if m := ledgerRowIDRe.FindStringSubmatch(line); m != nil {
				out = append(out, m[1])
			}
		}
	}
	return out, nil
}

func init() {
	factoryCmd.AddCommand(factoryNextIDCmd)
}

// normalizeNextIDContext accepts both the bare area ("SBX") and the
// ledger-prefixed form a user naturally pastes ("REQ-SBX") — the latter used
// to answer "REQ-REQ-SBX-001" (REQ-CROSS-210 cosmetics, RUN:2026-08-18).
func normalizeNextIDContext(raw string) string {
	c := strings.ToUpper(strings.TrimSpace(raw))
	return strings.TrimPrefix(c, "REQ-")
}
