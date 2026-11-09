package storage

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/hirochachacha/go-smb2"
	"github.com/jlaffaye/ftp"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type remoteAuth struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Key      string `json:"key"`
	Prefix   string `json:"prefix"`
}

func (a remoteAuth) addr(fallback int) string {
	port := a.Port
	if port == 0 {
		port = fallback
	}
	return net.JoinHostPort(a.Host, fmt.Sprint(port))
}

type sftpStore struct{ remoteAuth }

func newSFTP(raw json.RawMessage) (Store, error) {
	var settings sftpStore
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, err
	}
	if settings.Host == "" || settings.User == "" {
		return nil, fmt.Errorf("host and user are required")
	}
	return &settings, nil
}

func (s *sftpStore) client() (*ssh.Client, *sftp.Client, error) {
	auth, err := sshAuth(s.User, s.Password, s.Key)
	if err != nil {
		return nil, nil, err
	}
	client, err := ssh.Dial("tcp", s.addr(22), &ssh.ClientConfig{
		User:            s.User,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	})
	if err != nil {
		return nil, nil, err
	}
	session, err := sftp.NewClient(client)
	if err != nil {
		client.Close()
		return nil, nil, err
	}
	return client, session, nil
}

func sshAuth(user, password, key string) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	if strings.TrimSpace(key) != "" {
		signer, err := ssh.ParsePrivateKey([]byte(key))
		if err != nil {
			return nil, fmt.Errorf("ssh private key: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if password != "" {
		methods = append(methods, ssh.Password(password))
	}
	if len(methods) == 0 {
		return nil, fmt.Errorf("password or key is required")
	}
	_ = user
	return methods, nil
}

func (s *sftpStore) with(fn func(*sftp.Client) error) error {
	sshClient, client, err := s.client()
	if err != nil {
		return err
	}
	defer sshClient.Close()
	defer client.Close()
	return fn(client)
}

func (s *sftpStore) Put(_ context.Context, key string, r io.Reader) error {
	return s.with(func(client *sftp.Client) error {
		full := joinKey(s.Prefix, key)
		if err := client.MkdirAll(path.Dir(full)); err != nil {
			return err
		}
		file, err := client.Create(full)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(file, r)
		return err
	})
}

func (s *sftpStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	sshClient, client, err := s.client()
	if err != nil {
		return nil, err
	}
	file, err := client.Open(joinKey(s.Prefix, key))
	if err != nil {
		client.Close()
		sshClient.Close()
		return nil, err
	}
	return &sftpFile{file: file, client: client, ssh: sshClient}, nil
}

type sftpFile struct {
	file   *sftp.File
	client *sftp.Client
	ssh    *ssh.Client
}

func (f *sftpFile) Read(p []byte) (int, error) { return f.file.Read(p) }
func (f *sftpFile) Close() error {
	err := f.file.Close()
	f.client.Close()
	f.ssh.Close()
	return err
}

func (s *sftpStore) Delete(_ context.Context, key string) error {
	return s.with(func(client *sftp.Client) error {
		err := client.Remove(joinKey(s.Prefix, key))
		if err != nil && os.IsNotExist(err) {
			return nil
		}
		return err
	})
}

func (s *sftpStore) Exists(_ context.Context, key string) (bool, error) {
	var ok bool
	err := s.with(func(client *sftp.Client) error {
		_, statErr := client.Stat(joinKey(s.Prefix, key))
		if statErr == nil {
			ok = true
			return nil
		}
		if os.IsNotExist(statErr) {
			return nil
		}
		return statErr
	})
	return ok, err
}

func (s *sftpStore) Test(ctx context.Context) error { return testObject(ctx, s) }

type ftpStore struct {
	remoteAuth
	TLS bool `json:"tls"`
}

func newFTP(raw json.RawMessage) (Store, error) {
	var settings ftpStore
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, err
	}
	if settings.Host == "" || settings.User == "" {
		return nil, fmt.Errorf("host and user are required")
	}
	return &settings, nil
}

func (s *ftpStore) dial() (*ftp.ServerConn, error) {
	var options []ftp.DialOption
	options = append(options, ftp.DialWithTimeout(15*time.Second))
	if s.TLS {
		options = append(options, ftp.DialWithExplicitTLS(nil))
	}
	conn, err := ftp.Dial(s.addr(21), options...)
	if err != nil {
		return nil, err
	}
	if err := conn.Login(s.User, s.Password); err != nil {
		conn.Quit()
		return nil, err
	}
	return conn, nil
}

func (s *ftpStore) Put(_ context.Context, key string, r io.Reader) error {
	conn, err := s.dial()
	if err != nil {
		return err
	}
	defer conn.Quit()
	return conn.Stor(joinKey(s.Prefix, key), r)
}

