package cmd

import (
	"bytes"
	"crypto/md5"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/platform"
)

// SR-5075-0 (Epic 528): when the capabilities read offers direct_upload, the
// zip goes to a GCS resumable session the Core opens (POST
// /api/source-uploads), in chunks, and the upload completes through the
// existing import or source-archive endpoint with a JSON body naming it. The
// multipart POST through the edge (about 100 MB) stays for servers without
// direct_upload. The session URI is a bearer capability: it is never printed
// and the CLI's credential is never sent to it.

// directUploadChunkBytes is the size of one chunk PUT to a resumable session.
// GCS takes every chunk but the last in multiples of 256 KiB.
var directUploadChunkBytes int64 = 16 << 20

const (
	// directUploadAttempts is how many times in a row a chunk may fail
	// before the upload gives up.
	directUploadAttempts   = 5
	directUploadRetryDelay = 500 * time.Millisecond
)

// directUploadOffer is the capabilities read's direct_upload: the server
// opens storage sessions for zips up to maxBytes.
type directUploadOffer struct{ maxBytes int64 }

// zipBound is the largest zip a command uploads. maxSizeMB is the --max-size
// that sets it, 0 when direct_upload.max_bytes does.
type zipBound struct {
	bytes     int64
	maxSizeMB int
}

// uploadZipBound: direct_upload.max_bytes replaces the 100 MiB default of
// --max-size when the flag is not given; a given --max-size still applies,
// within direct_upload.max_bytes.
func uploadZipBound(maxSizeMB int, maxSizeGiven bool, caps uploadCapabilities) zipBound {
	flag := zipBound{bytes: int64(maxSizeMB) * 1024 * 1024, maxSizeMB: maxSizeMB}
	if caps.direct == nil {
		return flag
	}
	direct := zipBound{bytes: caps.direct.maxBytes}
	if maxSizeGiven && flag.bytes <= direct.bytes {
		return flag
	}
	return direct
}

// check refuses a zip over the bound before anything is sent.
func (b zipBound) check(sizes uploadSizes) error {
	if sizes.zip.size <= b.bytes {
		return nil
	}
	if b.maxSizeMB > 0 {
		return sizes.overMaxSize(b.maxSizeMB)
	}
	return sizes.overDirectMax(b.bytes)
}

// packedZip is the zip in a temporary file, with the size and MD5 the session
// is opened with.
type packedZip struct {
	file *os.File
	size int64
	md5  string // base64 of the raw digest, as GCS's md5Hash
}

// packZip writes the files to a temporary zip, hashing it as it is written.
// Close removes the file.
func packZip(files []importFile) (*packedZip, error) {
	file, err := os.CreateTemp("", "modernpath-source-*.zip")
	if err != nil {
		return nil, fmt.Errorf("failed to create zip: %w", err)
	}
	p := &packedZip{file: file}
	sum := md5.New()
	if err := writeZip(io.MultiWriter(file, sum), files); err != nil {
		p.Close()
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		p.Close()
		return nil, fmt.Errorf("failed to create zip: %w", err)
	}
	p.size = info.Size()
	p.md5 = base64.StdEncoding.EncodeToString(sum.Sum(nil))
	return p, nil
}

func (p *packedZip) ReadAt(b []byte, off int64) (int, error) { return p.file.ReadAt(b, off) }

func (p *packedZip) reader() io.Reader { return io.NewSectionReader(p.file, 0, p.size) }

func (p *packedZip) Close() {
	_ = p.file.Close()
	_ = os.Remove(p.file.Name())
}

// sourceSend is one upload of a packed zip: directly to storage when the
// server offers it, else multipart as before.
type sourceSend struct {
	client   *authenticatedClient
	caps     uploadCapabilities
	sizes    uploadSizes
	postURL  string
	fileName string
	// form is the multipart fields; complete is the JSON complete's fields
	// besides upload; session is the ids the session is opened for.
	form     [][2]string
	complete map[string]any
	session  map[string]any
	// failure prefixes a transport error of the POST.
	failure string
}

// send uploads the zip and returns the endpoint's 2xx answer.
func (s sourceSend) send() ([]byte, error) {
	if s.caps.direct == nil {
		return s.multipart()
	}
	upload, err := s.direct()
	var unavailable storageUnavailableError
	if errors.As(err, &unavailable) {
		// Plan assumption: a network that blocks the storage host keeps the
		// multipart path when the zip fits it.
		form, formErr := s.multipartForm()
		if formErr != nil {
			return nil, formErr
		}
		if form.size() > s.caps.limit.bytes {
			return nil, fmt.Errorf("%w; the zip does not fit the server's upload limit of %s without it, so nothing was uploaded",
				unavailable, byteCount(s.caps.limit.bytes))
		}
		printWarning("%s; posting the zip to the server instead.\n", unavailable)
		return s.postForm(form)
	}
	if err != nil {
		return nil, err
	}
	body := map[string]any{"upload": upload}
	for key, value := range s.complete {
		body[key] = value
	}
	raw, _ := json.Marshal(body)
	return s.post("application/json", func() io.Reader { return bytes.NewReader(raw) }, int64(len(raw)))
}

