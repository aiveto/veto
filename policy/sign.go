package policy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type signer interface {
	sign(caller, opID string, params map[string]string, exp time.Time) string
	consume(caller, token, opID string, params map[string]string, now time.Time) (bool, error)
}

type hmacSigner struct {
	secret []byte
	dir    string
}

func (h hmacSigner) sign(caller, opID string, params map[string]string, exp time.Time) string {
	unix := exp.Unix()
	nonce := uuid.NewString()
	mac := hmac.New(sha256.New, h.secret)
	_, _ = mac.Write([]byte(approvalPayload(caller, opID, params, unix, nonce)))
	sum := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("v1.%d.%s.%s", unix, nonce, sum)
}

func (h hmacSigner) consume(caller, token, opID string, params map[string]string, now time.Time) (bool, error) {
	unix, nonce, sum, ok := parseApproval(token, now)
	if !ok {
		return false, nil
	}
	mac := hmac.New(sha256.New, h.secret)
	_, _ = mac.Write([]byte(approvalPayload(caller, opID, params, unix, nonce)))
	if !hmac.Equal(sum, mac.Sum(nil)) {
		return false, nil
	}
	if h.dir == "" {
		return false, errors.New("approval nonce dir is not set")
	}
	nonce = filepath.Base(nonce)
	if nonce == "" || nonce == "." || !plainNonce(nonce) {
		return false, nil
	}
	f, err := os.OpenFile(filepath.Join(h.dir, nonce), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return false, nil
		}
		return false, fmt.Errorf("approval nonce: %w", err)
	}
	_, werr := fmt.Fprintf(f, "%d\n", unix)
	cerr := f.Close()
	if werr != nil {
		return false, fmt.Errorf("approval nonce: %w", werr)
	}
	if cerr != nil {
		return false, fmt.Errorf("approval nonce: %w", cerr)
	}
	return true, nil
}

func parseApproval(token string, now time.Time) (unix int64, nonce string, sum []byte, ok bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 4 || parts[0] != "v1" || !plainNonce(parts[2]) {
		return 0, "", nil, false
	}
	unix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || !now.Before(time.Unix(unix, 0)) {
		return 0, "", nil, false
	}
	sum, err = base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return 0, "", nil, false
	}
	return unix, parts[2], sum, true
}

func approvalPayload(caller, opID string, params map[string]string, exp int64, nonce string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n%s\n%d\n%s", opID, caller, exp, nonce)
	for _, k := range slices.Sorted(maps.Keys(params)) {
		fmt.Fprintf(&b, "\n%s=%s", k, params[k])
	}
	return b.String()
}

func plainNonce(nonce string) bool {
	if nonce == "" || len(nonce) > 128 {
		return false
	}
	for _, r := range nonce {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

func plainID(id string) bool {
	return plainNonce(id) && !strings.Contains(id, ".")
}
