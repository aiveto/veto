package auth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	errUserSignature = errors.New("user token signature rejected")
	errUserIssuer    = errors.New("user token issuer rejected")
	errUserAudience  = errors.New("user token audience rejected")
	errUserExpired   = errors.New("user token expired")
	errUserUnchecked = errors.New("user token could not be checked")
)

type (
	sigKey struct {
		kid string
		alg string
		pub crypto.PublicKey
	}

	jwksCache struct {
		mu sync.Mutex
		m  map[string][]sigKey
	}
)

func looksLikeJWT(tok string) bool {
	parts := strings.Split(tok, ".")
	return len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] != ""
}

// checkUserToken verifies a JWT user token when discovery published jwks_uri.
// An opaque token, or a JWT with no published jwks_uri, is returned unchanged.
func (r *Resolver) checkUserToken(ctx context.Context, s Scheme, stored storedToken, userTok string) (time.Time, error) {
	if !looksLikeJWT(userTok) {
		return time.Time{}, nil
	}
	jwksURI := s.JWKSURI
	if jwksURI == "" {
		jwksURI = stored.JWKSURI
	}
	if jwksURI == "" && s.Issuer != "" {
		if err := fillEndpoints(ctx, r.http, &s); err != nil {
			return time.Time{}, err
		}
		jwksURI = s.JWKSURI
	}
	if jwksURI == "" {
		return time.Time{}, nil
	}
	issuer := s.Issuer
	if issuer == "" {
		issuer = stored.Issuer
	}
	clientID := s.ClientID
	if clientID == "" {
		clientID = stored.ClientID
	}
	if r.jwks == nil {
		r.jwks = &jwksCache{m: map[string][]sigKey{}}
	}
	keys, err := r.jwks.keys(ctx, r.http, jwksURI)
	if err != nil {
		return time.Time{}, errUserUnchecked
	}
	exp, err := verifyUserJWT(keys, userTok, issuer, clientID, s.Audience, r.now())
	if err == nil {
		return exp, nil
	}
	if !errors.Is(err, errUserSignature) {
		return time.Time{}, err
	}
	r.jwks.drop(jwksURI)
	keys, err = r.jwks.keys(ctx, r.http, jwksURI)
	if err != nil {
		return time.Time{}, errUserUnchecked
	}
	return verifyUserJWT(keys, userTok, issuer, clientID, s.Audience, r.now())
}

func verifyUserJWT(keys []sigKey, tok, issuer, clientID, audience string, now time.Time) (time.Time, error) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return time.Time{}, errUserSignature
	}
	headerJSON, err := decodeB64(parts[0])
	if err != nil {
		return time.Time{}, errUserSignature
	}
	payloadJSON, err := decodeB64(parts[1])
	if err != nil {
		return time.Time{}, errUserSignature
	}
	sig, err := decodeB64(parts[2])
	if err != nil {
		return time.Time{}, errUserSignature
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if json.Unmarshal(headerJSON, &header) != nil || header.Alg == "" || strings.EqualFold(header.Alg, "none") {
		return time.Time{}, errUserSignature
	}
	signed := parts[0] + "." + parts[1]
	matched := false
	for _, key := range keys {
		if header.Kid != "" && key.kid != header.Kid {
			continue
		}
		if key.alg != "" && key.alg != header.Alg {
			continue
		}
		if verifySig(header.Alg, key.pub, signed, sig) != nil {
			continue
		}
		matched = true
		break
	}
	if !matched {
		return time.Time{}, errUserSignature
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(payloadJSON, &raw) != nil {
		return time.Time{}, errUserSignature
	}
	var iss string
	if json.Unmarshal(raw["iss"], &iss) != nil || !sameIssuer(iss, issuer) {
		return time.Time{}, errUserIssuer
	}
	if !audOK(audienceClaim(raw["aud"]), clientID, audience) {
		return time.Time{}, errUserAudience
	}
	var expNum float64
	if json.Unmarshal(raw["exp"], &expNum) != nil || expNum <= 0 {
		return time.Time{}, errUserExpired
	}
	exp := time.Unix(int64(expNum), 0)
	if !fresh(exp, now) {
		return time.Time{}, errUserExpired
	}
	return exp, nil
}

func audienceClaim(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		if one == "" {
			return nil
		}
		return []string{one}
	}
	var many []string
	if json.Unmarshal(raw, &many) != nil {
		return nil
	}
	return many
}

func audOK(aud []string, clientID, audience string) bool {
	if clientID == "" && audience == "" {
		return false
	}
	for _, a := range aud {
		if clientID != "" && a == clientID {
			return true
		}
		if audience != "" && a == audience {
			return true
		}
	}
	return false
}

func sameIssuer(claim, want string) bool {
	claim = strings.TrimRight(strings.TrimSpace(claim), "/")
	want = strings.TrimRight(strings.TrimSpace(want), "/")
	return claim != "" && want != "" && claim == want
}

