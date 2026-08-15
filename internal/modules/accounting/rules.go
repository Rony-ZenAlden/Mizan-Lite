package accounting

import (
	"context"
	"sort"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/kernel/id"
	"github.com/mizan-erp/mizan/internal/modules/accounting/contract"
	"github.com/mizan-erp/mizan/internal/modules/accounting/domain"
	"github.com/mizan-erp/mizan/internal/modules/accounting/infra/sqlite"
	auditc "github.com/mizan-erp/mizan/internal/modules/audit/contract"
	"github.com/mizan-erp/mizan/internal/platform/seeds"
)

// rulesDir is where posting-rule sets live, in both layers.
const rulesDir = "seeds/posting_rules"

// Stable codes for the posting engine.
const (
	CodeInvalidRules        = "accounting.invalid_rules"
	CodeShippedRulesInvalid = "accounting.shipped_rules_invalid"
	CodeUnknownRuleSet      = "accounting.unknown_rule_set"
	CodeNoRuleForEvent      = "accounting.no_rule_for_event"
	CodeBadSelector         = "accounting.bad_selector"
	CodeRulesExist          = "accounting.rules_exist"
)

// ActionRulesApplied is the audited action.
const (
	ActionRulesApplied = "accounting.rules.applied"
	EntityRules        = "accounting.rules"
)

// RuleSet is a named collection of posting rules.
type RuleSet struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Rules       []Rule `json:"rules"`
}

// Rule is what one domain event posts.
type Rule struct {
	Code        string     `json:"code"`
	Event       string     `json:"event"`
	Sequence    int        `json:"sequence"`
	Description string     `json:"description"`
	Lines       []RuleLine `json:"lines"`
}

// RuleLine is one side of one posting, described rather than computed.
type RuleLine struct {
	Side    string `json:"side"`
	Account string `json:"account"`
	Amount  string `json:"amount"`
	Memo    string `json:"memo"`
}

// validate checks a rule set at LOAD.
//
// The same reasoning as the chart of accounts: a rule naming an account role that does not
// exist, or an amount nobody publishes, fails when a customer posts their first invoice —
// months later, in the middle of a sale. Here it is a startup failure for a file we ship and a
// reported-and-skipped file for one an accountant wrote.
func (r RuleSet) validate(name string) error {
	invalid := func(field, value string) error {
		return errs.Validation(CodeInvalidRules, "the posting rules are not valid").
			WithParam("field", field).WithParam("value", value)
	}

	if r.Code == "" || r.Code != strings.ToLower(r.Code) {
		return invalid("code", r.Code)
	}
	if name != "" && name != r.Code {
		return errs.Validation(CodeInvalidRules,
			"the rule set's code does not match its filename").
			WithParam("file", name).WithParam("value", r.Code)
	}
	if len(r.Rules) == 0 {
		return invalid("rules", "empty")
	}

	seen := map[string]bool{}
	for _, rule := range r.Rules {
		if rule.Code == "" {
			return invalid("rules[].code", "")
		}
		if seen[rule.Code] {
			return errs.Validation(CodeInvalidRules, "the rule set lists a rule twice").
				WithParam("code", rule.Code)
		}
		seen[rule.Code] = true

		if rule.Event == "" {
			return invalid("rules["+rule.Code+"].event", "")
		}
		if len(rule.Lines) < 2 {
			// One line cannot balance. A rule that produces a single-sided entry would be
			// refused by the aggregate at posting time — better to refuse the rule.
			return errs.Validation(CodeInvalidRules,
				"a posting rule needs at least two lines to balance").
				WithParam("code", rule.Code)
		}

		var debits, credits int
		for i, line := range rule.Lines {
			where := rule.Code + ".lines[" + itoa(i+1) + "]"
			switch domain.Side(line.Side) {
			case domain.Debit:
				debits++
			case domain.Credit:
				credits++
			default:
				return invalid(where+".side", line.Side)
			}
			if line.Account == documentAccountsSelector {
				continue
			}
			if _, _, err := parseAccountSelector(line.Account); err != nil {
				return invalid(where+".account", line.Account)
			}
			if line.Amount == "" {
				return invalid(where+".amount", "")
			}
		}
		// Both sides must be represented. A rule of three debits and no credit cannot produce a
		// balanced entry from any input at all, which makes it dead data that would only be
		// discovered by a failed sale.
		if debits == 0 || credits == 0 {
			return errs.Validation(CodeInvalidRules,
				"a posting rule must have at least one debit and one credit").
				WithParam("code", rule.Code)
		}
	}
	return nil
}

