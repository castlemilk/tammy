//go:build !tammy_sqlcipher

package main

import "io"

func reportSQLCipher([]string, io.Writer, io.Writer) (bool, int) {
	return false, 0
}
