package accounting

import (
	"sort"
	"strings"

	"github.com/mizan-erp/mizan/internal/kernel/errs"
	"github.com/mizan-erp/mizan/internal/modules/accounting/domain"
	"github.com/mizan-erp/mizan/internal/platform/seeds"
)

// chartDir is where chart-of-accounts templates live, in both layers.
const chartDir = "seeds/chart_of_accounts"

// Chart is one chart-of-accounts template.
//
// A file rather than a migration (§20.1): a chart is country- and trade-specific, and a default
// written into schema would be an accounting opinion nobody could replace without a release.
// It rides the layered loader Step 1.8 built — shipped templates plus whatever an accountant
// dropped into the data directory — which is this step's payoff for having built that loader
// generically rather than for country profiles alone.
type Chart struct {
	Code    string `json:"code"`
	NameKey string `json:"name_key"`
	Name    string `json:"name"`
	// Description says who this chart is FOR. An accountant choosing between templates needs
	// prose, not a code.
	Description string `json:"description"`

	Accounts []ChartAccount `json:"accounts"`
	// Mappings names which account plays each role the posting layer needs (§20.3), by CODE —
	// the stable key. Ids do not exist until the chart is applied.
	Mappings map[string]string `json:"mappings"`
}

// ChartAccount is one line of a template.
//
// Parent is a CODE, not an id, and the tree is rebuilt on load. A template that referenced ids
// could not be written by hand, which is the entire point of it being a file.
type ChartAccount struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	NameKey string `json:"name_key"`
	Parent  string `json:"parent"`
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	// System marks an account the mapping layer depends on: protected from deletion, freely
	// renameable.
	System bool `json:"system"`
}

// validate checks a template against the filename and its own internal consistency.
//
// # Why the mapping check is here
//
// A chart with no receivables account is not discovered when it is loaded — it is discovered
// when a customer tries to sell something, months later, and posting fails. Validating at load
// turns that into a startup failure for a template we ship and a reported-and-skipped file for
// one an accountant wrote.
func (c Chart) validate(name string) error {
	invalid := func(field, value string) error {
		return errs.Validation(CodeInvalidChart, "the chart of accounts is not valid").
			WithParam("field", field).WithParam("value", value)
	}

	if c.Code == "" || c.Code != strings.ToLower(c.Code) {
		return invalid("code", c.Code)
	}
	if name != "" && name != c.Code {
		return errs.Validation(CodeInvalidChart,
			"the chart's code does not match its filename").
			WithParam("file", name).WithParam("value", c.Code)
	}
	if c.Name == "" {
		return invalid("name", c.Name)
	}
	if len(c.Accounts) == 0 {
		return invalid("accounts", "empty")
	}

	byCode := make(map[string]ChartAccount, len(c.Accounts))
	for _, account := range c.Accounts {
		if account.Code == "" {
			return invalid("accounts[].code", "")
		}
		if _, duplicate := byCode[account.Code]; duplicate {
			return errs.Validation(CodeInvalidChart, "the chart lists an account twice").
				WithParam("code", account.Code)
		}
		if !domain.ValidType(domain.AccountType(account.Type)) {
			return invalid("accounts["+account.Code+"].type", account.Type)
		}
		byCode[account.Code] = account
	}

	for _, account := range c.Accounts {
		if account.Parent == "" {
			continue
		}
		parent, found := byCode[account.Parent]
		if !found {
			return errs.Validation(CodeInvalidChart, "an account names a parent the chart does not define").
				WithParam("code", account.Code).WithParam("parent", account.Parent)
		}
		// Caught here as well as in NewAccount, because a template is data somebody typed and
		// the message can name the file. The domain check is the one that cannot be bypassed.
		if parent.Type != account.Type {
			return errs.Validation(CodeInvalidChart,
				"an account has a different type from its parent").
				WithParam("code", account.Code).WithParam("parent", account.Parent)
		}
	}

	if err := c.checkCycles(byCode); err != nil {
		return err
	}

	for _, key := range domain.RequiredMappings() {
		code, mapped := c.Mappings[key]
		if !mapped {
			return errs.Validation(CodeInvalidChart,
				"the chart does not say which account plays a required role").
				WithParam("mapping", key)
		}
		account, found := byCode[code]
		if !found {
			return errs.Validation(CodeInvalidChart,
				"a mapping names an account the chart does not define").
				WithParam("mapping", key).WithParam("code", code)
		}
		// A mapping must resolve to something a journal line can actually use. Pointing AR at a
		// heading would produce a posting failure at the worst possible moment.
		if hasChildren(c.Accounts, account.Code) {
			return errs.Validation(CodeInvalidChart,
				"a mapping names a heading account, which cannot take postings").
				WithParam("mapping", key).WithParam("code", code)
		}
	}
	return nil
}

