package site

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("the record changed; reload before saving")

type Record struct {
	Key     string
	Data    []byte
	Version string
}
type Store interface {
	Get(context.Context, string) (Record, error)
	Put(context.Context, string, []byte, string) (string, error)
	List(context.Context, string) ([]Record, error)
	Delete(context.Context, string) error
	PutFile(context.Context, string, []byte, string) error
	File(context.Context, string) ([]byte, error)
	DeleteFile(context.Context, string) error
}

// Local storage is for development or a single process on a persistent disk.
type LocalStore struct {
	Dir string
	mu  sync.Mutex
}

func (s *LocalStore) path(key string) string {
	return filepath.Join(s.Dir, filepath.FromSlash(key)+".json")
}
func digest(b []byte) string { v := sha256.Sum256(b); return hex.EncodeToString(v[:]) }
func (s *LocalStore) Get(_ context.Context, k string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, e := os.ReadFile(s.path(k))
	if os.IsNotExist(e) {
		return Record{}, ErrNotFound
	}
	return Record{k, b, digest(b)}, e
}
func atomicWrite(p string, b []byte) error {
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(p), ".write-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	return os.Rename(f.Name(), p)
}
func (s *LocalStore) Put(_ context.Context, k string, b []byte, expected string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, e := os.ReadFile(s.path(k))
	if e != nil && !os.IsNotExist(e) {
		return "", e
	}
	if expected == "create" && e == nil {
		return "", ErrConflict
	}
	if expected != "" && expected != "create" && (e != nil || digest(old) != expected) {
		return "", ErrConflict
	}
	e = atomicWrite(s.path(k), b)
	return digest(b), e
}
func (s *LocalStore) List(_ context.Context, p string) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, e := os.ReadDir(filepath.Join(s.Dir, p))
	if os.IsNotExist(e) {
		return []Record{}, nil
	}
	if e != nil {
		return nil, e
	}
	out := []Record{}
	var totalBytes int
	for _, v := range entries {
		if !v.IsDir() && strings.HasSuffix(v.Name(), ".json") {
			k := p + "/" + strings.TrimSuffix(v.Name(), ".json")
			b, e := os.ReadFile(s.path(k))
			if e != nil {
				return nil, e
			}
			totalBytes += len(b)
			if totalBytes > 32<<20 || len(out) >= 20000 {
				return nil, errors.New("collection exceeds admin list budget; archive older records")
			}
			out = append(out, Record{k, b, digest(b)})
		}
	}
	return out, nil
}
func (s *LocalStore) Delete(_ context.Context, k string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := os.Remove(s.path(k))
	if os.IsNotExist(e) {
		return nil
	}
	return e
}
func (s *LocalStore) PutFile(_ context.Context, k string, b []byte, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return atomicWrite(filepath.Join(s.Dir, "files", k), b)
}
func (s *LocalStore) File(_ context.Context, k string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, e := os.ReadFile(filepath.Join(s.Dir, "files", k))
	if os.IsNotExist(e) {
		return nil, ErrNotFound
	}
	return b, e
}
func (s *LocalStore) DeleteFile(_ context.Context, k string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := os.Remove(filepath.Join(s.Dir, "files", k))
	if os.IsNotExist(e) {
		return nil
	}
	return e
}

type CloudStore struct {
	Project, Database, Namespace, Bucket  string
	Client                                *http.Client
	MetadataURL, FirestoreURL, StorageURL string
	mu                                    sync.Mutex
	token                                 string
	expires                               time.Time
}

func (s *CloudStore) bearer(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Now().Before(s.expires) {
		return s.token, nil
	}
	endpoint := s.MetadataURL
	if endpoint == "" {
		endpoint = "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token"
	}
	r, e := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if e != nil {
		return "", e
	}
	r.Header.Set("Metadata-Flavor", "Google")
	resp, e := s.Client.Do(r)
	if e != nil {
		return "", e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("metadata token: %d", resp.StatusCode)
	}
	var v struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if e = json.NewDecoder(resp.Body).Decode(&v); e != nil {
		return "", e
	}
	if v.AccessToken == "" {
		return "", errors.New("missing metadata token")
	}
	s.token = v.AccessToken
	s.expires = time.Now().Add(time.Duration(v.ExpiresIn-60) * time.Second)
	return s.token, nil
}
func (s *CloudStore) do(ctx context.Context, method, endpoint string, b []byte, ctype string) ([]byte, error) {
	token, e := s.bearer(ctx)
	if e != nil {
		return nil, e
	}
	r, e := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(b))
	if e != nil {
		return nil, e
	}
	r.Header.Set("Authorization", "Bearer "+token)
	if ctype != "" {
		r.Header.Set("Content-Type", ctype)
	}
	resp, e := s.Client.Do(r)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	out, e := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if e != nil {
		return nil, e
	}
	if resp.StatusCode == 404 {
		return nil, ErrNotFound
	}
	if resp.StatusCode == 409 || resp.StatusCode == 412 {
		return nil, ErrConflict
	}
	if resp.StatusCode == 400 {
		var apiError struct {
			Error struct {
				Status string `json:"status"`
			} `json:"error"`
		}
		_ = json.Unmarshal(out, &apiError)
		if apiError.Error.Status == "FAILED_PRECONDITION" || apiError.Error.Status == "ALREADY_EXISTS" {
			return nil, ErrConflict
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("cloud API status %d", resp.StatusCode)
	}
	return out, nil
}
func (s *CloudStore) root() string {
	base := s.FirestoreURL
	if base == "" {
		base = "https://firestore.googleapis.com/v1"
	}
	return base + "/projects/" + url.PathEscape(s.Project) + "/databases/" + url.PathEscape(s.Database) + "/documents/" + url.PathEscape(s.Namespace) + "/main"
}

