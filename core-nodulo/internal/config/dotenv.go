package config

import (
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

// LoadDotEnv loads the repository's .env when it exists. Values already set
// in the process environment take precedence, which keeps deployment secrets
// injected by Docker, Coolify, or the shell authoritative.
func LoadDotEnv() (string, error) {
	paths := []string{filepath.Join("core-nodulo", ".env"), ".env"}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", err
		}
		if info.IsDir() {
			continue
		}
		if err := godotenv.Load(path); err != nil {
			return "", err
		}
		return path, nil
	}
	return "", nil
}
