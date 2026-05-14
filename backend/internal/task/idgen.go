package task

import (
	"crypto/rand"
	"encoding/hex"
)

// newTaskID returns a 12-character lowercase hex string (48 bits of randomness).
// Shorter than a UUID so it doesn't cause model hallucination when used in /workspace CWD paths.
func newTaskID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic("task: failed to read random bytes: " + err.Error())
	}
	return hex.EncodeToString(b)
}
