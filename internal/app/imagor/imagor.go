package imagor

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"hash"
	"os"
	"time"

	i "github.com/cshum/imagor"
	"github.com/cshum/imagor/imagorpath"
	"github.com/cshum/imagor/storage/filestorage"
	"github.com/cshum/imagor/vips"
	"github.com/jaredLunde/railway-images/internal/app/imagor/httploader"
	"github.com/jaredLunde/railway-images/internal/app/keyval"
)

type Config struct {
	KeyVal             *keyval.KeyVal
	UploadPath         string
	MaxUploadSize      int
	SignSecret         string
	AllowedHTTPSources string
	AutoWebP           bool
	AutoAVIF           bool
	ResultCacheTTL     time.Duration
	Concurrency        int
	RequestTimeout     time.Duration
	CacheControlTTL    time.Duration
	CacheControlSWR    time.Duration
	Debug              bool
}

func New(ctx context.Context, cfg Config) (*i.Imagor, error) {
	tmpDir, err := os.MkdirTemp("", "imagor-*")
	if err != nil {
		return nil, err
	}

	loaders := []i.Loader{
		NewKVStorage(cfg.KeyVal, cfg.UploadPath),
	}

	if cfg.AllowedHTTPSources != "" {
		loaders = append(loaders, httploader.New(
			httploader.WithForwardClientHeaders(false),
			httploader.WithAccept("image/*"),
			httploader.WithForwardHeaders(""),
			httploader.WithOverrideResponseHeaders(""),
			httploader.WithAllowedSources(cfg.AllowedHTTPSources),
			httploader.WithAllowedSourceRegexps(""),
			httploader.WithMaxAllowedSize(cfg.MaxUploadSize),
			httploader.WithInsecureSkipVerifyTransport(false),
			httploader.WithDefaultScheme("https"),
			httploader.WithBaseURL(""),
			httploader.WithProxyTransport("", ""),
			httploader.WithBlockLoopbackNetworks(false),
			httploader.WithBlockPrivateNetworks(false),
			httploader.WithBlockLinkLocalNetworks(false),
			httploader.WithBlockNetworks(),
			httploader.WithUserAgent("RailwayImagesClient/1.0 (Platform: Linux; Architecture: x64)"),
		))
	}

	imagorService := i.New(
		i.WithLoaders(loaders...),
		i.WithProcessors(vips.NewProcessor()),
		i.WithSigner(NewHMACSigner(sha256.New, 0, cfg.SignSecret)),
		i.WithBasePathRedirect(""),
		i.WithBaseParams(""),
		i.WithRequestTimeout(cfg.RequestTimeout),
		i.WithLoadTimeout(cfg.RequestTimeout),
		i.WithSaveTimeout(cfg.RequestTimeout),
		i.WithProcessTimeout(cfg.RequestTimeout),
		i.WithProcessConcurrency(int64(cfg.Concurrency)),
		i.WithProcessQueueSize(100),
		i.WithCacheHeaderTTL(cfg.CacheControlTTL),
		i.WithCacheHeaderSWR(cfg.CacheControlSWR),
		i.WithCacheHeaderNoCache(false),
		i.WithAutoWebP(cfg.AutoWebP),
		i.WithAutoAVIF(cfg.AutoAVIF),
		i.WithModifiedTimeCheck(false),
		i.WithDisableErrorBody(false),
		i.WithDisableParamsEndpoint(true),
		i.WithResultStorages(filestorage.New(tmpDir, filestorage.WithExpiration(cfg.ResultCacheTTL))),
		i.WithStoragePathStyle(imagorpath.DigestStorageHasher),
		i.WithResultStoragePathStyle(imagorpath.DigestResultStorageHasher),
		i.WithUnsafe(cfg.Debug),
		i.WithDebug(cfg.Debug),
	)

	appCtx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()

	if err := imagorService.Startup(appCtx); err != nil {
		return nil, err
	}

	return imagorService, nil
}

func NewHMACSigner(alg func() hash.Hash, truncate int, secret string) imagorpath.Signer {
	return &hmacSigner{
		alg:      alg,
		truncate: truncate,
		secret:   []byte(secret),
	}
}

type hmacSigner struct {
	alg      func() hash.Hash
	truncate int
	secret   []byte
}

func (s *hmacSigner) Sign(path string) string {
	h := hmac.New(s.alg, s.secret)
	h.Write([]byte(path))
	sig := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(h.Sum(nil))
	if s.truncate > 0 && len(sig) > s.truncate {
		return sig[:s.truncate]
	}
	return sig
}
