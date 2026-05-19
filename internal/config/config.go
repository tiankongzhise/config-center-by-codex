package config

import (
	"bufio"
	"fmt"
	"os"
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
	AuthLimitServiceID   string
	AuthLimitAppID       string
	AuthLimitAppSecret   string
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
		AuthLimitServiceID:   get("AUTH_LIMIT_SERVICE_ID", ""),
		AuthLimitAppID:       get("AUTH_LIMIT_APP_ID", ""),
		AuthLimitAppSecret:   get("AUTH_LIMIT_APP_SECRET", ""),
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
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
