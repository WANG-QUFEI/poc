package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func init() {
	switch Environment() {
	case "staging", "sandbox", "production":
		// do nothing in cloud env
	default:
		err := maybeLoadDotEnv()
		if err != nil {
			log.Fatal().Err(err).Msg("failed to load .env file if it is present")
		}
	}
	time.Local = time.UTC
	zerolog.TimeFieldFormat = time.RFC3339Nano
	if zerolog.DefaultContextLogger == nil {
		zerolog.DefaultContextLogger = &log.Logger
	}
	zerolog.SetGlobalLevel(logLevel())
}

func Environment() string {
	return os.Getenv("ENVIRONMENT")
}

func DatabaseURL() string {
	return os.Getenv("DATABASE_URL")
}

func WebServicePort() int {
	return 8080
}

func GrpcPort() int {
	port := 50051
	s := os.Getenv("GRPC_PORT")
	if s != "" {
		p, err := strconv.Atoi(s)
		if err != nil {
			log.Fatal().Err(err).Msgf("failed to parse GRPC_PORT: %s", s)
		}
		port = p
	}

	return port
}

func RESTApiPath() string {
	path := os.Getenv("REST_DEVICE_DATA_PATH")
	if path == "" {
		path = "/api/data"
	}
	return path
}

func RESTApiPort() int {
	port := 8080
	s := os.Getenv("REST_PORT")
	if s != "" {
		p, err := strconv.Atoi(s)
		if err != nil {
			log.Fatal().Err(err).Msgf("failed to parse REST_PORT: %s", s)
		}
		port = p
	}

	return port
}

func RESTSchema() string {
	s := os.Getenv("REST_SCHEMA")
	if s == "" {
		s = "http"
	}
	return s
}

func HealthCheckPath() string {
	path := os.Getenv("HEALTH_CHECK_PATH")
	if path == "" {
		path = "/health"
	}
	return path
}

func HealthCheckTimeout() time.Duration {
	timeout := os.Getenv("HEALTH_CHECK_TIMEOUT")
	if timeout == "" {
		return 5 * time.Second
	}
	t, err := time.ParseDuration(timeout)
	if err != nil {
		log.Fatal().Err(err).Msgf("failed to parse HEALTH_CHECK_TIMEOUT: %s", timeout)
	}
	return t
}

func ExternalChecksumGeneratorLocation() string {
	location := os.Getenv("EXTERNAL_CHECKSUM_GENERATOR_LOCATION")
	if location == "" {
		return "/app/checksum_gen"
	}
	return location
}

func EnableGormLogging() bool {
	enable := os.Getenv("ENABLE_GORM_LOGGING")
	if enable == "" {
		return false
	}
	b, err := strconv.ParseBool(enable)
	if err != nil {
		log.Fatal().Err(err).Msgf("failed to parse ENABLE_GORM_LOGGING: %s", enable)
	}
	return b
}

func GetPollingBatchSize() int {
	batchSize := 100
	s := os.Getenv("POLLING_BATCH_SIZE")
	if s != "" {
		b, err := strconv.Atoi(s)
		if err != nil {
			log.Fatal().Err(err).Msgf("failed to parse POLLING_BATCH_SIZE: %s", s)
		}
		batchSize = b
	}

	return batchSize
}

func maybeLoadDotEnv() error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	for {
		candidate := filepath.Join(dir, ".env")
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			return godotenv.Load(candidate)
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
		d := filepath.Dir(dir)
		if d == dir {
			break
		}
		dir = d
	}

	return nil
}

func logLevel() zerolog.Level {
	level := os.Getenv("LOG_LEVEL")
	if strings.EqualFold("debug", level) {
		return zerolog.DebugLevel
	}
	if strings.EqualFold("warn", level) {
		return zerolog.WarnLevel
	}
	if strings.EqualFold("error", level) {
		return zerolog.ErrorLevel
	}
	if strings.EqualFold("fatal", level) {
		return zerolog.FatalLevel
	}

	return zerolog.InfoLevel
}
