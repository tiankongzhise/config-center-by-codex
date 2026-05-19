package config

import (
	"bufio"
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"
)

const (
	defaultAddr             = ":8080"
	defaultBaseURL          = "http://localhost:8080"
	defaultAuthLimitBaseURL = "https://auth-limit.baichengedu.com"
)

type Config struct {
	EnvPath string

	Addr    string
	BaseURL string

	DatabaseURL string

	PostgresHost        string
	PostgresPort        string
	PostgresAdmin       string
	PostgresAdminSecret string

	ConfigDBName     string
	ConfigDBUser     string
	ConfigDBPassword string

	AuthLimitBaseURL     string
	AuthLimitAdmin       string
	AuthLimitAdminSecret string
	AuthLimitServiceCode string
	AuthLimitServiceName string
	AuthLimitServiceID   string
	AuthLimitAppName     string
	AuthLimitAppID       string
	AuthLimitAppSecret   string
	AllowLocalServiceURL bool
}

func Load(path string) (Config, error) {
	values, err := readDotEnv(path)
	if err != nil {
		return Config{}, err
	}

	get := func(key, fallback string) string {
		if value, ok := os.LookupEnv(key); ok {
			return strings.TrimSpace(value)
		}
		if value, ok := values[key]; ok {
			return strings.TrimSpace(value)
		}
		return fallback
	}

	cfg := Config{
		EnvPath: path,

		Addr:    get("CONFIG_CENTER_ADDR", defaultAddr),
		BaseURL: strings.TrimRight(get("CONFIG_CENTER_BASE_URL", defaultBaseURL), "/"),

		DatabaseURL: get("CONFIG_CENTER_DATABASE_URL", ""),

		PostgresHost:        get("PG_HOST", "127.0.0.1"),
		PostgresPort:        get("PG_PORT", "5432"),
		PostgresAdmin:       get("PG_ADMIN", ""),
		PostgresAdminSecret: get("PG_ADMIN_SECRET", ""),

		ConfigDBName:     get("CONFIG_CENTER_DB_NAME", "config_center"),
		ConfigDBUser:     get("CONFIG_CENTER_DB_USER", "config_center"),
		ConfigDBPassword: get("CONFIG_CENTER_DB_PASSWORD", ""),

		AuthLimitBaseURL:     strings.TrimRight(get("AUTH_LIMIT_BASE_URL", defaultAuthLimitBaseURL), "/"),
		AuthLimitAdmin:       firstNonEmpty(get("AUTH_LIMIT_ADMIN", ""), get("AUTH_SERVICE_ADMIN", "")),
		AuthLimitAdminSecret: firstNonEmpty(get("AUTH_LIMIT_ADMIN_SECRET", ""), get("AUTH_SERVICE_ADMIN_SECRET", "")),
		AuthLimitServiceCode: get("AUTH_LIMIT_SERVICE_CODE", "config-center"),
		AuthLimitServiceName: get("AUTH_LIMIT_SERVICE_NAME", "配置中心"),
		AuthLimitServiceID:   get("AUTH_LIMIT_SERVICE_ID", ""),
		AuthLimitAppName:     get("AUTH_LIMIT_APP_NAME", "config-center"),
		AuthLimitAppID:       get("AUTH_LIMIT_APP_ID", ""),
		AuthLimitAppSecret:   get("AUTH_LIMIT_APP_SECRET", ""),
		AllowLocalServiceURL: parseBool(get("ALLOW_LOCAL_SERVICE_URL", "")),
	}

	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}

	if cfg.DatabaseURL == "" && cfg.ConfigDBPassword != "" {
		cfg.DatabaseURL = fmt.Sprintf(
			"postgres://%s:%s@%s:%s/%s?sslmode=disable",
			urlEscape(cfg.ConfigDBUser),
			urlEscape(cfg.ConfigDBPassword),
			cfg.PostgresHost,
			cfg.PostgresPort,
			urlEscape(cfg.ConfigDBName),
		)
	}

	return cfg, nil
}

func (cfg Config) ValidatePublicServiceURL() error {
	parsed, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return fmt.Errorf("CONFIG_CENTER_BASE_URL is invalid: %w", err)
	}
	if parsed.Scheme != "https" {
		if cfg.AllowLocalServiceURL {
			return nil
		}
		return fmt.Errorf("CONFIG_CENTER_BASE_URL must be a public https URL before registering to auth-limit, got %q", cfg.BaseURL)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("CONFIG_CENTER_BASE_URL must include a host")
	}
	if isLocalHost(host) && !cfg.AllowLocalServiceURL {
		return fmt.Errorf("CONFIG_CENTER_BASE_URL must not be localhost or a private address before registering to auth-limit, got %q", cfg.BaseURL)
	}
	return nil
}

func readDotEnv(path string) (map[string]string, error) {
	values := make(map[string]string)
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return values, nil
		}
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key != "" {
			values[key] = value
		}
	}
	return values, scanner.Err()
}

func UpdateDotEnv(path string, updates map[string]string) error {
	lines, err := readDotEnvLines(path)
	if err != nil {
		return err
	}

	seen := make(map[string]bool, len(updates))
	for index, line := range lines {
		key, _, ok := parseEnvLine(line)
		if !ok {
			continue
		}
		value, exists := updates[key]
		if !exists {
			continue
		}
		lines[index] = key + "=" + quoteEnvValue(value)
		seen[key] = true
	}

	var missing []string
	for key := range updates {
		if !seen[key] {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 && len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
		lines = append(lines, "")
	}
	for _, key := range missing {
		lines = append(lines, key+"="+quoteEnvValue(updates[key]))
	}

	content := strings.Join(lines, "\n")
	if content != "" {
		content += "\n"
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func readDotEnvLines(path string) ([]string, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	content := strings.ReplaceAll(string(bytes), "\r\n", "\n")
	content = strings.TrimSuffix(content, "\n")
	if content == "" {
		return nil, nil
	}
	return strings.Split(content, "\n"), nil
}

func parseEnvLine(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	key, value, ok := strings.Cut(trimmed, "=")
	if !ok {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false
	}
	return key, strings.Trim(strings.TrimSpace(value), `"'`), true
}

func quoteEnvValue(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, " #\"'") {
		escaped := strings.ReplaceAll(value, `"`, `\"`)
		return `"` + escaped + `"`
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func isLocalHost(host string) bool {
	lower := strings.ToLower(host)
	if lower == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

func urlEscape(value string) string {
	replacer := strings.NewReplacer(
		" ", "%20",
		"@", "%40",
		":", "%3A",
		"/", "%2F",
		"?", "%3F",
		"#", "%23",
		"&", "%26",
		"=", "%3D",
	)
	return replacer.Replace(value)
}
