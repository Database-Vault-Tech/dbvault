//go:build !unix

package sqlite

import "os"

func chownLike(string, os.FileInfo) {}
