package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/modernpath/cli/internal/contract"
)

// REQ-CROSS-390 (EPIC-CLI-019): before a store write the CLI compares its
// build against the contract the server advertises — once per loaded
// environment (one per command in practice),
// through the single call chokepoint — and refuses by name when this build
// lacks a capability the write needs. A read against a newer contract warns
// once per version. A server without the read is older than the check and
// is written to as before.

type serverContract struct {
	fetched      bool
	absent       bool
	status       int   // the read's HTTP status; 0 when the transport failed
	readErr      error // the transport failure, when there was one
	version      int
	capabilities map[string][]string
}

// ensureContract fetches the advertisement once per loaded environment. A transport
// failure is left to the write itself to report; the check does not run. The
// status and the failure are kept so a write that needs a server-side name
// (requireServerCapability) can tell an older server from a failed read.
func (e *factoryEnv) ensureContract() *serverContract {
	if e.contract != nil && e.contract.fetched {
		return e.contract
	}
	c := &serverContract{fetched: true}
	e.contract = c
	status, body, err := e.call("GET", "/api/v1/sync/contract", nil)
	c.status, c.readErr = status, err
	if err != nil || status == 404 {
		c.absent = true
		return c
	}
	if status != 200 {
		c.absent = true
		return c
	}
	data := dataOf(body)
	if v, ok := data["version"].(float64); ok {
		c.version = int(v)
	}
	c.capabilities = map[string][]string{}
	if caps, ok := data["capabilities"].(map[string]any); ok {
		for write, names := range caps {
			for _, n := range stringSlice(names) {
				c.capabilities[write] = append(c.capabilities[write], n)
			}
		}
	}
	return c
}

// checkWrite refuses a write whose required capabilities this build lacks.
func (e *factoryEnv) checkWrite(method, apiPath string, payload any) error {
	switch method {
	case "GET", "HEAD", "OPTIONS":
		return nil
	}
	c := e.ensureContract()
	if c.absent {
		return nil
	}
	name := writeName(apiPath, payload)
	if missing := contract.Missing(c.capabilities[name]); len(missing) > 0 {
		return fmt.Errorf("this build (%s) predates %s, which the server requires for %s; rebuild and install (modernpath-core/AGENTS.md §CLI release procedure)",
			Version, strings.Join(missing, ", "), name)
	}
	return nil
}

// requireServerCapability — SR-CLI-027-001 (EPIC-CLI-027): the other direction
// of REQ-CROSS-390. A write that posts a field an older server's field take
// would drop silently refuses before any request unless the server advertises
// `name` under `write`. A 404 contract read is a server older than the
// advertisement and reads as not advertised; a transport failure, or any other
// non-200 status, is reported as a failed read naming the status — never as a
// missing capability. `consequence` says what the server cannot do.
func (e *factoryEnv) requireServerCapability(write, name, consequence string) error {
	c := e.ensureContract()
	if c.readErr != nil {
		return fmt.Errorf("could not read the server's sync contract: %v — retry, or check the binding (modernpath status)", c.readErr)
	}
	if c.status != 200 && c.status != 404 {
		return fmt.Errorf("the server's sync contract read answered %d — retry, or check the binding (modernpath status)", c.status)
	}
	if contains(c.capabilities[write], name) {
		return nil
	}
	return fmt.Errorf("%s — deploy the server first (it does not advertise %s under %s)", consequence, name, write)
}

// noteServedContract records the version the server named on a response and
// warns once per version when this build is behind it.
func (e *factoryEnv) noteServedContract(header string) {
	if header == "" {
		return
	}
	e.contractVersion = header
	served, err := strconv.Atoi(strings.TrimSpace(header))
	if err != nil || served <= contract.Version {
		return
	}
	key := "contract-warning:" + header
	if e.warned == nil {
		e.warned = map[string]bool{}
	}
	if e.warned[key] {
		return
	}
	e.warned[key] = true
	if e.Root == "" || noticeOnce(e.Root, key) {
		printWarning("the server speaks sync contract %d; this build (%s) speaks %d — rebuild and install before writing (modernpath-core/AGENTS.md §CLI release procedure)\n",
			served, Version, contract.Version)
	}
}

// writeName is the write's key in the capability map: the author action, or
// for a create the record kind (author.gate, author.backlog), else the sync
// path with its separators normalised (work_selection, gate.answer).
func writeName(apiPath string, payload any) string {
	if name := reverseWriteName(apiPath); name != "" {
		return name
	}
	path := strings.TrimPrefix(apiPath, "/api/v1/sync/")
	path = strings.TrimPrefix(path, "/api/v1/")
	if idx := strings.Index(path, "?"); idx >= 0 {
		path = path[:idx]
	}
	if path == "author" {
		body, _ := payload.(map[string]any)
		action := str(body, "action")
		if action == "create" {
			if record, ok := body["record"].(map[string]any); ok && str(record, "kind") != "" {
				return "author." + str(record, "kind")
			}
		}
		if action != "" {
			return "author." + action
		}
		return "author"
	}
	if strings.HasPrefix(path, "gates/") && strings.HasSuffix(path, "/answer") {
		return "gate.answer"
	}
	return strings.NewReplacer("/", ".", "-", "_").Replace(path)
}
