package postmangen

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ResolveEnvironment applies Postman values from the project .env file and the
// process environment. Explicit option values take precedence, followed by the
// process environment, .env and generator defaults.
func ResolveEnvironment(opts Options) (Options, map[string]string, error) {
	root := strings.TrimSpace(opts.Root)
	if root == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Options{}, nil, fmt.Errorf("resolve project root: %w", err)
	}
	env, err := loadEnvironment(absRoot, os.Environ())
	if err != nil {
		return Options{}, nil, err
	}
	opts.Root = absRoot
	applyEnv := func(current *string, key string) {
		if strings.TrimSpace(*current) == "" {
			*current = strings.TrimSpace(env[key])
		}
	}
	applyEnv(&opts.Out, "POSTMAN_COLLECTION_FILE")
	applyEnv(&opts.Name, "POSTMAN_COLLECTION_NAME")
	applyEnv(&opts.ConfigPath, "POSTMAN_CONFIG_PATH")
	applyEnv(&opts.BaseURL, "POSTMAN_BASE_URL")
	applyEnv(&opts.AdminURL, "POSTMAN_ADMIN_URL")
	applyEnv(&opts.UploadURL, "POSTMAN_UPLOAD_URL")
	applyEnv(&opts.AdminKey, "POSTMAN_ADMIN_KEY")
	applyEnv(&opts.AuthKey, "POSTMAN_AUTH_KEY")
	return opts, env, nil
}

// GenerateFromEnvironment generates a collection using project .env values,
// while allowing command-line options to override them.
func GenerateFromEnvironment(opts Options) (Result, error) {
	resolved, _, err := ResolveEnvironment(opts)
	if err != nil {
		return Result{}, err
	}
	return Generate(resolved)
}

func loadEnvironment(root string, process []string) (map[string]string, error) {
	env := map[string]string{}
	path := filepath.Join(root, ".env")
	file, err := os.Open(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		lineNumber := 0
		for scanner.Scan() {
			lineNumber++
			key, value, ok, parseErr := parseDotEnvLine(scanner.Text())
			if parseErr != nil {
				return nil, fmt.Errorf("parse %s:%d: %w", path, lineNumber, parseErr)
			}
			if ok {
				env[key] = value
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
	}
	for _, entry := range process {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			env[key] = value
		}
	}
	return env, nil
}

func parseDotEnvLine(line string) (string, string, bool, error) {
	line = strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false, nil
	}
	if strings.HasPrefix(line, "export ") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
	}
	key, raw, ok := strings.Cut(line, "=")
	if !ok {
		return "", "", false, errors.New("expected KEY=VALUE")
	}
	key = strings.TrimSpace(key)
	if !validEnvKey(key) {
		return "", "", false, fmt.Errorf("invalid variable name %q", key)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return key, "", true, nil
	}
	if raw[0] == '\'' {
		value, err := parseQuotedDotEnvValue(raw, '\'')
		return key, value, true, err
	}
	if raw[0] == '"' {
		quoted, err := parseQuotedDotEnvValue(raw, '"')
		if err != nil {
			return "", "", false, err
		}
		value, err := strconv.Unquote("\"" + quoted + "\"")
		if err != nil {
			return "", "", false, fmt.Errorf("invalid double-quoted value: %w", err)
		}
		return key, value, true, nil
	}
	if index := inlineCommentIndex(raw); index >= 0 {
		raw = strings.TrimSpace(raw[:index])
	}
	return key, raw, true, nil
}

func parseQuotedDotEnvValue(raw string, quote byte) (string, error) {
	escaped := false
	for index := 1; index < len(raw); index++ {
		if quote == '"' && raw[index] == '\\' && !escaped {
			escaped = true
			continue
		}
		if raw[index] == quote && !escaped {
			trailing := strings.TrimSpace(raw[index+1:])
			if trailing != "" && !strings.HasPrefix(trailing, "#") {
				return "", errors.New("unexpected text after quoted value")
			}
			return raw[1:index], nil
		}
		escaped = false
	}
	return "", errors.New("unterminated quoted value")
}

func validEnvKey(key string) bool {
	if key == "" || !isEnvKeyStart(key[0]) {
		return false
	}
	for i := 1; i < len(key); i++ {
		if !isEnvKeyStart(key[i]) && (key[i] < '0' || key[i] > '9') {
			return false
		}
	}
	return true
}

func isEnvKeyStart(value byte) bool {
	return value == '_' || value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func inlineCommentIndex(value string) int {
	for i := 1; i < len(value); i++ {
		if value[i] == '#' && (value[i-1] == ' ' || value[i-1] == '\t') {
			return i
		}
	}
	return -1
}
