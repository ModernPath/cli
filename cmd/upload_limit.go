package cmd

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// REQ-CROSS-500 (EPIC-CLI-029, USER:2026-09-29 D6): before the POST,
// `import --local` and `source push` compare the request body they are about
// to send with the server's upload limit (REQ-SYS-236,
// GET /api/import/capabilities). A body that does not fit is refused here,
// naming the zip size, the limit, the largest top-level directories and an
// --exclude example; a 413 that arrives anyway gets the same guidance, printed
// once (D4).

const (
	// legacyUploadLimitBytes is the endpoint limit of a server that predates
	// the capabilities read, which it answers with 404.
	legacyUploadLimitBytes int64 = 50_000_000
	// defaultUploadLimitBytes is the limit a server with the read has by
	// default; assumed when the read fails.
	defaultUploadLimitBytes int64 = 90_000_000
	// largestDirectoriesShown is how many top-level directories a size
	// refusal lists.
	largestDirectoriesShown = 10
)

// uploadCapabilities is what the one capabilities read says (D2): the upload
// limit, and the app address UI links open (REQ-CROSS-502), empty when the
// server names none or one that is not a web address.
type uploadCapabilities struct {
	limit  uploadLimit
	appURL string
}

// uploadLimit is the largest request body the upload routes accept. note
// says where the number came from when the server did not state it.
type uploadLimit struct {
	bytes int64
	note  string
}