// parseAccountSelector splits `mapping:AR` or `account:1200`.
//
// Two forms and no third. `mapping:` is the one to use — it is the indirection that lets the
// same rule serve a shop whose receivables account is 1200 and one whose accountant numbered it
// 130. `account:` exists for the rare rule that genuinely means one specific account and would
// otherwise need a mapping key invented for a single use.
func parseAccountSelector(selector string) (kind, value string, err error) {
	parts := strings.SplitN(selector, ":", 2)
	if len(parts) != 2 || parts[1] == "" {
		return "", "", errs.Validation(CodeBadSelector,
			"an account selector reads `mapping:KEY` or `account:CODE`").
			WithParam("selector", selector)
	}
	switch parts[0] {
	case "mapping", "account":
		return parts[0], parts[1], nil
	}
	return "", "", errs.Validation(CodeBadSelector,
		"an account selector reads `mapping:KEY` or `account:CODE`").
		WithParam("selector", selector)
}

// loadRuleSets discovers and validates every rule set.
func loadRuleSets(layers []seeds.Layer) ([]RuleSet, []seeds.Problem, error) {
	set, err := seeds.Discover(rulesDir, layers...)
	if err != nil {
		return nil, nil, err
	}

	result, err := seeds.Decode(set, func(data []byte, r *RuleSet) error {
		return seeds.StrictJSON(data, r)
	})
	if err != nil {
		return nil, nil, err
	}

	out := make([]RuleSet, 0, len(result.Docs))
	problems := result.Problems
	for _, doc := range result.Docs {
		if validateErr := doc.Value.validate(doc.File.Name); validateErr != nil {
			if doc.File.Origin == seeds.OriginShipped {
				return nil, nil, errs.Wrap(validateErr, errs.CategoryInternal, CodeShippedRulesInvalid,
					"posting rules shipped with this build are invalid").
					WithParam("file", doc.File.Path)
			}
			problems = append(problems, seeds.Problem{
				Path: doc.File.Path, Origin: doc.File.Origin, Err: validateErr,
			})
			continue
		}
		out = append(out, doc.Value)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out, problems, nil
}

// RuleSets lists the rule sets on offer.
func (s *Service) RuleSets() []RuleSet {
	out := make([]RuleSet, len(s.ruleSets))
	copy(out, s.ruleSets)
	return out
}

// RuleSet returns one by code.
func (s *Service) RuleSet(code string) (RuleSet, bool) {
	for _, set := range s.ruleSets {
		if set.Code == code {
			return set, true
		}
	}
	return RuleSet{}, false
}

// ApplyRules writes a rule set's rows for a company, in one transaction.
//
// Rules live in the DATABASE once applied, not in the file. That is the point of §20.3: an
// accountant changes what a sale posts by editing a row, and the change takes effect with no
// release. The file is the starting point, not the running configuration.
func (s *Service) ApplyRules(ctx context.Context, companyID id.ID, code string) error {
	set, ok := s.RuleSet(code)
	if !ok {
		return errs.NotFound(CodeUnknownRuleSet, "no such posting rule set").WithParam("rules", code)
	}

	return s.db.Do(ctx, func(ctx context.Context) error {
		existing, err := s.repos.CountRules(ctx, companyID)
		if err != nil {
			return err
		}
		if existing > 0 {
			return errs.Conflict(CodeRulesExist, "this company already has posting rules")
		}

		for _, rule := range set.Rules {
			ruleID, idErr := id.New()
			if idErr != nil {
				return idErr
			}
			stored := sqlite.PostingRule{
				ID: ruleID, CompanyID: companyID, Event: rule.Event,
				Sequence: rule.Sequence, Code: rule.Code, Description: rule.Description,
			}
			for i, line := range rule.Lines {
				stored.Lines = append(stored.Lines, sqlite.PostingRuleLine{
					Number: i + 1, Side: line.Side,
					AccountSelector: line.Account, AmountSelector: line.Amount,
					Memo: line.Memo,
				})
			}
			if err = s.repos.InsertRule(ctx, stored); err != nil {
				return err
			}
		}

		return s.audit(ctx, auditc.Auditable{
			Action:      ActionRulesApplied,
			EntityType:  EntityRules,
			EntityID:    companyID,
			EntityLabel: set.Name,
			After:       rulesSnapshot{Code: set.Code, Rules: len(set.Rules)},
		})
	})
}

type rulesSnapshot struct {
	Code  string `json:"code"`
	Rules int    `json:"rules"`
}

// ── evaluation ──────────────────────────────────────────────────────────────────

// Record turns a Postable event into a journal entry, in the caller's transaction.
//
// # This is the whole point of the phase
//
// Sales publishes "an invoice was posted, and here are its amounts". This finds the rules for
// that event, resolves each line's account and amount from data, and posts one balanced entry.
// Sales contains no account codes, no debit/credit decisions, and no knowledge that accounting
// exists (§20.3).
//
// # Lines that resolve to zero are skipped
//
// Which is why `condition_expr` is reserved and unimplemented. "Post tax only when there is
// tax" falls out of the amount being zero, and so does "post cost only when stock is tracked" —
// a shop that does not track stock simply publishes no cost amount. An expression language
// would buy the same behaviour plus a parser, and anything powerful enough to need a parser is
// powerful enough to hide a bug in a customer's books.
//
// # An event with no rule is NOT an error
//
// A build may publish events an accountant has written no rule for, and refusing would stop the
// sale rather than the bookkeeping. It is reported to the caller as "nothing posted", and the
// caller decides — which for the subscriber below means logging it, not failing.
func (s *Service) Record(ctx context.Context, event contract.Postable) ([]domain.Entry, error) {
	rules, err := s.repos.RulesFor(ctx, event.CompanyID, event.Action)
	if err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		return nil, nil
	}

	var posted []domain.Entry
	for _, rule := range rules {
		lines, buildErr := s.linesFor(ctx, event, rule)
		if buildErr != nil {
			return nil, buildErr
		}
		if len(lines) == 0 {
			// Every amount this rule names was zero or absent. Normal: the cost rule of a shop
			// that does not track stock does exactly this on every sale.
			continue
		}

		entry, postErr := s.postWithin(ctx, PostInput{
			CompanyID:          event.CompanyID,
			Date:               event.Date,
			SourceModule:       moduleOf(event.Action),
			SourceDocumentType: event.DocumentType,
			SourceDocumentID:   event.DocumentID,
			BranchID:           event.BranchID,
			Memo:               memoFor(event, rule),
			Lines:              lines,
		})
		if postErr != nil {
			return nil, postErr
		}
		posted = append(posted, entry)
	}
	return posted, nil
}

