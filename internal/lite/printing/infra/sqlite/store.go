// Package sqlite is the printing module's tables.
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mizan-erp/mizan/internal/kernel/clock"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/lite/printing"
	"github.com/mizan-erp/mizan/internal/platform/database"
)

// Store implements printing.Store. Rows are only ever inserted (a test scans this file).
type Store struct{ db database.DB }

// NewStore builds the store.
func NewStore(db database.DB) *Store { return &Store{db: db} }

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Store) NextSeq(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.Reader(ctx).QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM print_jobs`).Scan(&n)
	return n, s.db.Dialect().TranslateError(err)
}

func (s *Store) Insert(ctx context.Context, j printing.Job) error {
	outcome := "failed"
	if j.Sent {
		outcome = "sent"
	}
	_, err := s.db.Writer(ctx).ExecContext(ctx, `INSERT INTO print_jobs (id, seq, document_kind, subject_id, copy_no, printer_name, path, outcome, error_code, printed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, j.ID.String(), j.Seq, string(j.Kind), nullable(j.SubjectID.String()), j.CopyNo, j.Printer, j.Path, outcome,
		nullable(j.ErrorCode), clock.Format(j.PrintedAt))
	return s.db.Dialect().TranslateError(err)
}

func (s *Store) SentCopies(ctx context.Context, subjectID id.ID) (int, error) {
	var n int
	err := s.db.Reader(ctx).QueryRowContext(ctx, `SELECT COUNT(*) FROM print_jobs WHERE subject_id = ? AND outcome = 'sent'`, subjectID.String()).Scan(&n)
	return n, s.db.Dialect().TranslateError(err)
}

func (s *Store) Jobs(ctx context.Context, limit int) ([]printing.Job, error) {
	rows, err := s.db.Reader(ctx).QueryContext(ctx, `SELECT id, seq, document_kind, COALESCE(subject_id, ''), copy_no, printer_name, path, outcome,
		COALESCE(error_code, ''), printed_at FROM print_jobs ORDER BY seq DESC LIMIT ?`, limit)
	if err != nil {
		return nil, s.db.Dialect().TranslateError(err)
	}
	defer func() { _ = rows.Close() }()
	var out []printing.Job
	for rows.Next() {
		var j printing.Job
		var rawID, subject, kind, outcome, at string
		if err = rows.Scan(&rawID, &j.Seq, &kind, &subject, &j.CopyNo, &j.Printer, &j.Path, &outcome, &j.ErrorCode, &at); err != nil {
			return nil, s.db.Dialect().TranslateError(err)
		}
		j.ID, j.SubjectID, j.Kind, j.Sent = id.ID(rawID), id.ID(subject), printing.Kind(kind), outcome == "sent"
		j.PrintedAt, _ = clock.ParseTimestamp(at)
		out = append(out, j)
	}
	return out, s.db.Dialect().TranslateError(rows.Err())
}

func (s *Store) AssignVoucher(ctx context.Context, entryID id.ID) (int64, error) {
	if n, found, err := s.Voucher(ctx, entryID); err != nil || found {
		return n, err
	}
	var next int64
	if err := s.db.Reader(ctx).QueryRowContext(ctx, `SELECT COALESCE(MAX(voucher_no), 0) + 1 FROM voucher_numbers`).Scan(&next); err != nil {
		return 0, s.db.Dialect().TranslateError(err)
	}
	_, err := s.db.Writer(ctx).ExecContext(ctx, `INSERT INTO voucher_numbers (entry_id, voucher_no) VALUES (?, ?)`, entryID.String(), next)
	return next, s.db.Dialect().TranslateError(err)
}

func (s *Store) Voucher(ctx context.Context, entryID id.ID) (int64, bool, error) {
	var n int64
	err := s.db.Reader(ctx).QueryRowContext(ctx, `SELECT voucher_no FROM voucher_numbers WHERE entry_id = ?`, entryID.String()).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return n, err == nil, s.db.Dialect().TranslateError(err)
}
