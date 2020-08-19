package dbhelper

import (
	"fmt"
	"sync"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

type dbState uint8

const (
	closed dbState = iota
	opened
)

// DB a wrapper on an actual store
type DB struct {
	db      *leveldb.DB
	dbState dbState
	dbPath  string
	mutex   sync.RWMutex

	readOpts        *opt.ReadOptions
	writeOptsNoSync *opt.WriteOptions
	writeOptsSync   *opt.WriteOptions
}

// CreateDB constructs a DB
func CreateDB(dbPath string) *DB {
	readOpts := &opt.ReadOptions{}
	writeOptsNoSync := &opt.WriteOptions{}
	writeOptsSync := &opt.WriteOptions{}
	writeOptsSync.Sync = true

	return &DB{
		dbPath:          dbPath,
		dbState:         closed,
		readOpts:        readOpts,
		writeOptsNoSync: writeOptsNoSync,
		writeOptsSync:   writeOptsSync,
	}
}

// Open opens the underlying db
func (db *DB) Open() {
	db.mutex.Lock()
	defer db.mutex.Unlock()

	if db.dbState == opened {
		return
	}

	if db, err := leveldb.OpenFile(db.dbPath, nil); err != nil {
		panic(fmt.Sprintf("Error opening leveldb: %s", err))
	}

	db.db = db
	db.dbState = opened
}

func (db *DB) Close() {
	db.mutex.Lock()
	defer db.mutex.Unlock()

	if err := db.db.Close(); err != nil {
		// TODO logger
		fmt.Printf("Error closing leveldb: %s \n", err)
	}
}

// Get returns the value for the given key
func (db *DB) Get(key []byte) ([]byte, error) {
	db.mutex.RLock()
	defer db.mutex.RUnlock()

	value, err := db.db.Get(key, db.readOpts)
	if err == leveldb.ErrNotFound {
		value = nil
		err = nil
	}

	if err != nil {
		// TODO logger
		fmt.Printf("Error retrieving leveldb key [%#v]: %s", key, err)
		return nil, err
	}

	return value, nil
}

// Put saves the key/value
func (db *DB) Put(key []byte, value []byte, sync bool) error {
	db.mutex.RLock() // RLock?
	defer db.mutex.RUnlock()
	wo := db.writeOptsNoSync
	if sync {
		wo = db.writeOptsSync
	}

	err := db.db.Put(key, value, wo)
	if err != nil {
		// TODO logger
		fmt.Printf("Error writing leveldb key [%#v]", key)
		return err
	}
	return nil
}

// Delete deletes the given key
func (db *DB) Delete(key []byte, sync bool) error {
	db.mutex.RLock() // RLock?
	defer db.mutex.RUnlock()
	wo := db.writeOptsNoSync
	if sync {
		wo = db.writeOptsSync
	}
	err := db.db.Delete(key, wo)
	if err != nil {
		// TODO looger
		fmt.Printf("Error deleting leveldb key [%#v]", key)
		return err
	}
	return nil
}