// multipartForm is the multipart request around the zip: the fields and the
// file part's header, the zip streamed from its temporary file, then the
// closing boundary. Its size is known without reading the zip.
type multipartForm struct {
	head, tail  []byte
	zip         *packedZip
	contentType string
}

func (f multipartForm) size() int64 {
	return int64(len(f.head)) + f.zip.size + int64(len(f.tail))
}

func (f multipartForm) body() io.Reader {
	return io.MultiReader(bytes.NewReader(f.head), f.zip.reader(), bytes.NewReader(f.tail))
}

func (s sourceSend) multipartForm() (multipartForm, error) {
	var buf bytes.Buffer
	form := multipart.NewWriter(&buf)
	for _, field := range s.form {
		_ = form.WriteField(field[0], field[1])
	}
	if _, err := form.CreateFormFile("file", s.fileName); err != nil {
		return multipartForm{}, fmt.Errorf("failed to create form: %w", err)
	}
	head := bytes.Clone(buf.Bytes())
	buf.Reset()
	if err := form.Close(); err != nil {
		return multipartForm{}, fmt.Errorf("failed to create form: %w", err)
	}
	return multipartForm{head: head, tail: buf.Bytes(), zip: s.sizes.zip, contentType: form.FormDataContentType()}, nil
}

// multipart posts the zip as today (REQ-CROSS-500).
func (s sourceSend) multipart() ([]byte, error) {
	form, err := s.multipartForm()
	if err != nil {
		return nil, err
	}
	return s.postForm(form)
}

// postForm posts the form; a body over the server's upload limit is refused
// before sending.
func (s sourceSend) postForm(form multipartForm) ([]byte, error) {
	s.sizes.body = form.size()
	if s.sizes.body > s.caps.limit.bytes {
		return nil, s.sizes.overLimit(s.caps.limit.bytes)
	}
	raw, err := s.post(form.contentType, form.body, s.sizes.body)
	var tooLarge bodyTooLargeError
	if errors.As(err, &tooLarge) {
		return nil, s.sizes.refused(tooLarge.limitOr(s.caps.limit.bytes))
	}
	return raw, err
}

func (s sourceSend) post(contentType string, body func() io.Reader, length int64) ([]byte, error) {
	resp, err := s.client.PostLength(s.postURL, contentType, body, length)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", s.failure, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusRequestEntityTooLarge {
		return nil, bodyTooLarge(raw)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, pushRefusal(resp, raw)
	}
	return raw, nil
}