// readUploadCapabilities reads the server's upload limit and app address. A
// 404 is a server that predates the read, whose limit is the endpoint's
// 50,000,000 bytes; a network error, a 5xx or an answer without the limit
// assumes the default. Any other refusal (an expired sign-in) is returned, so
// nothing is uploaded only to be refused for the same reason.
//
// The read closes its connection: a server that predates the route may raise
// on it and drop the kept-alive connection a moment after the 404, and the
// upload that followed on the same socket failed with a reset — a POST that
// has written its body is not retried (RUN:2026-10-04, the local stack).
func readUploadCapabilities(client *authenticatedClient) (uploadCapabilities, error) {
	resp, err := client.GetClosing(client.baseURL + "/api/import/capabilities")
	if err != nil {
		return uploadCapabilities{limit: assumedUploadLimit(err.Error())}, nil
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return uploadCapabilities{limit: uploadLimit{legacyUploadLimitBytes, fmt.Sprintf(
			"This server predates the upload limit read (GET /api/import/capabilities answered 404); using its limit of %s.",
			byteCount(legacyUploadLimitBytes))}}, nil
	case resp.StatusCode >= 500:
		return uploadCapabilities{limit: assumedUploadLimit(resp.Status)}, nil
	case resp.StatusCode != http.StatusOK:
		return uploadCapabilities{}, pushRefusal(resp, raw)
	}
	var answer struct {
		UploadLimitBytes int64  `json:"upload_limit_bytes"`
		AppURL           string `json:"app_url"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil || answer.UploadLimitBytes <= 0 {
		return uploadCapabilities{limit: assumedUploadLimit("the answer has no upload_limit_bytes"), appURL: webAddress(answer.AppURL)}, nil
	}
	return uploadCapabilities{limit: uploadLimit{bytes: answer.UploadLimitBytes}, appURL: webAddress(answer.AppURL)}, nil
}

// webAddress is raw when it is an absolute http or https URL with a host and
// no control characters, else "": the server's app_url is printed as a link,
// and anything else — a javascript: URL, a relative path, a newline or a
// terminal escape — is treated as no address (REQ-CROSS-502).
func webAddress(raw string) string {
	if strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return raw
}

func assumedUploadLimit(reason string) uploadLimit {
	return uploadLimit{defaultUploadLimitBytes, fmt.Sprintf(
		"Could not read the server's upload limit (%s); assuming %s.", reason, byteCount(defaultUploadLimitBytes))}
}

// readAppURL is the app address the capabilities read names, or "" when the
// read names none, names one that is not a web address, or fails: a UI link
// then falls back to the API host.
func readAppURL(client *authenticatedClient) string {
	capabilities, err := readUploadCapabilities(client)
	if err != nil {
		return ""
	}
	return capabilities.appURL
}

// systemAppLink is the address of a system's page in the app (REQ-CROSS-502
// C3): on the app host the capabilities read names (REQ-SYS-236), else on the
// API host, with a note saying so — the API host does not serve the app.
func systemAppLink(appURL, apiBase string, systemID int, facet string) string {
	if appURL != "" {
		return fmt.Sprintf("%s/systems/%d/%s", strings.TrimRight(appURL, "/"), systemID, facet)
	}
	return fmt.Sprintf("%s/systems/%d/%s (the API host: the server does not name its app address)",
		strings.TrimRight(apiBase, "/"), systemID, facet)
}

// bodyTooLargeError is a 413 from an upload route. limit is the limit the
// server named, 0 when its answer named none.
type bodyTooLargeError struct{ limit int64 }

func (e bodyTooLargeError) Error() string {
	return "the server refused the upload as too large (413)"
}

// limitOr is the limit the 413 named, else fallback: the limit the CLI read.
func (e bodyTooLargeError) limitOr(fallback int64) int64 {
	if e.limit > 0 {
		return e.limit
	}
	return fallback
}

// bodyTooLarge reads the limit from a 413 answer: the route's own
// body_too_large carries limit_bytes; Plug's error view and an edge proxy's
// page carry none.
func bodyTooLarge(raw []byte) bodyTooLargeError {
	var answer struct {
		LimitBytes int64 `json:"limit_bytes"`
	}
	_ = json.Unmarshal(raw, &answer)
	return bodyTooLargeError{limit: answer.LimitBytes}
}

// uploadSizes is what a size refusal reports about one upload.
type uploadSizes struct {
	command string // the command an --exclude example repeats
	zip     []byte
	body    int64 // the multipart request body; 0 before it is built
}

// overLimit refuses a body over the limit before anything is sent.
func (u uploadSizes) overLimit(limit int64) error {
	return fmt.Errorf("the upload does not fit: the request body would be %s (the zip is %s) "+
		"and the server accepts at most %s; nothing was uploaded%s",
		byteCount(u.body), byteCount(int64(len(u.zip))), byteCount(limit), u.guidance(""))
}

// refused renders a 413 the server sent.
func (u uploadSizes) refused(limit int64) error {
	return fmt.Errorf("the server refused the upload as too large (413): the request body was %s (the zip is %s) "+
		"and the server accepts at most %s%s",
		byteCount(u.body), byteCount(int64(len(u.zip))), byteCount(limit), u.guidance(""))
}

// overMaxSize refuses a zip larger than --max-size.
func (u uploadSizes) overMaxSize(maxSizeMB int) error {
	return fmt.Errorf("the zip is %s, over --max-size %d MiB (%s bytes); nothing was uploaded%s",
		byteCount(int64(len(u.zip))), maxSizeMB, groupDigits(int64(maxSizeMB)*1024*1024),
		u.guidance(", or raise --max-size"))
}

// guidance lists the largest top-level directories of the zip and an
// --exclude example built from the largest one.
func (u uploadSizes) guidance(alternative string) string {
	var b strings.Builder
	shares := largestTopLevel(u.zip, largestDirectoriesShown)
	example := "<directory>"
	if len(shares) > 0 {
		b.WriteString("\n  Largest top-level directories in the zip (compressed):")
		width := 0
		for _, share := range shares {
			width = max(width, len(share.label()))
		}
		for _, share := range shares {
			fmt.Fprintf(&b, "\n    %-*s  %s", width, share.label(), byteCount(share.bytes))
		}
		for _, share := range shares {
			if share.name != "" {
				example = share.name
				break
			}
		}
	}
	fmt.Fprintf(&b, "\n  Leave out what the analysis does not need with --exclude%s, for example:\n    %s --exclude %s",
		alternative, u.command, example)
	return b.String()
}

// topLevelShare is the compressed bytes one top-level directory adds to the
// zip; name is empty for the files at the top level.
type topLevelShare struct {
	name  string
	bytes int64
}

func (s topLevelShare) label() string {
	if s.name == "" {
		return "(files at the top level)"
	}
	return s.name + "/"
}

// largestTopLevel sums the compressed size of the zip's entries by top-level
// directory, from the zip itself rather than a second walk of the tree.
func largestTopLevel(data []byte, n int) []topLevelShare {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil
	}
	sums := map[string]int64{}
	for _, entry := range reader.File {
		top, _, nested := strings.Cut(entry.Name, "/")
		if !nested {
			top = ""
		}
		sums[top] += int64(entry.CompressedSize64)
	}
	shares := make([]topLevelShare, 0, len(sums))
	for name, size := range sums {
		shares = append(shares, topLevelShare{name, size})
	}
	sort.Slice(shares, func(i, j int) bool {
		if shares[i].bytes != shares[j].bytes {
			return shares[i].bytes > shares[j].bytes
		}
		return shares[i].name < shares[j].name
	})
	if len(shares) > n {
		shares = shares[:n]
	}
	return shares
}

// byteCount renders an exact byte count, with megabytes (10^6 bytes, the unit
// the server's limit is set in) from one megabyte up.
func byteCount(n int64) string {
	if n < 1_000_000 {
		return groupDigits(n) + " bytes"
	}
	return fmt.Sprintf("%s bytes (%.1f MB)", groupDigits(n), float64(n)/1_000_000)
}

func groupDigits(n int64) string {
	s := strconv.FormatInt(n, 10)
	start := 0
	if n < 0 {
		start = 1
	}
	for i := len(s) - 3; i > start; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
