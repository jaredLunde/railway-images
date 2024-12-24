package keyval

import (
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/syndtr/goleveldb/leveldb"
)

type Config struct {
	UploadPath       string
	LevelDBPath      string
	SoftDelete       bool
	SignSecret       string
	BasePath         string
	MaxSize          int
	AllowedMimeTypes []string
	Logger           *slog.Logger
	Debug            bool
}

func New(cfg Config) (*KeyVal, error) {
	rand.New(rand.NewSource(time.Now().UnixNano()))
	db, err := leveldb.OpenFile(cfg.LevelDBPath, nil)
	if err != nil {
		return nil, err
	}

	return &KeyVal{
		db:               db,
		lock:             map[string]struct{}{},
		softDelete:       cfg.SoftDelete,
		volume:           cfg.UploadPath,
		signSecret:       cfg.SignSecret,
		basePath:         cfg.BasePath,
		maxFileSize:      cfg.MaxSize,
		allowedMimeTypes: cfg.AllowedMimeTypes,
		log:              cfg.Logger,
		debug:            cfg.Debug,
	}, nil
}

type KeyVal struct {
	db               *leveldb.DB
	mlock            sync.Mutex
	lock             map[string]struct{}
	log              *slog.Logger
	signSecret       string
	volume           string
	basePath         string
	maxFileSize      int
	allowedMimeTypes []string
	softDelete       bool
	debug            bool
}

func (k *KeyVal) Close() error {
	return k.db.Close()
}

func (k *KeyVal) UnlockKey(key []byte) {
	k.mlock.Lock()
	delete(k.lock, string(key))
	k.mlock.Unlock()
}

func (k *KeyVal) LockKey(key []byte) bool {
	k.mlock.Lock()
	defer k.mlock.Unlock()
	if _, prs := k.lock[string(key)]; prs {
		return false
	}
	k.lock[string(key)] = struct{}{}
	return true
}

func (k *KeyVal) GetRecord(key []byte) Record {
	data, err := k.db.Get(key, nil)
	rec := Record{HARD, ""}
	if err != leveldb.ErrNotFound {
		rec = toRecord(data)
	}
	return rec
}

func (k *KeyVal) PutRecord(key []byte, rec Record) error {
	data, err := fromRecord(rec)
	if err != nil {
		return err
	}
	return k.db.Put(key, data, nil)
}