// checkCycles refuses a parent chain that loops.
//
// SQL catches only the one-step case (an account whose parent is itself). A three-account loop
// is invisible to the schema and would make path-building recurse forever.
func (c Chart) checkCycles(byCode map[string]ChartAccount) error {
	for _, account := range c.Accounts {
		seen := map[string]bool{account.Code: true}
		for current := account; current.Parent != ""; {
			if seen[current.Parent] {
				return errs.Validation(domain.CodeAccountCycle,
					"the chart's account hierarchy contains a loop").
					WithParam("code", account.Code)
			}
			seen[current.Parent] = true
			current = byCode[current.Parent]
		}
	}
	return nil
}

func hasChildren(accounts []ChartAccount, code string) bool {
	for _, account := range accounts {
		if account.Parent == code {
			return true
		}
	}
	return false
}

// ordered returns the accounts parents-first.
//
// Applying a chart has to insert a parent before its children — the path is built from the
// parent's — and a template is written in whatever order made sense to its author. Sorting by
// DEPTH rather than trusting file order means a hand-written chart cannot fail on the ordering
// of its own lines.
func (c Chart) ordered() []ChartAccount {
	byCode := make(map[string]ChartAccount, len(c.Accounts))
	for _, account := range c.Accounts {
		byCode[account.Code] = account
	}

	depth := make(map[string]int, len(c.Accounts))
	var measure func(ChartAccount) int
	measure = func(account ChartAccount) int {
		if d, done := depth[account.Code]; done {
			return d
		}
		d := 0
		if account.Parent != "" {
			d = measure(byCode[account.Parent]) + 1
		}
		depth[account.Code] = d
		return d
	}
	for _, account := range c.Accounts {
		measure(account)
	}

	out := append([]ChartAccount(nil), c.Accounts...)
	sort.SliceStable(out, func(i, j int) bool {
		if depth[out[i].Code] != depth[out[j].Code] {
			return depth[out[i].Code] < depth[out[j].Code]
		}
		// Code order within a level, so applying the same chart twice produces the same ids in
		// the same order — which makes a diff between two installations meaningful.
		return out[i].Code < out[j].Code
	})
	return out
}

// loadCharts discovers and validates every chart template.
//
// Shipped failures are fatal, a customer's are reported and skipped — the asymmetry lives in
// platform/seeds (1.8 D4) and is applied here unchanged.
func loadCharts(layers []seeds.Layer) ([]Chart, []seeds.Problem, error) {
	set, err := seeds.Discover(chartDir, layers...)
	if err != nil {
		return nil, nil, err
	}

	result, err := seeds.Decode(set, func(data []byte, c *Chart) error {
		return seeds.StrictJSON(data, c)
	})
	if err != nil {
		return nil, nil, err
	}

	charts := make([]Chart, 0, len(result.Docs))
	problems := result.Problems
	for _, doc := range result.Docs {
		if validateErr := doc.Value.validate(doc.File.Name); validateErr != nil {
			if doc.File.Origin == seeds.OriginShipped {
				return nil, nil, errs.Wrap(validateErr, errs.CategoryInternal, CodeShippedChartInvalid,
					"a chart of accounts shipped with this build is invalid").
					WithParam("file", doc.File.Path)
			}
			problems = append(problems, seeds.Problem{
				Path: doc.File.Path, Origin: doc.File.Origin, Err: validateErr,
			})
			continue
		}
		charts = append(charts, doc.Value)
	}

	sort.Slice(charts, func(i, j int) bool { return charts[i].Code < charts[j].Code })
	return charts, problems, nil
}
