// Package config ports config.py: it loads the bot's configuration from the
// environment (and an optional .env file) and exposes it as a typed struct.
//
// Parsing semantics intentionally match the Python original so the same .env
// keeps working unchanged during the cutover:
//   - ADMINS is split on '|'
//   - GM_TIME / GN_TIME are "HH:MM" -> [hour, minute]
//   - SEND_GM_GN is a case-insensitive "true"
//   - the same keys are required vs optional as in config.py
package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Hard-coded constants mirroring the tail of config.py.
const (
	MaxTryAddCID            = 5
	DeletionTimeout         = 10
	DeletionTimeoutExtended = DeletionTimeout + 5
	DefaultAudioTag         = "[ناشناس]"
	// AllowedCIDChars must NEVER contain '|'. Kept here for parity with config.py;
	// the cipher itself uses the copy inside the encoder package.
	AllowedCIDChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"
	ExpireAfter     = 0.3 // seconds
	KeyMaxInt       = 100
)

// DeletionText mirrors config.DELETION_TEXT (a %d-second countdown notice).
const DeletionText = "%d ثانیه فرصت داری با دکمه ی زیر پاکش کنی.\n" +
	"<blockquote>غیرفعال‌سازیِ اخطار توی منوی تنظیماته</blockquote>"

// Config holds every runtime setting the bot needs.
type Config struct {
	BotToken string
	BotID    string // BOT_TOKEN before the first ':'
	Proxy    string

	ReportChatID string
	ErrorChatID  string
	Admins       []string
	SellerAdmin  string
	SupportAdmin string

	DBName string
	DBUser string
	DBPass string
	DBHost string
	DBPort int // optional (DB_PORT), defaults to 5432; config.py had no such key

	LogLevel string

	DefaultCIDLimit int
	MaxNameLength   int
	MaxCIDLength    int
	MinCIDLength    int

	HealthPort    int
	BotHealthPort int
	BaseAddress   string

	SendGMGN     bool
	GMTime       [2]int // hour, minute
	GNTime       [2]int
	GMGroupID    string
	GMGroupTopic string

	AIURL       string
	AISessionID string
	AIInterval  int

	DonationLink string
}

// Load reads configuration from the process environment. If a .env file exists
// in the working directory it is loaded first (without overriding variables
// that are already set, matching python-dotenv's default behavior).
func Load() (*Config, error) {
	if err := loadDotEnv(".env"); err != nil {
		return nil, err
	}

	var missing []string
	req := func(key string) string {
		v, ok := os.LookupEnv(key)
		if !ok || v == "" {
			missing = append(missing, key)
		}
		return v
	}
	opt := func(key, def string) string {
		if v, ok := os.LookupEnv(key); ok {
			return v
		}
		return def
	}

	c := &Config{}
	c.BotToken = req("BOT_TOKEN")
	if c.BotToken != "" {
		c.BotID = strings.SplitN(c.BotToken, ":", 2)[0]
	}
	c.Proxy = opt("PROXY", "")

	c.ReportChatID = opt("REPORT_CHAT_ID", "")
	c.ErrorChatID = opt("ERROR_CHAT_ID", "")
	if admins := opt("ADMINS", ""); admins != "" {
		c.Admins = strings.Split(admins, "|")
	}
	c.SellerAdmin = opt("SELLER_ADMIN", "")
	c.SupportAdmin = opt("SUPPORT_ADMIN", "")

	c.DBName = opt("DB_NAME", "")
	c.DBUser = opt("DB_USER", "")
	c.DBPass = opt("DB_PASS", "")
	c.DBHost = opt("DB_HOST", "localhost")

	c.LogLevel = opt("LOG_LEVEL", "INFO")
	c.BaseAddress = opt("BASE_ADDRESS", "/health")
	c.GMGroupTopic = opt("GM_GROUP_TOPIC_ID", "")
	c.AIURL = opt("AI_URL", "")
	c.AISessionID = opt("AI_SESSION_ID", "")

	// errors are accumulated so the operator sees every problem at once.
	var errs []string
	mustInt := func(key string) int {
		v := req(key)
		if v == "" {
			return 0
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s must be an integer (got %q)", key, v))
		}
		return n
	}

	c.DefaultCIDLimit = mustInt("DEFAULT_CID_LIMIT")
	c.MaxNameLength = mustInt("MAX_NAME_LENGTH")
	c.MaxCIDLength = mustInt("MAX_CID_LENGTH")
	c.MinCIDLength = mustInt("MIN_CID_LENGTH")
	c.HealthPort = mustInt("HEALTH_PORT")

	if v, ok := os.LookupEnv("BOT_HEALTH_PORT"); ok && v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			errs = append(errs, fmt.Sprintf("BOT_HEALTH_PORT must be an integer (got %q)", v))
		}
		c.BotHealthPort = n
	} else {
		c.BotHealthPort = c.HealthPort
	}

	// DB_PORT is optional (psycopg2 defaulted to 5432).
	c.DBPort = 5432
	if v, ok := os.LookupEnv("DB_PORT"); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.DBPort = n
		} else {
			errs = append(errs, fmt.Sprintf("DB_PORT must be an integer (got %q)", v))
		}
	}

	// AI_INTERVAL defaults to 5 (matches config.py's get with default 5).
	c.AIInterval = 5
	if v, ok := os.LookupEnv("AI_INTERVAL"); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.AIInterval = n
		} else {
			errs = append(errs, fmt.Sprintf("AI_INTERVAL must be an integer (got %q)", v))
		}
	}

	c.SendGMGN = strings.EqualFold(req("SEND_GM_GN"), "true")
	c.GMTime = parseHHMM("GM_TIME", req("GM_TIME"), &errs)
	c.GNTime = parseHHMM("GN_TIME", req("GN_TIME"), &errs)
	c.GMGroupID = req("GM_GROUP_ID")

	c.DonationLink = req("DONATION_LINK")

	if len(missing) > 0 {
		errs = append(errs, "missing required environment variables: "+strings.Join(missing, ", "))
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("config: %s", strings.Join(errs, "; "))
	}
	return c, nil
}

// parseHHMM parses "HH:MM" into [hour, minute], mirroring config.py's
// [int(x.strip()) for x in env.split(":")].
func parseHHMM(key, raw string, errs *[]string) [2]int {
	if raw == "" {
		return [2]int{}
	}
	parts := strings.Split(raw, ":")
	if len(parts) < 2 {
		*errs = append(*errs, fmt.Sprintf("%s must be HH:MM (got %q)", key, raw))
		return [2]int{}
	}
	h, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	m, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil {
		*errs = append(*errs, fmt.Sprintf("%s must be HH:MM integers (got %q)", key, raw))
		return [2]int{}
	}
	return [2]int{h, m}
}

// loadDotEnv loads KEY=VALUE pairs from path into the environment without
// overriding already-set variables. A missing file is not an error. Supports
// '#' comments, blank lines, optional surrounding quotes, and an optional
// leading "export ".
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') ||
				(val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, val); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}
