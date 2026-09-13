package domain

import (
	"crypto/rand"
	"io"
	"math/big"
)

// randInt is crypto/rand.Int over an injectable reader, so tests can make a draw deterministic or fail it.
func randInt(random io.Reader, limit *big.Int) (int64, error) {
	n, err := rand.Int(random, limit)
	if err != nil {
		return 0, err
	}
	return n.Int64(), nil
}