// linesFor resolves one rule against one event.
func (s *Service) linesFor(
	ctx context.Context, event contract.Postable, rule sqlite.PostingRule,
) ([]domain.Line, error) {
	var lines []domain.Line

	for _, ruleLine := range rule.Lines {
		// The document-named form: one line per account the document carries. Used where the
		// debit side is open-ended (expense categories) and no rule could enumerate it.
		if ruleLine.AccountSelector == documentAccountsSelector {
			documentLines, err := s.linesFromDocument(event, ruleLine)
			if err != nil {
				return nil, err
			}
			lines = append(lines, documentLines...)
			continue
		}

		amount, present := event.Amounts[ruleLine.AmountSelector]
		if !present || amount == 0 {
			continue
		}
		if amount < 0 {
			// A negative amount would flip the side silently. A credit note is a DIFFERENT
			// event with its own rule, not a sale with a minus sign — which is also what keeps
			// the ledger's "a line is debit XOR credit, both non-negative" rule meaningful.
			return nil, errs.Validation(domain.CodeInvalidLine,
				"a posted amount cannot be negative; use the event for the reverse operation").
				WithParam("amount", ruleLine.AmountSelector)
		}

		account, err := s.resolveSelector(ctx, event, ruleLine.AccountSelector)
		if err != nil {
			return nil, err
		}

		line := domain.Line{
			AccountID:    account.ID,
			CurrencyCode: event.CurrencyCode,
			RateMicro:    event.RateMicro,
			BranchID:     event.BranchID,
			PartnerID:    event.PartnerID,
			Memo:         ruleLine.Memo,
		}
		if domain.Side(ruleLine.Side) == domain.Debit {
			line.Debit = amount
		} else {
			line.Credit = amount
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// documentAccountsSelector is the rule form that posts to accounts the DOCUMENT names.
//
// A third selector kind, added in Phase 7 for expenses. It is deliberately not a `mapping:` or an
// `account:` because it resolves to a SET rather than to one account — and giving it the same
// syntax would hide that difference.
const documentAccountsSelector = "document:accounts"

// linesFromDocument builds one line per account the document carries.
//
// The amounts come from `AccountAmounts`, which the posting module filled in. Each is checked for
// negativity like any other, because a negative here would flip a side just as silently.
func (s *Service) linesFromDocument(
	event contract.Postable, ruleLine sqlite.PostingRuleLine,
) ([]domain.Line, error) {
	if len(event.AccountAmounts) == 0 {
		return nil, nil
	}

	// Sorted, so the entry's lines are in a stable order. An entry whose lines shuffle between
	// runs is one a golden test cannot pin and a reader cannot compare.
	accounts := make([]id.ID, 0, len(event.AccountAmounts))
	for accountID := range event.AccountAmounts {
		accounts = append(accounts, accountID)
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i] < accounts[j] })

	out := make([]domain.Line, 0, len(accounts))
	for _, accountID := range accounts {
		amount := event.AccountAmounts[accountID]
		if amount == 0 {
			continue
		}
		if amount < 0 {
			return nil, errs.Validation(domain.CodeInvalidLine,
				"a posted amount cannot be negative; use the event for the reverse operation").
				WithParam("account", string(accountID))
		}

		line := domain.Line{
			AccountID:    accountID,
			CurrencyCode: event.CurrencyCode,
			RateMicro:    event.RateMicro,
			BranchID:     event.BranchID,
			PartnerID:    event.PartnerID,
			Memo:         ruleLine.Memo,
		}
		if domain.Side(ruleLine.Side) == domain.Debit {
			line.Debit = amount
		} else {
			line.Credit = amount
		}
		out = append(out, line)
	}
	return out, nil
}

// resolveSelector turns `mapping:AR` or `account:1200` into a real account.
func (s *Service) resolveSelector(
	ctx context.Context, event contract.Postable, selector string,
) (domain.Account, error) {
	kind, value, err := parseAccountSelector(selector)
	if err != nil {
		return domain.Account{}, err
	}
	if kind == "account" {
		return s.repos.AccountByCode(ctx, event.CompanyID, value)
	}
	return s.repos.ResolveMapping(ctx, event.CompanyID, event.BranchID, value)
}

// moduleOf reads the publishing module out of an action, so a journal entry records where it
// came from without the publisher having to say it twice.
func moduleOf(action string) string {
	if i := strings.Index(action, "."); i > 0 {
		return action[:i]
	}
	return "unknown"
}

func memoFor(event contract.Postable, rule sqlite.PostingRule) string {
	if event.Memo != "" {
		return event.Memo
	}
	if event.DocumentNumber != "" {
		return event.DocumentNumber
	}
	return rule.Code
}

// onPostable is the bus subscriber: it records what a module says happened.
//
// # Why a failure here fails the caller
//
// It returns the error, which aborts the publish and rolls back the sale that raised it (0.6
// §3.2). That is the same trade phase D7 made for the audit trail, and the argument is stronger
// here: a shop that cannot record what it sold has books that are wrong, and wrong books are
// harder to repair than a refused sale is to retry.
//
// # Why an event with no rule is not a failure
//
// A build publishes events an accountant may have written no rule for — and refusing would stop
// the sale rather than the bookkeeping. Record returns "nothing posted", and that is a
// legitimate outcome rather than an error to propagate.
func (s *Service) onPostable(ctx context.Context, event contract.Postable) error {
	_, err := s.Record(ctx, event)
	return err
}

// Rules lists a company's stored posting rules, for diagnostics and the settings screen.
func (s *Service) Rules(ctx context.Context, companyID id.ID) ([]sqlite.PostingRule, error) {
	return s.repos.Rules(ctx, companyID)
}
