//go:build ignore

package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

func main() {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	salt := hex.EncodeToString(b)
	sum := sha256.Sum256([]byte(salt + "subport-admin"))
	fmt.Printf("%s|%s\n", salt, hex.EncodeToString(sum[:]))
}