// direct opens a session for the zip and uploads it; it returns the upload id
// the complete names.
func (s sourceSend) direct() (string, error) {
	zip := s.sizes.zip
	request := map[string]any{"size": zip.size, "md5": zip.md5}
	for key, value := range s.session {
		request[key] = value
	}
	raw, _ := json.Marshal(request)
	resp, err := s.client.Post(s.client.baseURL+"/api/source-uploads", "application/json", bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("%s: %w", s.failure, err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", pushRefusal(resp, answer)
	}
	var opened struct {
		Upload     string `json:"upload"`
		SessionURI string `json:"session_uri"`
	}
	if err := json.Unmarshal(answer, &opened); err != nil || opened.Upload == "" || opened.SessionURI == "" {
		return "", fmt.Errorf("unexpected answer to the upload session request (%s)", resp.Status)
	}
	if err := uploadToSession(opened.SessionURI, zip); err != nil {
		return "", err
	}
	return opened.Upload, nil
}

// storageUnavailableError is a session that never answered as one: its host
// could not be reached, or something on the way (a proxy, a firewall)
// answered in its place. Nothing reached storage, so the multipart path may
// still be taken.
type storageUnavailableError struct {
	host string
	err  error
}

func (e storageUnavailableError) Error() string {
	var refused *storageRefusal
	if errors.As(e.err, &refused) {
		return fmt.Sprintf("the storage host %s refused the upload (%s)", e.host, refused.answer)
	}
	return fmt.Sprintf("the storage host %s is unreachable (%v)", e.host, e.err)
}

func (e storageUnavailableError) Unwrap() error { return e.err }

// unreachable is a transport error before a connection to the host was made:
// a name that does not resolve, a refused or timed-out connect, a proxy that
// refuses CONNECT, or a certificate the CLI does not trust (an intercepting
// proxy). A connection that broke later reached the host.
func unreachable(err error) bool {
	var dns *net.DNSError
	var cert *tls.CertificateVerificationError
	var op *net.OpError
	switch {
	case errors.As(err, &dns), errors.As(err, &cert):
		return true
	case errors.As(err, &op):
		return op.Op == "dial" || op.Op == "proxyconnect"
	}
	return false
}

// uploadToSession PUTs the zip to a GCS resumable session in chunks. After a
// chunk fails it asks the session how much it holds (Content-Range:
// bytes */<size>) and continues from the range the 308 reports. Until the
// session has answered once (a 308, 200 or 201), a host that cannot be
// reached, a refusal, or retries that never get a session answer are a
// storageUnavailableError.
func uploadToSession(sessionURI string, zip *packedZip) error {
	parsed, err := url.Parse(sessionURI)
	if err != nil {
		return fmt.Errorf("the server answered an invalid upload session")
	}
	session := storageSession{
		uri:  sessionURI,
		host: parsed.Host,
		size: zip.size,
		// The session answers 308 without a Location: never follow it.
		client: &http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}
	var offset int64
	answered := false
	failures := 0
	// stop ends the upload with err: before the session answered, as storage
	// that cannot be used.
	stop := func(err error) error {
		if !answered {
			return storageUnavailableError{host: session.host, err: err}
		}
		return err
	}
	for {
		end := offset + directUploadChunkBytes
		if end > zip.size {
			end = zip.size
		}
		resp, err := session.put(fmt.Sprintf("bytes %d-%d/%d", offset, end-1, zip.size),
			io.NewSectionReader(zip.file, offset, end-offset), end-offset)
		if err == nil {
			var done bool
			done, offset, err = session.progress(resp)
			if done {
				return nil
			}
			if err == nil {
				answered = true
				failures = 0
				continue
			}
			if refused := (*storageRefusal)(nil); errors.As(err, &refused) {
				return stop(err)
			}
		} else if !answered && unreachable(err) {
			return stop(err)
		}
		// The chunk failed: ask the session what it holds and go on from there.
		for {
			failures++
			if failures > directUploadAttempts {
				return stop(fmt.Errorf("the upload to storage failed after %d attempts: %w", directUploadAttempts, err))
			}
			time.Sleep(time.Duration(failures) * directUploadRetryDelay)
			var resp *http.Response
			resp, err = session.put(fmt.Sprintf("bytes */%d", zip.size), http.NoBody, 0)
			if err != nil {
				continue
			}
			var done bool
			done, offset, err = session.progress(resp)
			if done {
				return nil
			}
			if err == nil {
				answered = true
				break
			}
			if refused := (*storageRefusal)(nil); errors.As(err, &refused) {
				return stop(err)
			}
		}
	}
}

type storageSession struct {
	uri    string
	host   string
	size   int64
	client *http.Client
}

// put sends one PUT to the session. A transport error is returned without
// the request URL, which holds the session.
func (s storageSession) put(contentRange string, body io.Reader, length int64) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPut, s.uri, body)
	if err != nil {
		return nil, fmt.Errorf("the server answered an invalid upload session")
	}
	// Prepare changes only requests to the platform host, never a storage
	// session; no credential is attached here.
	platform.Prepare(req)
	req.ContentLength = length
	req.Header.Set("Content-Range", contentRange)
	resp, err := s.client.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return nil, err
	}
	return resp, nil
}

// progress reads a session answer: 200 or 201 is the whole object; 308 is the
// offset to continue at, from its Range (bytes=0-N), 0 without one. A 5xx or
// 429 is an error to retry; any other answer stops the upload.
func (s storageSession) progress(resp *http.Response) (bool, int64, error) {
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	switch {
	case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated:
		return true, s.size, nil
	case resp.StatusCode == http.StatusPermanentRedirect:
		persisted := resp.Header.Get("Range")
		if persisted == "" {
			return false, 0, nil
		}
		_, last, ok := strings.Cut(strings.TrimPrefix(persisted, "bytes="), "-")
		end, err := strconv.ParseInt(last, 10, 64)
		if !ok || err != nil || end+1 > s.size {
			return false, 0, &storageRefusal{fmt.Sprintf("an invalid range %q", persisted)}
		}
		return false, end + 1, nil
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests:
		return false, 0, fmt.Errorf("storage answered %s", resp.Status)
	default:
		return false, 0, &storageRefusal{resp.Status}
	}
}

// storageRefusal stops the upload: the session refused it or answered
// something the CLI cannot continue from.
type storageRefusal struct{ answer string }

func (e *storageRefusal) Error() string {
	return "storage refused the upload: " + e.answer
}
