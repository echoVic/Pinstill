package store

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"time"
)

// idAlphabet contains the characters allowed in the random suffix of an ID.
const idAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// idAlphabetMax is the largest multiple of len(idAlphabet) that fits in a
// byte; random bytes above it are rejected to avoid modulo bias.
const idAlphabetMax = 256 / len(idAlphabet) * len(idAlphabet)

// idPattern matches the frozen ID format: 20060102-150405-[a-z0-9]{4}.
var idPattern = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[a-z0-9]{4}$`)

// newID builds an ID for a screenshot taken at createdAt. The ID is safe to use
// as a file name on every platform.
func newID(createdAt time.Time) (string, error) {
	suffix := make([]byte, 4)
	buffer := make([]byte, 8)
	for filled := 0; filled < len(suffix); {
		if _, err := rand.Read(buffer); err != nil {
			return "", fmt.Errorf("store: generate id: %w", err)
		}
		for _, b := range buffer {
			if int(b) >= idAlphabetMax {
				continue
			}
			suffix[filled] = idAlphabet[int(b)%len(idAlphabet)]
			filled++
			if filled == len(suffix) {
				break
			}
		}
	}
	return createdAt.UTC().Format("20060102-150405") + "-" + string(suffix), nil
}

// validID reports whether id matches the frozen, filesystem-safe format.
func validID(id string) bool {
	return idPattern.MatchString(id)
}

// pngName returns the PNG file name for id.
func pngName(id string) string { return id + pngExt }

// thumbName returns the JPEG thumbnail file name for id.
func thumbName(id string) string { return id + thumbExt }
