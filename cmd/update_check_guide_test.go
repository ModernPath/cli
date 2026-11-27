package cmd

import (
	"os"
	"strings"
	"testing"
)

// ucNormalize lowercases text, drops Markdown backticks and joins line breaks,
// so a phrase matches however the guide wraps it.
func ucNormalize(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.ReplaceAll(text, "`", "")), " "))
}

// Criterion 1a10c714 (also linked to 5607ba0b and 1abd8f02): the CLI guide,
// the kit copy always and modernpath-core/docs/cli.md when it exists, describes
// the release states prepare shows, the update notice, what --version shows,
// where the release cache lives, the CI rule, MODERNPATH_NO_UPDATE_CHECK, how
// the upgrade command is picked from the install path, that prepare waits up
// to 3 seconds for its lookup and when a command waits for its background
// lookup. The help that agents read through .modernpath/cli-reference.md
// carries the same rules.
func TestUpdateCheckGuideDescribesTheUpdateCheck(t *testing.T) {
	guides := map[string]string{}
	for _, path := range []string{"../internal/kit/assets/cli-guide/cli.md", "../../../docs/cli.md"} {
		body, err := os.ReadFile(path)
		// docs/cli.md lies outside this module; the core image tests the module
		// alone. Its identical kit copy is always checked.
		if os.IsNotExist(err) && path == "../../../docs/cli.md" {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		guides[path] = ucNormalize(string(body))
	}
	required := []struct {
		topic   string
		phrases []string
	}{
		{"the release states prepare shows", []string{
			"cli_release", "upgrade available", "up to date", "local build; update check not used",
			"latest release unknown: the lookup failed", "this lookup failed",
			"update check off: ci is set", "update check off: modernpath_no_update_check is set",
		}},
		{"the update notice", []string{
			"is available (you have", "upgrade it the way you installed it; modernpath prepare lists the commands.",
			"about once in 24 hours",
		}},
		{"what modernpath --version shows", []string{
			"modernpath --version", "this is the latest release.", "no newer release is available.", "couldn't check for a newer release.",
		}},
		{"where the release cache lives", []string{"release-latest.json", "user's cache folder", "4 hours"}},
		{"the CI rule and MODERNPATH_NO_UPDATE_CHECK", []string{
			"modernpath_no_update_check", "any value except empty, 0, false, no or off",
			"ignoring letter case and surrounding spaces", "turn every release lookup and update notice off",
		}},
		{"how the upgrade command is picked from the install path", []string{
			"resolved path of its own binary", "cellar/modernpath/", "scoop/apps/modernpath/", "~/.local/bin",
			`%localappdata%\programs\modernpath`,
		}},
		{"how long prepare waits for its lookup", []string{"prepare waits up to 3 seconds for its lookup"}},
		{"when a command waits for its background lookup", []string{
			"records it as cut off", "the next command that starts a background lookup waits for its own lookup",
		}},
	}
	forbidden := []string{
		"it does not guess whether homebrew, scoop or an install script was used",
		"an unavailable lookup leaves the latest version unknown",
		"when kit drift exists but the release lookup is unavailable, it asks you to check the release channel first",
		"the advice follows the operating system",
		"never makes the command wait",
		"never makes a command wait",
		"bounded at one second",
	}
	for name, text := range guides {
		for _, rule := range required {
			for _, phrase := range rule.phrases {
				if !strings.Contains(text, phrase) {
					t.Errorf("%s does not describe %s: missing %q", name, rule.topic, phrase)
				}
			}
		}
		for _, sentence := range forbidden {
			if strings.Contains(text, sentence) {
				t.Errorf("%s still says %q", name, sentence)
			}
		}
		// The sentence on when prepare exits non-zero names a newer release.
		start := strings.Index(text, "exits non-zero when authentication")
		if start < 0 {
			t.Errorf("%s has no sentence on when prepare exits non-zero", name)
			continue
		}
		sentence := text[start:]
		if end := strings.Index(sentence, ". "); end >= 0 {
			sentence = sentence[:end]
		}
		if !strings.Contains(sentence, "newer cli release") {
			t.Errorf("%s: the sentence on prepare's non-zero exit names no newer CLI release: %q", name, sentence)
		}
	}

	// Agents in connected repositories read the command help, rendered into
	// .modernpath/cli-reference.md, not the guide.
	prepareHelp, err := runRoot(t, "prepare", "--help")
	if err != nil {
		t.Fatal(err)
	}
	rootHelp, err := runRoot(t, "--help")
	if err != nil {
		t.Fatal(err)
	}
	for name, check := range map[string]struct {
		text               string
		phrases, forbidden []string
	}{
		"prepare --help": {prepareHelp, []string{
			"cli_release", "local_build", "lookup_failed", "release-latest.json", "user's cache folder",
			"modernpath_no_update_check", "any value except empty, 0, false, no or off", "waits up to 3 seconds",
		}, []string{"at most one second"}},
		"modernpath --help": {rootHelp, []string{
			"modernpath --version", "newer cli release", "update notice", "modernpath_no_update_check",
		}, nil},
	} {
		text := ucNormalize(check.text)
		for _, phrase := range check.phrases {
			if !strings.Contains(text, phrase) {
				t.Errorf("%s does not say %q", name, phrase)
			}
		}
		for _, phrase := range check.forbidden {
			if strings.Contains(text, phrase) {
				t.Errorf("%s still says %q", name, phrase)
			}
		}
	}
}
