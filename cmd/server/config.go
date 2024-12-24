package main

import (
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/jaredLunde/railway-images/internal/pkg/logger"
)

type Config struct {
	Host        string `env:"HOST" envDefault:"[::]"`
	Port        int    `env:"PORT" envDefault:"3000"`
	CertFile    string `env:"CERT_FILE" envDefault:""`
	CertKeyFile string `env:"CERT_KEY_FILE" envDefault:""`
	// The maximum duration for reading the entire request, including the body
	RequestTimeout time.Duration `env:"REQUEST_TIMEOUT" envDefault:"30s"`
	// Allowed origins for CORS
	CORSAllowedOrigins string `env:"CORS_ALLOWED_ORIGINS" envDefault:"*"`

	// The maximum size of a request body in bytes
	MaxUploadSize int `env:"MAX_UPLOAD_SIZE" envDefault:"10485760"` // 10MB
	// The path to the directory where uploaded files are stored
	UploadPath string `env:"UPLOAD_PATH" envDefault:"/data/uploads"`
	// The path to the LevelDB database
	LevelDBPath string `env:"LEVELDB_PATH" envDefault:"/data/db"`
	// Used for securing the key value storage API
	SecretKey string `env:"SECRET_KEY" envDefault:"password"`
	// Used for signing URLs
	SignatureSecretKey string `env:"SIGNATURE_SECRET_KEY" envDefault:"secret"`

	// A comma-separated list of allowed URL sources
	ServeAllowedHTTPSources string `env:"SERVE_ALLOWED_HTTP_SOURCES" envDefault:"*"`
	// Automatically convert images to WebP
	ServeAutoWebP bool `env:"SERVE_AUTO_WEBP" envDefault:"true"`
	// Automatically convert images to AVIF
	ServeAutoAVIF bool `env:"SERVE_AUTO_AVIF" envDefault:"true"`
	// The max number of images to process concurrently
	ServeConcurrency int `env:"SERVE_CONCURRENCY" envDefault:"20"`
	// The duration to cache processed images
	ServeCacheTTL time.Duration `env:"SERVE_RESULT_CACHE_TTL" envDefault:"24h"`
	// The TTL for the Cache-Control header
	ServeCacheControlTTL time.Duration `env:"SERVE_CACHE_CONTROL_TTL" envDefault:"8760h"`
	// The SWR time for the Cache-Control header
	ServeCacheControlSWR time.Duration `env:"SERVE_CACHE_CONTROL_SWR" envDefault:"24h"`

	Environment Environment     `env:"ENVIRONMENT" envDefault:"production"`
	LogLevel    logger.LogLevel `env:"LOG_LEVEL" envDefault:"info"`
}

type Environment string

const (
	EnvironmentDevelopment Environment = "development"
	EnvironmentProduction  Environment = "production"
)

func LoadConfig() (cfg Config, err error) {
	cfg = Config{}
	if err = env.ParseWithOptions(&cfg, env.Options{RequiredIfNoDef: true}); err != nil {
		return
	}

	return
}
