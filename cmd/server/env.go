package main

import "os"

func secretEnvString(fallback string, names ...string) string {
	for _, name := range names {
		if v := os.Getenv(name); v != "" {
			return v
		}
	}
	return fallback
}