type cloudDoc struct {
	Name       string                       `json:"name,omitempty"`
	Fields     map[string]map[string]string `json:"fields"`
	UpdateTime string                       `json:"updateTime,omitempty"`
}

func decodeDoc(b []byte, key string) (Record, error) {
	var d cloudDoc
	if e := json.Unmarshal(b, &d); e != nil {
		return Record{}, e
	}
	v := d.Fields["payload"]["stringValue"]
	if v == "" {
		return Record{}, errors.New("missing document payload")
	}
	return Record{key, []byte(v), d.UpdateTime}, nil
}
func (s *CloudStore) Get(ctx context.Context, k string) (Record, error) {
	b, e := s.do(ctx, "GET", s.root()+"/"+k, nil, "")
	if e != nil {
		return Record{}, e
	}
	return decodeDoc(b, k)
}
func (s *CloudStore) Put(ctx context.Context, k string, b []byte, expected string) (string, error) {
	doc := cloudDoc{Fields: map[string]map[string]string{"payload": {"stringValue": string(b)}}}
	raw, _ := json.Marshal(doc)
	q := url.Values{}
	if expected == "create" {
		q.Set("currentDocument.exists", "false")
	} else if expected != "" {
		q.Set("currentDocument.updateTime", expected)
	}
	endpoint := s.root() + "/" + k
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}
	out, e := s.do(ctx, "PATCH", endpoint, raw, "application/json")
	if e != nil {
		return "", e
	}
	d, e := decodeDoc(out, k)
	return d.Version, e
}
func (s *CloudStore) List(ctx context.Context, p string) ([]Record, error) {
	out := []Record{}
	var totalBytes int
	token := ""
	for {
		q := url.Values{"pageSize": {"300"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		b, e := s.do(ctx, "GET", s.root()+"/"+p+"?"+q.Encode(), nil, "")
		if e != nil {
			return nil, e
		}
		var v struct {
			Documents []cloudDoc `json:"documents"`
			Next      string     `json:"nextPageToken"`
		}
		if e = json.Unmarshal(b, &v); e != nil {
			return nil, e
		}
		for _, d := range v.Documents {
			parts := strings.Split(d.Name, "/")
			totalBytes += len(d.Fields["payload"]["stringValue"])
			if totalBytes > 32<<20 {
				return nil, errors.New("collection exceeds admin list budget; archive older records")
			}
			out = append(out, Record{p + "/" + parts[len(parts)-1], []byte(d.Fields["payload"]["stringValue"]), d.UpdateTime})
		}
		token = v.Next
		if token == "" {
			break
		}
		if len(out) > 20000 {
			return nil, errors.New("collection exceeds export limit; archive older leads")
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}
func (s *CloudStore) Delete(ctx context.Context, k string) error {
	_, e := s.do(ctx, "DELETE", s.root()+"/"+k, nil, "")
	if errors.Is(e, ErrNotFound) {
		return nil
	}
	return e
}
func (s *CloudStore) storageBase() string {
	if s.StorageURL != "" {
		return s.StorageURL
	}
	return "https://storage.googleapis.com"
}
func (s *CloudStore) object(k string) string { return s.Namespace + "/" + k }
func (s *CloudStore) PutFile(ctx context.Context, k string, b []byte, ctype string) error {
	q := url.Values{"uploadType": {"media"}, "name": {s.object(k)}, "ifGenerationMatch": {"0"}}
	_, e := s.do(ctx, "POST", s.storageBase()+"/upload/storage/v1/b/"+url.PathEscape(s.Bucket)+"/o?"+q.Encode(), b, ctype)
	return e
}
func (s *CloudStore) File(ctx context.Context, k string) ([]byte, error) {
	return s.do(ctx, "GET", s.storageBase()+"/storage/v1/b/"+url.PathEscape(s.Bucket)+"/o/"+url.PathEscape(s.object(k))+"?alt=media", nil, "")
}
func (s *CloudStore) DeleteFile(ctx context.Context, k string) error {
	_, e := s.do(ctx, "DELETE", s.storageBase()+"/storage/v1/b/"+url.PathEscape(s.Bucket)+"/o/"+url.PathEscape(s.object(k)), nil, "")
	if errors.Is(e, ErrNotFound) {
		return nil
	}
	return e
}
