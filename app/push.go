package app

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"claude-squad/config"
	"claude-squad/log"
)

// Web Push to the phone: the page (installed on the home screen, opened over HTTPS)
// subscribes, Squad keeps the subscriptions and sends a short notice when a session
// needs the user. Standard library only: VAPID (RFC 8292) and aes128gcm (RFC 8291).

var b64 = base64.RawURLEncoding

type vapidKeys struct {
	Private string `json:"private"` // the scalar, base64url
	Public  string `json:"public"`  // uncompressed point, base64url
}

type pushSub struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	Added time.Time `json:"added"`
	Agent string    `json:"agent,omitempty"`
}

type pusher struct {
	mu    sync.Mutex
	dir   string
	vapid *ecdsa.PrivateKey
	pub   []byte
	subs  []pushSub
	last  map[string]time.Time // per key: do not repeat the same notice too often
	httpc *http.Client
	send  func(sub pushSub, payload []byte) (int, error) // swapped in tests
}

func newPusher() *pusher {
	dir, err := config.GetConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	_ = os.MkdirAll(dir, 0700)
	p := &pusher{dir: dir, last: map[string]time.Time{}, httpc: &http.Client{Timeout: 15 * time.Second}}
	p.send = p.post
	if err := p.loadKeys(); err != nil {
		log.WarningLog.Printf("push: keys: %v", err)
	}
	p.loadSubs()
	return p
}

func (p *pusher) loadKeys() error {
	path := filepath.Join(p.dir, "vapid.json")
	var k vapidKeys
	if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &k) == nil && k.Private != "" {
		d, err := b64.DecodeString(k.Private)
		if err == nil {
			priv := new(ecdsa.PrivateKey)
			priv.Curve = elliptic.P256()
			priv.D = new(big.Int).SetBytes(d)
			priv.PublicKey.X, priv.PublicKey.Y = priv.Curve.ScalarBaseMult(d)
			p.vapid = priv
			p.pub = elliptic.Marshal(elliptic.P256(), priv.X, priv.Y) //nolint:staticcheck // the wire format Web Push wants
			return nil
		}
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	p.vapid = priv
	p.pub = elliptic.Marshal(elliptic.P256(), priv.X, priv.Y) //nolint:staticcheck
	d := priv.D.FillBytes(make([]byte, 32))
	data, _ := json.Marshal(vapidKeys{Private: b64.EncodeToString(d), Public: b64.EncodeToString(p.pub)})
	return os.WriteFile(path, data, 0600)
}

func (p *pusher) subsPath() string { return filepath.Join(p.dir, "push-subs.json") }

func (p *pusher) loadSubs() {
	data, err := os.ReadFile(p.subsPath())
	if err == nil {
		_ = json.Unmarshal(data, &p.subs)
	}
}

func (p *pusher) saveSubs() {
	data, _ := json.MarshalIndent(p.subs, "", "  ")
	_ = os.WriteFile(p.subsPath(), data, 0600)
}

func (p *pusher) add(s pushSub) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, x := range p.subs {
		if x.Endpoint == s.Endpoint {
			p.subs[i] = s
			p.saveSubs()
			return
		}
	}
	p.subs = append(p.subs, s)
	p.saveSubs()
}

func (p *pusher) remove(endpoint string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.subs[:0]
	for _, x := range p.subs {
		if x.Endpoint != endpoint {
			out = append(out, x)
		}
	}
	p.subs = out
	p.saveSubs()
}

func (p *pusher) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.subs)
}

type pushMsg struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Tag   string `json:"tag,omitempty"` // a newer notice with the same tag replaces the older one
	URL   string `json:"url,omitempty"`
}

// notify sends to every phone; key + every rate-limit the same notice (empty key: always).
func (p *pusher) notify(key string, every time.Duration, m pushMsg) {
	if p == nil || p.vapid == nil {
		return
	}
	p.mu.Lock()
	if key != "" {
		if t, ok := p.last[key]; ok && time.Since(t) < every {
			p.mu.Unlock()
			return
		}
		p.last[key] = time.Now()
	}
	subs := append([]pushSub(nil), p.subs...)
	p.mu.Unlock()
	if len(subs) == 0 {
		return
	}
	payload, _ := json.Marshal(m)
	go func() {
		for _, s := range subs {
			code, err := p.send(s, payload)
			if err != nil {
				log.WarningLog.Printf("push: %v", err)
				continue
			}
			if code == http.StatusGone || code == http.StatusNotFound {
				p.remove(s.Endpoint) // the phone unsubscribed or the app was removed
			} else if code >= 300 {
				log.WarningLog.Printf("push: %s answered %d", hostOf(s.Endpoint), code)
			}
		}
	}()
}

