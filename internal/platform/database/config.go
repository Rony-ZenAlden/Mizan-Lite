package database

import "time"

// Config is the boot configuration for the database. It is passed in by the
// composition root (manual DI); this package reads no global state.
type Config struct {
	// Path is the SQLite database file path. Required. It should live under the OS
	// app-data directory (resolved by platform/fs), not inside the project tree.
	Path string
	// ReaderPoolSize is the max connections in the read pool. Default 4.
	ReaderPoolSize int
	// BusyTimeout is how long a connection waits on a locked database before erroring.
	// Default 5s. With the single-writer pool this turns write contention into a
	// bounded wait rather than a SQLITE_BUSY error.
	BusyTimeout time.Duration
}

func (c Config) withDefaults() Config {
	if c.ReaderPoolSize <= 0 {
		c.ReaderPoolSize = 4
	}
	if c.BusyTimeout <= 0 {
		c.BusyTimeout = 5 * time.Second
	}
	return c
}
