package util

import (
	"math/rand"
)

const (
	rnb = "0123456789"
	rlb = "abcdefghijklmnopqrstuvwxyz"
	rLb = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"

	rnbl = int64(10)
	rlbl = int64(26)
	rLbl = int64(26)
)

func RandStr(n int) string {
	if n <= 0 {
		n = 12
	}
	buff := make([]byte, n)
	for i := 0; i < n; i++ {
		switch rand.Intn(3) {
		case 0:
			buff[i] = rnb[rand.Int63()%rnbl]
		case 1:
			buff[i] = rlb[rand.Int63()%rlbl]
		case 2:
			buff[i] = rLb[rand.Int63()%rLbl]
		}
	}
	return string(buff)
}
