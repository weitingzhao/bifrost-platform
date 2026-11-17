package server

import (
	gitsha1 "crypto/sha1" // Git object identity, not a credential or signature.
	"fmt"
)

func gitBlobID(content string) string {
	return fmt.Sprintf("%x", gitsha1.Sum([]byte(fmt.Sprintf("blob %d\x00%s", len(content), content))))
}
