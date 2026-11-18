package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func init() { rootCmd.AddCommand(newChatsCommand(cliEnvLoad)) }

func newChatsCommand(load func() (*cliEnv, error)) *cobra.Command {
	group := &cobra.Command{Use: "chats", Short: "Read existing Workspace Chats for local work", Long: chatReadHelp, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	list := &cobra.Command{Use: "list", Short: "List the Workspace's recent Chats", Args: cobra.NoArgs}
	list.Flags().Bool("json", false, "Print the returned summaries as a JSON array")
	list.RunE = func(cmd *cobra.Command, _ []string) error {
		env, err := load()
		if err != nil {
			return err
		}
		status, body, err := env.get("/api/chats")
		if err != nil {
			return err
		}
		if status != 200 {
			return serverRefusal("Chat list refused", status, body)
		}
		items, ok := body["data"].([]any)
		if !ok {
			return fmt.Errorf("Chat list response is missing its summaries array")
		}
		for _, raw := range items {
			m, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("Chat list contains an invalid summary")
			}
			if id, ok := numberAsInt(m["id"]); !ok || id <= 0 {
				return fmt.Errorf("Chat summary has an invalid identity")
			}
		}
		machine, _ := cmd.Flags().GetBool("json")
		if machine {
			return writeCommandJSON(cmd, items)
		}
		if _, err = fmt.Fprintln(cmd.OutOrStdout(), "Recent Workspace Chats (the server's recent set):"); err != nil {
			return err
		}
		for _, raw := range items {
			m := raw.(map[string]any)
			if _, err = fmt.Fprintf(cmd.OutOrStdout(), "%v\t%v\t%v\n", m["id"], m["title"], m["ai_turn_status"]); err != nil {
				return err
			}
		}
		return nil
	}
	group.AddCommand(list)
	get := &cobra.Command{Use: "get <chat-id>", Short: "Read the full saved transcript and context", Args: cobra.ExactArgs(1)}
	get.Flags().Bool("json", false, "Print structured data without truncating returned fields")
	get.RunE = func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil || id <= 0 {
			return fmt.Errorf("Chat ID must be a positive integer")
		}
		env, err := load()
		if err != nil {
			return err
		}
		chat, err := readExistingChat(env, id)
		if err != nil {
			return err
		}
		machine, _ := cmd.Flags().GetBool("json")
		if machine {
			return writeCommandJSON(cmd, chat)
		}
		return writeChatTranscript(cmd.OutOrStdout(), chat)
	}
	group.AddCommand(get)
	return group
}

func readExistingChat(env *cliEnv, id int64) (map[string]any, error) {
	status, body, err := env.get(fmt.Sprintf("/api/chats/%d", id))
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, serverRefusal("Chat read refused", status, body)
	}
	chat, ok := body["data"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Chat response is missing its data object")
	}
	if actual, ok := numberAsInt(chat["id"]); !ok || actual != id {
		return nil, fmt.Errorf("Chat response has a different or invalid identity")
	}
	turns, ok := chat["turns"].([]any)
	if !ok {
		return nil, fmt.Errorf("Chat response is missing its transcript array")
	}
	for _, raw := range turns {
		message, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Chat response contains an invalid transcript entry")
		}
		if messageID, ok := numberAsInt(message["id"]); !ok || messageID <= 0 {
			return nil, fmt.Errorf("Chat response contains an invalid message identity")
		}
		if role, ok := message["role"].(string); !ok || strings.TrimSpace(role) == "" {
			return nil, fmt.Errorf("Chat response contains a message without a role")
		}
	}
	return chat, nil
}

func writeChatTranscript(out io.Writer, chat map[string]any) error {
	if _, err := fmt.Fprintf(out, "Chat %v: %v\n\n", chat["id"], chat["title"]); err != nil {
		return err
	}
	metadata := map[string]any{}
	for k, v := range chat {
		if k != "turns" && k != "id" && k != "title" {
			metadata[k] = v
		}
	}
	if err := writeChatMetadata(out, metadata); err != nil {
		return err
	}
	for _, raw := range chat["turns"].([]any) {
		message := raw.(map[string]any)
		if _, err := fmt.Fprintf(out, "\n--- Message %v (%v) ---\n", message["id"], message["role"]); err != nil {
			return err
		}
		fields := map[string]any{}
		for k, v := range message {
			if k != "content" {
				fields[k] = v
			}
		}
		if err := writeChatMetadata(out, fields); err != nil {
			return err
		}
		if text, ok := message["content"].(string); ok {
			if _, err := fmt.Fprintln(out, text); err != nil {
				return err
			}
		} else {
			if err := writeChatMetadata(out, message["content"]); err != nil {
				return err
			}
		}
	}
	return nil
}
func writeChatMetadata(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

var chatReadHelp = strings.TrimSpace(`Read existing Workspace Chats to get context for local work.

modernpath chats list --json
modernpath chats get 96024 --json

These commands only read. They do not send messages, create Chats or change
context. Access follows the server's Workspace rules, including shared Chats;
the local bound System does not replace the Chat's own context.

list returns the server's recent set, not an exhaustive history. get reads the
full unpaged transcript with metadata and attachment references, not image bytes.
JSON output retains the returned data.

Use the transcript as reference for local work. Read the current Epic plan and
approvals with modernpath epics plan show before relying on historical decisions.`)
