package app

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// decryptPush is what the phone does (RFC 8291), to check encryptPush end to end.
func decryptPush(t *testing.T, ua *ecdh.PrivateKey, auth, body []byte) []byte {
	salt := body[:16]
	rs := binary.BigEndian.Uint32(body[16:20])
	require.EqualValues(t, 4096, rs)
	idlen := int(body[20])
	asPubRaw := body[21 : 21+idlen]
	ct := body[21+idlen:]
	asPub, err := ecdh.P256().NewPublicKey(asPubRaw)
	require.NoError(t, err)
	shared, err := ua.ECDH(asPub)
	require.NoError(t, err)
	info := append(append([]byte("WebPush: info\x00"), ua.PublicKey().Bytes()...), asPubRaw...)
	ikm := hkdf(auth, shared, info, 32)
	cek := hkdf(salt, ikm, []byte("Content-Encoding: aes128gcm\x00"), 16)
	nonce := hkdf(salt, ikm, []byte("Content-Encoding: nonce\x00"), 12)
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	pt, err := gcm.Open(nil, nonce, ct, nil)
	require.NoError(t, err)
	require.Equal(t, byte(2), pt[len(pt)-1]) // the last-record delimiter
	return pt[:len(pt)-1]
}

func TestPushEncryptAndVapid(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ua, err := ecdh.P256().GenerateKey(rand.Reader)
	require.NoError(t, err)
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	var sub pushSub
	sub.Endpoint = "https://web.push.apple.com/QGuQyavXutnMH"
	sub.Keys.P256dh = b64.EncodeToString(ua.PublicKey().Bytes())
	sub.Keys.Auth = b64.EncodeToString(auth)

	body, err := encryptPush(sub, []byte(`{"title":"Дирижёр","body":"ждёт тебя"}`))
	require.NoError(t, err)
	require.Equal(t, `{"title":"Дирижёр","body":"ждёт тебя"}`, string(decryptPush(t, ua, auth, body)))

	p := newPusher()
	require.Len(t, p.pub, 65)
	jwt, err := p.vapidJWT(sub.Endpoint)
	require.NoError(t, err)
	parts := strings.Split(jwt, ".")
	require.Len(t, parts, 3)
	claims, _ := b64.DecodeString(parts[1])
	var c map[string]any
	require.NoError(t, json.Unmarshal(claims, &c))
	require.Equal(t, "https://web.push.apple.com", c["aud"])
	sig, _ := b64.DecodeString(parts[2])
	require.Len(t, sig, 64)
	h := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	x, y := elliptic.Unmarshal(elliptic.P256(), p.pub) //nolint:staticcheck
	pub := &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}
	require.True(t, ecdsa.Verify(pub, h[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])))

	// the keys survive a restart
	p2 := newPusher()
	require.Equal(t, p.pub, p2.pub)
}

func TestPushNotifyRateLimitAndGone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p := newPusher()
	var s1, s2 pushSub
	s1.Endpoint, s2.Endpoint = "https://a.example/1", "https://b.example/2"
	p.add(s1)
	p.add(s2)
	var mu sync.Mutex
	got := map[string]int{}
	done := make(chan struct{}, 8)
	p.send = func(s pushSub, payload []byte) (int, error) {
		mu.Lock()
		got[s.Endpoint]++
		mu.Unlock()
		done <- struct{}{}
		if s.Endpoint == s2.Endpoint {
			return http.StatusGone, nil
		}
		return http.StatusCreated, nil
	}
	p.notify("k", time.Hour, pushMsg{Title: "x"})
	<-done
	<-done
	p.notify("k", time.Hour, pushMsg{Title: "x"}) // rate-limited: nothing sent
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	require.Equal(t, 1, got[s1.Endpoint])
	require.Equal(t, 1, got[s2.Endpoint])
	mu.Unlock()
	require.Eventually(t, func() bool { return p.count() == 1 }, time.Second, 10*time.Millisecond) // gone → dropped
}
