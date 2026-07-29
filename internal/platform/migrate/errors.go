package migrate

// Stable error codes. They double as i18n keys and are part of the public contract —
// never rename once shipped.
const (
	CodeLoadFailed         = "migrate.load_failed"
	CodeBadFileName        = "migrate.bad_file_name"
	CodeDuplicateVersion   = "migrate.duplicate_version"
	CodeForbiddenStatement = "migrate.forbidden_statement"

	// Safety / integrity
	CodeCorruptDatabase  = "migrate.corrupt_database"
	CodeInsufficientDisk = "migrate.insufficient_disk"
	CodeBackupFailed     = "migrate.backup_failed"
	CodeRestoreFailed    = "migrate.restore_failed"

	// History integrity
	CodeChecksumMismatch = "migrate.checksum_mismatch"
	CodeMissingHistory   = "migrate.missing_history"

	// Version gate
	CodeDatabaseTooNew = "migrate.database_too_new"

	// Concurrency
	CodeLockUnavailable = "migrate.lock_unavailable"

	// Apply
	CodeMigrationFailed = "migrate.migration_failed"
)
