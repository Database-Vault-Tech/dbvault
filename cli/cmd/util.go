package cmd

import "os"

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown host"
	}
	return h
}
