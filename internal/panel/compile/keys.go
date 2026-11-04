package compile

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
)

// SSUserKey derives a user's Shadowsocks-2022 key from their UUID: the UUID
// is 16 random bytes, exactly the key size of 2022-blake3-aes-128-gcm.
func SSUserKey(uuid string) string {
	b, err := hex.DecodeString(strings.ReplaceAll(uuid, "-", ""))
	if err != nil || len(b) != 16 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(b)
}