func hostOf(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "?"
	}
	return u.Host
}

func (p *pusher) post(s pushSub, payload []byte) (int, error) {
	body, err := encryptPush(s, payload)
	if err != nil {
		return 0, err
	}
	jwt, err := p.vapidJWT(s.Endpoint)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequest(http.MethodPost, s.Endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", "3600")
	req.Header.Set("Urgency", "high")
	req.Header.Set("Authorization", "vapid t="+jwt+", k="+b64.EncodeToString(p.pub))
	resp, err := p.httpc.Do(req)
	if err != nil {
		return 0, err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode, nil
}

func (p *pusher) vapidJWT(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	head := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, _ := json.Marshal(map[string]any{"aud": u.Scheme + "://" + u.Host, "exp": time.Now().Add(12 * time.Hour).Unix(), "sub": "mailto:squad@localhost.localdomain"})
	in := head + "." + b64.EncodeToString(claims)
	h := sha256.Sum256([]byte(in))
	r, s, err := ecdsa.Sign(rand.Reader, p.vapid, h[:])
	if err != nil {
		return "", err
	}
	sig := append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...)
	return in + "." + b64.EncodeToString(sig), nil
}

func hkdf(salt, ikm, info []byte, n int) []byte {
	m := hmac.New(sha256.New, salt)
	m.Write(ikm)
	prk := m.Sum(nil)
	m = hmac.New(sha256.New, prk)
	m.Write(info)
	m.Write([]byte{1})
	return m.Sum(nil)[:n]
}

func decodeKey(s string) ([]byte, error) {
	if b, err := b64.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.URLEncoding.DecodeString(s)
}

// encryptPush: RFC 8291 (aes128gcm, one record).
func encryptPush(s pushSub, payload []byte) ([]byte, error) {
	uaPubRaw, err := decodeKey(s.Keys.P256dh)
	if err != nil {
		return nil, fmt.Errorf("p256dh: %w", err)
	}
	auth, err := decodeKey(s.Keys.Auth)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	curve := ecdh.P256()
	uaPub, err := curve.NewPublicKey(uaPubRaw)
	if err != nil {
		return nil, fmt.Errorf("p256dh: %w", err)
	}
	as, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	shared, err := as.ECDH(uaPub)
	if err != nil {
		return nil, err
	}
	asPub := as.PublicKey().Bytes()
	info := append(append([]byte("WebPush: info\x00"), uaPubRaw...), asPub...)
	ikm := hkdf(auth, shared, info, 32)
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	cek := hkdf(salt, ikm, []byte("Content-Encoding: aes128gcm\x00"), 16)
	nonce := hkdf(salt, ikm, []byte("Content-Encoding: nonce\x00"), 12)
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, append(append([]byte{}, payload...), 2), nil)
	var out bytes.Buffer
	out.Write(salt)
	_ = binary.Write(&out, binary.BigEndian, uint32(4096))
	out.WriteByte(byte(len(asPub)))
	out.Write(asPub)
	out.Write(ct)
	return out.Bytes(), nil
}

// ---- web ----

func (p *pusher) webKey(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"key": b64.EncodeToString(p.pub), "subs": p.count()})
}

func (p *pusher) webSub(w http.ResponseWriter, r *http.Request) {
	var s pushSub
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&s); err != nil || s.Endpoint == "" || s.Keys.P256dh == "" || s.Keys.Auth == "" {
		http.Error(w, "нет подписки", http.StatusBadRequest)
		return
	}
	if u, err := url.Parse(s.Endpoint); err != nil || u.Scheme != "https" {
		http.Error(w, "адрес подписки должен быть https", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("off") == "1" {
		p.remove(s.Endpoint)
		writeJSON(w, map[string]any{"ok": true, "subs": p.count()})
		return
	}
	s.Added = time.Now()
	s.Agent = r.UserAgent()
	if len(s.Agent) > 160 {
		s.Agent = s.Agent[:160]
	}
	p.add(s)
	writeJSON(w, map[string]any{"ok": true, "subs": p.count()})
}

func (p *pusher) webTest(w http.ResponseWriter, r *http.Request) {
	if p.count() == 0 {
		http.Error(w, "ни один телефон не подписан", http.StatusBadRequest)
		return
	}
	p.notify("", 0, pushMsg{Title: "orchestraByVitalik", Body: "Проверка: уведомления на телефон работают", Tag: "test"})
	writeJSON(w, map[string]bool{"ok": true})
}
