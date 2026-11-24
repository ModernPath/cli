package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// SR-5054-0: expose the same owner-scoped planning images as the web app.
const maxPlanningImageBytes = 20_000_000

func init() {
	epicsCmd.AddCommand(newPlanningImagesCommand(cliEnvLoad, "Epic", "/api/work/epics/"))
}

func newPlanningImagesCommand(load func() (*cliEnv, error), owner, prefix string) *cobra.Command {
	parent := &cobra.Command{Use: "images", Short: "Manage " + owner + " planning images"}
	idArg := "<epic-id>"
	if owner == "Initiative" {
		idArg = "<initiative-uuid>"
	}
	scoped := func(args []string, suffix string) (*cliEnv, string, error) {
		id := args[0]
		if owner == "Epic" {
			value, err := strconv.ParseInt(id, 10, 64)
			if err != nil || value <= 0 {
				return nil, "", fmt.Errorf("Epic ID must be a positive integer")
			}
			id = strconv.FormatInt(value, 10)
		} else if err := acceptanceUUID(id, owner); err != nil {
			return nil, "", err
		}
		env, err := load()
		if err != nil {
			return nil, "", err
		}
		path := fmt.Sprintf("%s%s/planning-context/images%s?system_id=%d", prefix, strings.ToLower(id), suffix, env.SystemID)
		return env, path, nil
	}

	list := &cobra.Command{Use: "list " + idArg, Short: "List planning image IDs, names and metadata as JSON", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, path, err := scoped(args, "")
			if err != nil {
				return err
			}
			body, err := planningImageRequest(env, http.MethodGet, path, nil, "application/json", 1<<20, http.StatusOK)
			if err != nil {
				return err
			}
			return writePlanningImageMetadata(cmd, body)
		}}
	download := &cobra.Command{Use: "download " + idArg + " <image-uuid>", Short: "Save a planning image to a new local file", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := acceptanceUUID(args[1], "Image"); err != nil {
				return err
			}
			env, path, err := scoped(args, "/"+strings.ToLower(args[1])+"/blob")
			if err != nil {
				return err
			}
			data, err := planningImageRequest(env, http.MethodGet, path, nil, "application/json", maxPlanningImageBytes, http.StatusOK)
			if err != nil {
				return err
			}
			out, _ := cmd.Flags().GetString("output")
			file, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return err
			}
			_, writeErr := file.Write(data)
			closeErr := file.Close()
			if writeErr != nil || closeErr != nil {
				_ = os.Remove(out)
				if writeErr != nil {
					return writeErr
				}
				return closeErr
			}
			return nil
		}}
	download.Flags().StringP("output", "o", "", "New local file for the downloaded image")
	_ = download.MarkFlagRequired("output")

	upload := &cobra.Command{Use: "upload " + idArg + " <file>", Short: "Upload a PNG or JPEG and print its metadata as JSON", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, path, err := scoped(args, "")
			if err != nil {
				return err
			}
			requestID, _ := cmd.Flags().GetString("request-id")
			if err := acceptanceUUID(requestID, "Upload request"); err != nil {
				return err
			}
			body, contentType, err := planningImageUpload(args[1], strings.ToLower(requestID))
			if err != nil {
				return err
			}
			data, err := planningImageRequest(env, http.MethodPost, path, body, contentType, 1<<20, http.StatusCreated, http.StatusOK)
			if err != nil {
				return err
			}
			return writePlanningImageMetadata(cmd, data)
		}}
	upload.Flags().String("request-id", "", "UUID for this upload. Reuse it when retrying the same file")
	_ = upload.MarkFlagRequired("request-id")
	remove := &cobra.Command{Use: "remove " + idArg + " <image-uuid>", Short: "Remove a planning image from its owner", Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := acceptanceUUID(args[1], "Image"); err != nil {
				return err
			}
			env, path, err := scoped(args, "/"+strings.ToLower(args[1]))
			if err != nil {
				return err
			}
			_, err = planningImageRequest(env, http.MethodDelete, path, nil, "application/json", 0, http.StatusNoContent)
			return err
		}}
	parent.AddCommand(list, download, upload, remove)
	return parent
}

func planningImageUpload(path, requestID string) ([]byte, string, error) {
	file, err := openPlanningImage(path)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, "", err
	}
	if !info.Mode().IsRegular() || info.Size() > maxPlanningImageBytes {
		return nil, "", fmt.Errorf("upload needs a regular file of at most %d bytes", maxPlanningImageBytes)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("request_id", requestID); err != nil {
		return nil, "", err
	}
	part, err := form.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return nil, "", err
	}
	size, err := io.Copy(part, io.LimitReader(file, maxPlanningImageBytes+1))
	if err != nil {
		return nil, "", err
	}
	if size > maxPlanningImageBytes {
		return nil, "", fmt.Errorf("upload exceeds %d bytes", maxPlanningImageBytes)
	}
	if err := form.Close(); err != nil {
		return nil, "", err
	}
	return body.Bytes(), form.FormDataContentType(), nil
}

func planningImageRequest(env *cliEnv, method, path string, body []byte, contentType string, limit int64, statuses ...int) ([]byte, error) {
	_, data, err := retryRateLimited(method, path, func() (int, []byte, error) {
		return sendPlanningImageRequest(env, method, path, body, contentType, limit, statuses)
	})
	return data, err
}

func sendPlanningImageRequest(env *cliEnv, method, path string, body []byte, contentType string, limit int64, statuses []int) (int, []byte, error) {
	// Recreate the reader for rate-limit retries. The bytes, multipart boundary
	// and upload request UUID stay identical. Only the scoped route may get them.
	resp, err := env.sendHTTP(method, path, bytes.NewReader(body), contentType, func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	})
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	status := resp.StatusCode
	if status == http.StatusUnauthorized {
		return status, nil, env.credentialRejected()
	}
	if !slices.Contains(statuses, status) {
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
		if err != nil {
			return status, nil, err
		}
		if err := rateLimitError(resp, raw); err != nil {
			return status, nil, err
		}
		if status >= 200 && status < 300 {
			return status, nil, fmt.Errorf("unexpected planning image response (HTTP %d)", status)
		}
		var refusal map[string]any
		if json.Unmarshal(raw, &refusal) != nil || refusal == nil {
			reason := strings.TrimSpace(string(raw))
			if reason == "" {
				reason = http.StatusText(status)
			}
			refusal = map[string]any{"error": map[string]any{"reason": reason}}
		}
		if code, ok := refusal["error"].(string); ok && str(refusal, "message") != "" {
			refusal["error"] = map[string]any{"reason": code, "message": str(refusal, "message")}
		}
		if ref := resp.Header.Get("x-request-id"); ref != "" && str(refusal, "request_id") == "" {
			refusal["request_id"] = ref
		}
		return status, nil, serverRefusal("planning image request", status, refusal)
	}
	if strings.HasSuffix(strings.SplitN(path, "?", 2)[0], "/blob") {
		mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if mediaType != "image/png" && mediaType != "image/jpeg" {
			return status, nil, fmt.Errorf("server did not return a PNG or JPEG")
		}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if int64(len(data)) > limit {
		return status, nil, fmt.Errorf("planning image response exceeds %d bytes", limit)
	}
	return status, data, err
}

func writePlanningImageMetadata(cmd *cobra.Command, body []byte) error {
	var envelope struct{ Data json.RawMessage }
	if err := json.Unmarshal(body, &envelope); err != nil {
		return err
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return fmt.Errorf("planning image response is missing data")
	}
	return writeCommandJSON(cmd, envelope.Data)
}
