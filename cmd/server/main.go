package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/favicon"
	"github.com/gofiber/fiber/v3/middleware/healthcheck"
	"github.com/gofiber/fiber/v3/middleware/helmet"
	fiberrecover "github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/jaredLunde/railway-images/internal/app/imagor"
	"github.com/jaredLunde/railway-images/internal/app/keyval"
	"github.com/jaredLunde/railway-images/internal/app/signature"
	"github.com/jaredLunde/railway-images/internal/pkg/logger"
	"github.com/jaredLunde/railway-images/internal/pkg/mw"
	"golang.org/x/sync/errgroup"
)

func main() {
	ctx := context.Background()
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := LoadConfig()
	if err != nil {
		panic(err)
	}

	debug := cfg.Environment == EnvironmentDevelopment
	log := logger.New(logger.Options{
		LogLevel: cfg.LogLevel,
		Pretty:   debug,
	})

	kvService, err := keyval.New(keyval.Config{
		BasePath:         "/files",
		UploadPath:       cfg.UploadPath,
		LevelDBPath:      cfg.LevelDBPath,
		SoftDelete:       true,
		SignSecret:       cfg.SignatureSecretKey,
		MaxSize:          cfg.MaxUploadSize,
		AllowedMimeTypes: []string{"image/"},
		Logger:           log,
		Debug:            debug,
	})
	if err != nil {
		log.Error("keyval app failed to start", "error", err)
		os.Exit(1)
	}
	defer kvService.Close()

	imagorService, err := imagor.New(ctx, imagor.Config{
		KeyVal:             kvService,
		UploadPath:         cfg.UploadPath,
		MaxUploadSize:      cfg.MaxUploadSize,
		SignSecret:         cfg.SignatureSecretKey,
		AllowedHTTPSources: cfg.ServeAllowedHTTPSources,
		AutoWebP:           cfg.ServeAutoWebP,
		AutoAVIF:           cfg.ServeAutoAVIF,
		ResultCacheTTL:     cfg.ServeCacheTTL,
		Concurrency:        cfg.ServeConcurrency,
		CacheControlTTL:    cfg.ServeCacheControlTTL,
		CacheControlSWR:    cfg.ServeCacheControlSWR,
		RequestTimeout:     cfg.RequestTimeout,
		Debug:              debug,
	})
	if err != nil {
		log.Error("imagor app failed to start", "error", err)
		os.Exit(1)
	}

	signatureService := signature.New(cfg.SignatureSecretKey)

	app := fiber.New(fiber.Config{
		StrictRouting:     true,
		BodyLimit:         cfg.MaxUploadSize, // This doesn't actually work with StreamBodyRequest, but it's here for good times
		WriteTimeout:      cfg.RequestTimeout,
		ReadTimeout:       cfg.RequestTimeout,
		StreamRequestBody: true,
		ReduceMemoryUsage: true, // memory costs money brah, i'm a poor
		JSONEncoder: func(v interface{}) ([]byte, error) {
			return json.MarshalWithOption(v, json.DisableHTMLEscape())
		},
		JSONDecoder: json.Unmarshal,
	})

	if cfg.Environment == EnvironmentDevelopment {
		log.Warn("running in development mode, signed URLs are not required")
	}
	if cfg.SecretKey == "" {
		log.Warn("no secret key provided, API key verification is disabled")
	}

	verifyAPIKey := mw.NewVerifyAPIKey(cfg.SecretKey)
	verifyAccess := mw.NewVerifyAccess(cfg.SecretKey, cfg.SignatureSecretKey)
	app.Use(mw.NewRealIP())
	app.Use(helmet.New(helmet.Config{
		HSTSPreloadEnabled:        true,
		HSTSMaxAge:                31536000,
		CrossOriginResourcePolicy: "cross-origin",
	}))
	app.Use(fiberrecover.New(fiberrecover.Config{EnableStackTrace: cfg.Environment == EnvironmentDevelopment}))
	app.Use(favicon.New())
	app.Use(requestid.New())
	corsAllowedOrigins := strings.Split(cfg.CORSAllowedOrigins, ",")
	app.Use(cors.New(cors.Config{
		AllowOrigins:        corsAllowedOrigins,
		AllowMethods:        []string{fiber.MethodGet, fiber.MethodHead, fiber.MethodPost, fiber.MethodPut, fiber.MethodPatch, fiber.MethodDelete, fiber.MethodOptions},
		AllowHeaders:        []string{"Origin", "Content-Type", "Accept", "Cache-Control", "If-Match", "If-None-Match", "x-api-key", "x-signature", "x-expire"},
		ExposeHeaders:       []string{"Content-Disposition", "X-Request-ID", "Content-Md5", "Content-Range", "Accept-Ranges", "ETag"},
		AllowPrivateNetwork: true,
		MaxAge:              int(time.Hour),
		AllowCredentials:    !slices.Contains(corsAllowedOrigins, "*"),
	}))
	app.Get(mw.HealthCheckEndpoint, healthcheck.NewHealthChecker())
	app.Use(mw.NewLogger(log.With("source", "http"), slog.LevelInfo))
	app.Get("/serve/*", adaptor.HTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		sig := q.Get("x-signature")
		if sig == "" {
			sig = r.Header.Get("x-signature")
		}
		if sig == "" {
			sig = "unsafe"
		}
		r.URL.Path = fmt.Sprintf("/%s%s", sig, strings.TrimPrefix(r.URL.Path, "/serve"))
		q.Del("x-signature")
		r.URL.RawQuery = q.Encode()
		imagorService.ServeHTTP(w, r)
	})))
	app.Get("/files", kvService.ServeHTTP, verifyAccess)
	app.Get("/files/*", kvService.ServeHTTP, verifyAccess)
	app.Put("/files/*", kvService.ServeHTTP, verifyAccess)
	app.Delete("/files/*", kvService.ServeHTTP, verifyAccess)
	app.Get("/sign/*", signatureService.ServeHTTP, verifyAPIKey)

	g := errgroup.Group{}
	g.Go(func() error {
		addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
		listenerNetwork := fiber.NetworkTCP4
		if cfg.Host == "[::]" {
			listenerNetwork = fiber.NetworkTCP6
		}
		// NOTE: We cannot use prefork because LevelDB uses a single file lock
		listenConfig := fiber.ListenConfig{
			GracefulContext:       ctx,
			ListenerNetwork:       listenerNetwork,
			DisableStartupMessage: true,
			CertFile:              cfg.CertFile,
			CertKeyFile:           cfg.CertKeyFile,
			OnShutdownError: func(err error) {
				log.Error("error shutting down objects server", "error", err)
			},
			OnShutdownSuccess: func() {
				if err := imagorService.Shutdown(ctx); err != nil {
					log.Error("imagor service did not shutdown gracefully", "error", err)
				}

				log.Info("server shutdown successfully")
			},
		}

		log.Info("starting server", "address", addr, "environment", cfg.Environment)
		if err := app.Listen(addr, listenConfig); err != nil {
			return err
		}

		return nil
	})

	if err := g.Wait(); err != nil {
		log.Error("error starting application", "error", err)
		os.Exit(1)
	}

	<-ctx.Done()
	log.Info("exit 0")
}