func (s *ftpStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	conn, err := s.dial()
	if err != nil {
		return nil, err
	}
	response, err := conn.Retr(joinKey(s.Prefix, key))
	if err != nil {
		conn.Quit()
		return nil, err
	}
	return &ftpFile{response: response, conn: conn}, nil
}

type ftpFile struct {
	response *ftp.Response
	conn     *ftp.ServerConn
}

func (f *ftpFile) Read(p []byte) (int, error) { return f.response.Read(p) }
func (f *ftpFile) Close() error {
	err := f.response.Close()
	f.conn.Quit()
	return err
}

func (s *ftpStore) Delete(_ context.Context, key string) error {
	conn, err := s.dial()
	if err != nil {
		return err
	}
	defer conn.Quit()
	err = conn.Delete(joinKey(s.Prefix, key))
	if err != nil && strings.Contains(err.Error(), "550") {
		return nil
	}
	return err
}

func (s *ftpStore) Exists(_ context.Context, key string) (bool, error) {
	conn, err := s.dial()
	if err != nil {
		return false, err
	}
	defer conn.Quit()
	_, err = conn.FileSize(joinKey(s.Prefix, key))
	if err != nil {
		return false, nil
	}
	return true, nil
}

func (s *ftpStore) Test(ctx context.Context) error { return testObject(ctx, s) }

type azureSettings struct {
	Account    string `json:"account"`
	AccountKey string `json:"accountKey"`
	Container  string `json:"container"`
	Endpoint   string `json:"endpoint"`
	Prefix     string `json:"prefix"`
}

type azureStore struct{ azureSettings }

func newAzure(raw json.RawMessage) (Store, error) {
	var settings azureStore
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, err
	}
	if settings.Account == "" || settings.AccountKey == "" || settings.Container == "" {
		return nil, fmt.Errorf("account, accountKey and container are required")
	}
	if settings.Endpoint == "" {
		settings.Endpoint = "https://" + settings.Account + ".blob.core.windows.net"
	}
	return &settings, nil
}

