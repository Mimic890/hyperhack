package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/altcha-org/altcha-lib-go/v2"
)

// ALTCHA v2 settings. The client in static/altcha.js implements exactly this:
// key = SHA-256(salt || nonce || uint32 big-endian counter), cost 1, 32-byte key,
// solved when the hex key starts with keyPrefix. ALTCHA_MAX_NUMBER bounds the counter.
const (
	altchaAlgorithm = "SHA-256"
	altchaCost      = 1
	maxUsedAltcha   = 100000 // replay map cap; past it new solutions are refused
)

var errAltchaBusy = errors.New("too many open challenges")

// altchaGuard creates challenges and verifies solutions once (replay protection).
type altchaGuard struct {
	sigKey, keySigKey string // two keys derived from ALTCHA_HMAC_KEY
	maxNumber         int64
	expires           time.Duration
	now               func() time.Time

	mu        sync.Mutex
	used      map[string]int64 // challenge signature → expiresAt (unix s)
	lastPurge time.Time
}

func newAltchaGuard(hmacKey string, maxNumber int64, expires time.Duration) *altchaGuard {
	derive := func(label string) string {
		m := hmac.New(sha256.New, []byte(hmacKey))
		m.Write([]byte(label))
		return hex.EncodeToString(m.Sum(nil))
	}
	return &altchaGuard{
		sigKey: derive("altcha signature"), keySigKey: derive("altcha key signature"),
		maxNumber: maxNumber, expires: expires, now: time.Now, used: map[string]int64{},
	}
}

func (g *altchaGuard) challenge() (altcha.Challenge, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(g.maxNumber+1))
	if err != nil {
		return altcha.Challenge{}, err
	}
	counter := int(n.Int64())
	exp := g.now().Add(g.expires)
	return altcha.CreateChallenge(altcha.CreateChallengeOptions{
		Algorithm:              altchaAlgorithm,
		Cost:                   altchaCost,
		Counter:                &counter,
		DeriveKey:              altcha.DeriveKeySHA(),
		ExpiresAt:              &exp,
		HMACSignatureSecret:    g.sigKey,
		HMACKeySignatureSecret: g.keySigKey,
	})
}

// verify checks a solution and marks its challenge used. The error says why it failed.
func (g *altchaGuard) verify(p altcha.Payload) error {
	params := p.Challenge.Parameters
	if params.Algorithm != altchaAlgorithm || params.Cost != altchaCost || params.ExpiresAt <= 0 {
		return errors.New("unexpected challenge parameters")
	}
	res, err := altcha.VerifySolution(altcha.VerifySolutionOptions{
		Challenge:              p.Challenge,
		Solution:               p.Solution,
		DeriveKey:              altcha.DeriveKeySHA(),
		HMACSignatureSecret:    g.sigKey,
		HMACKeySignatureSecret: g.keySigKey,
	})
	switch {
	case err != nil:
		return err
	case res.Expired:
		return errors.New("challenge expired")
	case !res.Verified:
		return errors.New("invalid solution")
	}
	now := g.now()
	g.mu.Lock()
	defer g.mu.Unlock()
	if now.Sub(g.lastPurge) > time.Minute || len(g.used) >= maxUsedAltcha {
		for sig, exp := range g.used {
			if exp < now.Unix() {
				delete(g.used, sig)
			}
		}
		g.lastPurge = now
	}
	if _, ok := g.used[p.Challenge.Signature]; ok {
		return errors.New("challenge already used")
	}
	if len(g.used) >= maxUsedAltcha {
		return errAltchaBusy
	}
	g.used[p.Challenge.Signature] = params.ExpiresAt
	return nil
}