func decodeB64(s string) ([]byte, error) {
	if out, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return out, nil
	}
	return base64.URLEncoding.DecodeString(s)
}

func verifySig(alg string, pub crypto.PublicKey, signed string, sig []byte) error {
	sum, hashID, err := jwtHash(alg, signed)
	if err != nil {
		return err
	}
	switch key := pub.(type) {
	case *rsa.PublicKey:
		if !strings.HasPrefix(alg, "RS") {
			return errUserSignature
		}
		if rsa.VerifyPKCS1v15(key, hashID, sum, sig) != nil {
			return errUserSignature
		}
		return nil
	case *ecdsa.PublicKey:
		if !strings.HasPrefix(alg, "ES") {
			return errUserSignature
		}
		size := (key.Curve.Params().BitSize + 7) / 8
		if len(sig) != 2*size {
			return errUserSignature
		}
		r := new(big.Int).SetBytes(sig[:size])
		s := new(big.Int).SetBytes(sig[size:])
		if !ecdsa.Verify(key, sum, r, s) {
			return errUserSignature
		}
		return nil
	default:
		return errUserSignature
	}
}

func jwtHash(alg, signed string) ([]byte, crypto.Hash, error) {
	var h hash.Hash
	var id crypto.Hash
	switch alg {
	case "RS256", "ES256":
		h = sha256.New()
		id = crypto.SHA256
	case "RS384", "ES384":
		h = sha512.New384()
		id = crypto.SHA384
	case "RS512", "ES512":
		h = sha512.New()
		id = crypto.SHA512
	default:
		return nil, 0, errUserSignature
	}
	_, _ = h.Write([]byte(signed))
	return h.Sum(nil), id, nil
}

func (c *jwksCache) keys(ctx context.Context, client *http.Client, uri string) ([]sigKey, error) {
	c.mu.Lock()
	if c.m == nil {
		c.m = map[string][]sigKey{}
	}
	if keys, ok := c.m[uri]; ok && len(keys) > 0 {
		c.mu.Unlock()
		return keys, nil
	}
	c.mu.Unlock()
	keys, err := fetchJWKS(ctx, client, uri)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.m[uri] = keys
	c.mu.Unlock()
	return keys, nil
}

func (c *jwksCache) drop(uri string) {
	c.mu.Lock()
	delete(c.m, uri)
	c.mu.Unlock()
}

func fetchJWKS(ctx context.Context, client *http.Client, uri string) ([]sigKey, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, errUserUnchecked
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errUserUnchecked
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil, errUserUnchecked
	}
	var doc struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			Alg string `json:"alg"`
			Use string `json:"use"`
			N   string `json:"n"`
			E   string `json:"e"`
			Crv string `json:"crv"`
			X   string `json:"x"`
			Y   string `json:"y"`
		} `json:"keys"`
	}
	if json.Unmarshal(body, &doc) != nil || len(doc.Keys) == 0 {
		return nil, errUserUnchecked
	}
	var keys []sigKey
	for _, k := range doc.Keys {
		if k.Use != "" && k.Use != "sig" {
			continue
		}
		pub, err := parseJWK(k.Kty, k.N, k.E, k.Crv, k.X, k.Y)
		if err != nil {
			continue
		}
		keys = append(keys, sigKey{kid: k.Kid, alg: k.Alg, pub: pub})
	}
	if len(keys) == 0 {
		return nil, errUserUnchecked
	}
	return keys, nil
}

func parseJWK(kty, n, e, crv, x, y string) (crypto.PublicKey, error) {
	switch kty {
	case "RSA":
		nb, err := decodeB64(n)
		if err != nil {
			return nil, err
		}
		eb, err := decodeB64(e)
		if err != nil {
			return nil, err
		}
		ei := new(big.Int).SetBytes(eb)
		if !ei.IsInt64() || ei.Int64() <= 0 {
			return nil, fmt.Errorf("bad exponent")
		}
		return &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: int(ei.Int64())}, nil
	case "EC":
		var curve elliptic.Curve
		switch crv {
		case "P-256":
			curve = elliptic.P256()
		case "P-384":
			curve = elliptic.P384()
		case "P-521":
			curve = elliptic.P521()
		default:
			return nil, fmt.Errorf("unsupported curve")
		}
		xb, err := decodeB64(x)
		if err != nil {
			return nil, err
		}
		yb, err := decodeB64(y)
		if err != nil {
			return nil, err
		}
		pub := &ecdsa.PublicKey{Curve: curve, X: new(big.Int).SetBytes(xb), Y: new(big.Int).SetBytes(yb)}
		if !curve.IsOnCurve(pub.X, pub.Y) {
			return nil, fmt.Errorf("point is not on the curve")
		}
		return pub, nil
	default:
		return nil, fmt.Errorf("unsupported key type")
	}
}
