package sqlite

import (
	"context"

	"github.com/mizan-erp/mizan/internal/kernel/id"
)

// PostingRule is one stored rule with its lines.
type PostingRule struct {
	ID          id.ID
	CompanyID   id.ID
	Event       string
	Sequence    int
	Code        string
	Description string
	Lines       []PostingRuleLine
}

// PostingRuleLine is one described posting.
type PostingRuleLine struct {
	Number          int
	Side            string
	AccountSelector string
	AmountSelector  string
	Memo            string
}

// InsertRule writes a rule and its lines.
func (r *Repos) InsertRule(ctx context.Context, rule PostingRule) error {
	now := r.now()
	if _, err := r.db.Writer(ctx).ExecContext(ctx, `
		INSERT INTO posting_rules (
			id, company_id, event_type, business_profile_id, sequence, code, description,
			is_active, created_at, updated_at, row_version
		) VALUES (?, ?, ?, NULL, ?, ?, ?, 1, ?, ?, 1)`,
		string(rule.ID), string(rule.CompanyID), rule.Event, rule.Sequence,
		rule.Code, nullable(rule.Description), now, now); err != nil {
		return r.wrap(err, "inserting a posting rule")
	}

	for _, line := range rule.Lines {
		lineID, err := id.New()
		if err != nil {
			return err
		}
		if _, err = r.db.Writer(ctx).ExecContext(ctx, `
			INSERT INTO posting_rule_lines (
				id, posting_rule_id, line_number, side, account_selector, amount_selector,
				condition_expr, dimension_map, memo, created_at
			) VALUES (?, ?, ?, ?, ?, ?, NULL, NULL, ?, ?)`,
			string(lineID), string(rule.ID), line.Number, line.Side,
			line.AccountSelector, line.AmountSelector, nullable(line.Memo), now); err != nil {
			return r.wrap(err, "inserting a posting rule line")
		}
	}
	return nil
}

// CountRules reports how many posting rules a company has.
func (r *Repos) CountRules(ctx context.Context, companyID id.ID) (int, error) {
	var count int
	err := r.db.Reader(ctx).QueryRowContext(ctx,
		`SELECT COUNT(*) FROM posting_rules WHERE company_id = ?`, string(companyID)).Scan(&count)
	if err != nil {
		return 0, r.wrap(err, "counting posting rules")
	}
	return count, nil
}

// RulesFor finds the active rules for an event, in sequence.
//
// Matched EXACTLY on the event type, with no pattern language: "which rule posted this?" is the
// first question asked when the books look wrong, and a pattern makes the answer a search.
func (r *Repos) RulesFor(ctx context.Context, companyID id.ID, event string) ([]PostingRule, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT id, company_id, event_type, sequence, code, description
		  FROM posting_rules
		 WHERE company_id = ? AND event_type = ? AND is_active = 1
		 ORDER BY sequence, code`, string(companyID), event)
	if err != nil {
		return nil, r.wrap(err, "finding posting rules")
	}
	defer func() { _ = rows.Close() }()

	var out []PostingRule
	for rows.Next() {
		var (
			rule        PostingRule
			description any
		)
		if scanErr := rows.Scan(&rule.ID, &rule.CompanyID, &rule.Event,
			&rule.Sequence, &rule.Code, &description); scanErr != nil {
			return nil, r.wrap(scanErr, "scanning a posting rule")
		}
		rule.Description = text(description)
		out = append(out, rule)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	for i := range out {
		if out[i].Lines, err = r.ruleLines(ctx, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *Repos) ruleLines(ctx context.Context, ruleID id.ID) ([]PostingRuleLine, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT line_number, side, account_selector, amount_selector, memo
		  FROM posting_rule_lines WHERE posting_rule_id = ? ORDER BY line_number`,
		string(ruleID))
	if err != nil {
		return nil, r.wrap(err, "reading posting rule lines")
	}
	defer func() { _ = rows.Close() }()

	var out []PostingRuleLine
	for rows.Next() {
		var (
			line PostingRuleLine
			memo any
		)
		if scanErr := rows.Scan(&line.Number, &line.Side,
			&line.AccountSelector, &line.AmountSelector, &memo); scanErr != nil {
			return nil, r.wrap(scanErr, "scanning a posting rule line")
		}
		line.Memo = text(memo)
		out = append(out, line)
	}
	return out, rows.Err()
}

// Rules lists every rule a company has, for the diagnostics screen.
func (r *Repos) Rules(ctx context.Context, companyID id.ID) ([]PostingRule, error) {
	rows, err := r.db.Reader(ctx).QueryContext(ctx, `
		SELECT id, company_id, event_type, sequence, code, description
		  FROM posting_rules WHERE company_id = ? ORDER BY event_type, sequence, code`,
		string(companyID))
	if err != nil {
		return nil, r.wrap(err, "listing posting rules")
	}
	defer func() { _ = rows.Close() }()

	var out []PostingRule
	for rows.Next() {
		var (
			rule        PostingRule
			description any
		)
		if scanErr := rows.Scan(&rule.ID, &rule.CompanyID, &rule.Event,
			&rule.Sequence, &rule.Code, &description); scanErr != nil {
			return nil, r.wrap(scanErr, "scanning a posting rule")
		}
		rule.Description = text(description)
		out = append(out, rule)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Lines, err = r.ruleLines(ctx, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}
