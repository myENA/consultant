package util

import (
	"math/rand"
)

const (
	rnb = "0123456789"
	rlb = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
)

var (
	rnbl = int64(10)
	rlbl = int64(52)
)

func RandStr(n int) string {
	if n <= 0 {
		n = 12
	}
	buff := make([]byte, n)
	for i := 0; i < n; i++ {
		switch rand.Intn(1) {
		case 0:
			buff[i] = rnb[rand.Int63()%rnbl]
		default:
			buff[i] = rlb[rand.Int63()%rlbl]
		}
	}
	return string(buff)
}