func (s *azureStore) Put(ctx context.Context, key string, r io.Reader) error {
	blob := joinKey(s.Prefix, key)
	var ids []string
	const block = 8 << 20
	buf := make([]byte, block)
	index := 0
	for {
		n, err := io.ReadFull(r, buf)
		if n > 0 {
			index++
			id := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("block-%06d", index)))
			if err := s.block(ctx, blob, id, buf[:n]); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return err
		}
	}
	if len(ids) == 0 {
		id := base64.StdEncoding.EncodeToString([]byte("block-000001"))
		if err := s.block(ctx, blob, id, nil); err != nil {
			return err
		}
		ids = []string{id}
	}
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="utf-8"?><BlockList>`)
	for _, id := range ids {
		body.WriteString("<Latest>" + id + "</Latest>")
	}
	body.WriteString(`</BlockList>`)
	_, err := s.request(ctx, http.MethodPut, blob, "comp=blocklist", "application/xml", []byte(body.String()), map[string]string{})
	return err
}

func (s *azureStore) block(ctx context.Context, blob, id string, data []byte) error {
	query := "comp=block&blockid=" + url.QueryEscape(id)
	_, err := s.request(ctx, http.MethodPut, blob, query, "application/octet-stream", data, map[string]string{"x-ms-blob-type": ""})
	return err
}

func (s *azureStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	response, err := s.request(ctx, http.MethodGet, joinKey(s.Prefix, key), "", "", nil, nil)
	if err != nil {
		return nil, err
	}
	return response.Body, nil
}

func (s *azureStore) Delete(ctx context.Context, key string) error {
	_, err := s.request(ctx, http.MethodDelete, joinKey(s.Prefix, key), "", "", nil, nil)
	if err != nil && strings.Contains(err.Error(), "404") {
		return nil
	}
	return err
}

func (s *azureStore) Exists(ctx context.Context, key string) (bool, error) {
	response, err := s.request(ctx, http.MethodHead, joinKey(s.Prefix, key), "", "", nil, nil)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return false, nil
		}
		return false, err
	}
	response.Body.Close()
	return true, nil
}

func (s *azureStore) Test(ctx context.Context) error { return testObject(ctx, s) }

func (s *azureStore) request(ctx context.Context, method, blob, query, contentType string, body []byte, extra map[string]string) (*http.Response, error) {
	endpoint := strings.TrimRight(s.Endpoint, "/")
	rawURL := endpoint + "/" + s.Container + "/" + escapeBlob(blob)
	if query != "" {
		rawURL += "?" + query
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return nil, err
	}
	date := time.Now().UTC().Format(http.TimeFormat)
	req.Header.Set("x-ms-date", date)
	req.Header.Set("x-ms-version", "2021-08-06")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range extra {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	req.Header.Set("Authorization", s.sign(method, len(body), contentType, req.Header, blob, query))
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 300 {
		defer response.Body.Close()
		msg, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("azure %s: %s", response.Status, strings.TrimSpace(string(msg)))
	}
	return response, nil
}

func (s *azureStore) sign(method string, length int, contentType string, header http.Header, blob, query string) string {
	var names []string
	canon := map[string]string{}
	for key, values := range header {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "x-ms-") {
			names = append(names, lower)
			canon[lower] = strings.Join(values, ",")
		}
	}
	sort.Strings(names)
	var canonicalHeaders strings.Builder
	for _, name := range names {
		canonicalHeaders.WriteString(name + ":" + canon[name] + "\n")
	}
	resource := "/" + s.Account + "/" + s.Container + "/" + blob
	if query != "" {
		parts := strings.Split(query, "&")
		sort.Strings(parts)
		for _, part := range parts {
			name, value, _ := strings.Cut(part, "=")
			decoded, _ := url.QueryUnescape(value)
			resource += "\n" + strings.ToLower(name) + ":" + decoded
		}
	}
	contentLength := ""
	if length > 0 || method == http.MethodPut {
		contentLength = fmt.Sprint(length)
	}
	stringToSign := strings.Join([]string{
		method, "", "", contentLength, "", contentType, "", "", "", "", "", "",
		canonicalHeaders.String() + resource,
	}, "\n")
	key, _ := base64.StdEncoding.DecodeString(s.AccountKey)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(stringToSign))
	return "SharedKey " + s.Account + ":" + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func escapeBlob(blob string) string {
	parts := strings.Split(blob, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

type driveSettings struct {
	AccessToken string `json:"accessToken"`
	FolderID    string `json:"folderId"`
	Prefix      string `json:"prefix"`
}

type driveStore struct{ driveSettings }

func newDrive(raw json.RawMessage) (Store, error) {
	var settings driveStore
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, err
	}
	if settings.AccessToken == "" {
		return nil, fmt.Errorf("accessToken is required")
	}
	return &settings, nil
}

func (s *driveStore) Put(ctx context.Context, key string, r io.Reader) error {
	meta := map[string]any{"name": path.Base(joinKey(s.Prefix, key))}
	if s.FolderID != "" {
		meta["parents"] = []string{s.FolderID}
	}
	body, _ := json.Marshal(meta)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://www.googleapis.com/upload/drive/v3/files?uploadType=resumable", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.AccessToken)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("X-Upload-Content-Type", "application/octet-stream")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	response.Body.Close()
	if response.StatusCode >= 300 {
		return fmt.Errorf("google drive start upload: %s", response.Status)
	}
	location := response.Header.Get("Location")
	if location == "" {
		return fmt.Errorf("google drive did not return an upload location")
	}
	const chunk = 8 << 20
	buf := make([]byte, chunk)
	offset := 0
	for {
		n, readErr := io.ReadFull(r, buf)
		if n == 0 && (readErr == io.EOF || readErr == io.ErrUnexpectedEOF) {
			break
		}
		last := readErr == io.EOF || readErr == io.ErrUnexpectedEOF
		total := "*"
		if last {
			total = fmt.Sprint(offset + n)
		}
		put, err := http.NewRequestWithContext(ctx, http.MethodPut, location, bytes.NewReader(buf[:n]))
		if err != nil {
			return err
		}
		put.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%s", offset, offset+n-1, total))
		put.ContentLength = int64(n)
		sent, err := http.DefaultClient.Do(put)
		if err != nil {
			return err
		}
		sent.Body.Close()
		if last {
			if sent.StatusCode >= 300 {
				return fmt.Errorf("google drive upload: %s", sent.Status)
			}
			return nil
		}
		if sent.StatusCode != 308 && sent.StatusCode != 200 && sent.StatusCode != 201 {
			return fmt.Errorf("google drive upload: %s", sent.Status)
		}
		offset += n
		if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
			return readErr
		}
	}
	return nil
}

func (s *driveStore) Open(context.Context, string) (io.ReadCloser, error) {
	return nil, fmt.Errorf("google drive restore reads are not implemented; download the file from Drive")
}

func (s *driveStore) Delete(context.Context, string) error {
	return fmt.Errorf("google drive delete is not implemented")
}

func (s *driveStore) Exists(context.Context, string) (bool, error) {
	return false, fmt.Errorf("google drive existence checks are not implemented")
}

func (s *driveStore) Test(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.googleapis.com/drive/v3/about?fields=user", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.AccessToken)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return fmt.Errorf("google drive: %s", response.Status)
	}
	return nil
}

type nasSettings struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Share    string `json:"share"`
	User     string `json:"user"`
	Password string `json:"password"`
	Domain   string `json:"domain"`
	Prefix   string `json:"prefix"`
}

type nasStore struct{ nasSettings }

func newNAS(raw json.RawMessage) (Store, error) {
	var settings nasStore
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, err
	}
	if settings.Host == "" || settings.Share == "" || settings.User == "" {
		return nil, fmt.Errorf("host, share and user are required")
	}
	return &settings, nil
}

func (s *nasStore) session() (*smb2.Session, *smb2.Share, error) {
	port := s.Port
	if port == 0 {
		port = 445
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(s.Host, fmt.Sprint(port)), 15*time.Second)
	if err != nil {
		return nil, nil, err
	}
	dialer := &smb2.Dialer{Initiator: &smb2.NTLMInitiator{User: s.User, Password: s.Password, Domain: s.Domain}}
	session, err := dialer.Dial(conn)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	share, err := session.Mount(s.Share)
	if err != nil {
		session.Logoff()
		return nil, nil, err
	}
	return session, share, nil
}

func (s *nasStore) Put(_ context.Context, key string, r io.Reader) error {
	session, share, err := s.session()
	if err != nil {
		return err
	}
	defer session.Logoff()
	defer share.Umount()
	full := joinKey(s.Prefix, key)
	if dir := path.Dir(full); dir != "." && dir != "/" {
		_ = share.MkdirAll(dir, 0o755)
	}
	file, err := share.Create(full)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, r)
	return err
}

func (s *nasStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	session, share, err := s.session()
	if err != nil {
		return nil, err
	}
	file, err := share.Open(joinKey(s.Prefix, key))
	if err != nil {
		share.Umount()
		session.Logoff()
		return nil, err
	}
	return &nasFile{file: file, share: share, session: session}, nil
}

type nasFile struct {
	file    *smb2.File
	share   *smb2.Share
	session *smb2.Session
}

func (f *nasFile) Read(p []byte) (int, error) { return f.file.Read(p) }
func (f *nasFile) Close() error {
	err := f.file.Close()
	f.share.Umount()
	f.session.Logoff()
	return err
}

func (s *nasStore) Delete(_ context.Context, key string) error {
	session, share, err := s.session()
	if err != nil {
		return err
	}
	defer session.Logoff()
	defer share.Umount()
	err = share.Remove(joinKey(s.Prefix, key))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *nasStore) Exists(_ context.Context, key string) (bool, error) {
	session, share, err := s.session()
	if err != nil {
		return false, err
	}
	defer session.Logoff()
	defer share.Umount()
	_, err = share.Stat(joinKey(s.Prefix, key))
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func (s *nasStore) Test(ctx context.Context) error { return testObject(ctx, s) }

type rcloneSettings struct {
	Remote string `json:"remote"`
	Bin    string `json:"bin"`
}

type rcloneStore struct{ rcloneSettings }

func newRclone(raw json.RawMessage) (Store, error) {
	var settings rcloneStore
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, err
	}
	if settings.Remote == "" {
		return nil, fmt.Errorf("remote is required, for example qiniu:bucket")
	}
	if settings.Bin == "" {
		settings.Bin = "rclone"
	}
	return &settings, nil
}

func (s *rcloneStore) dest(key string) string {
	return strings.TrimRight(s.Remote, "/") + "/" + strings.TrimLeft(key, "/")
}

func (s *rcloneStore) Put(ctx context.Context, key string, r io.Reader) error {
	cmd := exec.CommandContext(ctx, s.Bin, "rcat", s.dest(key))
	cmd.Stdin = r
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("rclone rcat: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (s *rcloneStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx, s.Bin, "cat", s.dest(key))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &cmdReader{ReadCloser: stdout, cmd: cmd}, nil
}

type cmdReader struct {
	io.ReadCloser
	cmd *exec.Cmd
}

func (r *cmdReader) Close() error {
	err := r.ReadCloser.Close()
	wait := r.cmd.Wait()
	if err != nil {
		return err
	}
	return wait
}

func (s *rcloneStore) Delete(ctx context.Context, key string) error {
	cmd := exec.CommandContext(ctx, s.Bin, "deletefile", s.dest(key))
	out, err := cmd.CombinedOutput()
	if err != nil && !strings.Contains(string(out), "not found") {
		return fmt.Errorf("rclone deletefile: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (s *rcloneStore) Exists(ctx context.Context, key string) (bool, error) {
	cmd := exec.CommandContext(ctx, s.Bin, "lsf", s.dest(key))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(string(out)) != "", nil
}

func (s *rcloneStore) Test(ctx context.Context) error { return testObject(ctx, s) }
