package jobs

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"b2bcommerce/internal/money"
	"b2bcommerce/internal/rebates"
	"b2bcommerce/internal/store/gen"
)

// SettleRebateArgs settles one rebate program for its current period: it issues
// a credit note + records a settlement for every qualifying customer. Run as a
// background job because a program can span tens of thousands of customers — far
// too many to settle inside an HTTP request. Idempotent: customers already
// settled for the period are skipped, so a retry (or a re-run) is safe.
type SettleRebateArgs struct {
	ProgramID int64  `json:"program_id"`
	Ref       string `json:"ref,omitempty"` // RFC3339 reference time for the period window; empty = now
}

func (SettleRebateArgs) Kind() string { return "settle_rebate" }

// SettleResult reports what a settlement run did.
type SettleResult struct {
	PeriodKey   string
	Settled     int
	Skipped     int
	TotalRebate string
}

// SettleRebateProgram performs the settlement. Exposed directly so it runs from
// the worker and (synchronously) from tests alike. One transaction; bounded only
// by the program's qualifying-customer count, which is why it's off the request
// path.
func SettleRebateProgram(ctx context.Context, pool *pgxpool.Pool, programID int64, ref string) (SettleResult, error) {
	q := gen.New(pool)
	p, err := q.GetRebateProgramByID(ctx, programID)
	if err != nil {
		return SettleResult{}, err
	}
	refTime := time.Now()
	if ref != "" {
		if t, e := time.Parse(time.RFC3339, ref); e == nil {
			refTime = t
		}
	}
	tierRows, _ := q.ListRebateTiers(ctx, p.ID)
	tiers := make([]rebates.Tier, len(tierRows))
	for i, t := range tierRows {
		tiers[i] = rebates.Tier{MinAmount: t.MinAmount, RatePercent: t.RatePercent}
	}
	start, end, key := rebates.PeriodWindow(p.Period, refTime)

	rows, err := q.RebateQualifyingTotals(ctx, gen.RebateQualifyingTotalsParams{
		OrganizationID: p.OrganizationID, Currency: p.Currency, CreatedAt: start, CreatedAt_2: end, Customer: p.CustomerID,
	})
	if err != nil {
		return SettleResult{}, err
	}
	already := map[int64]bool{}
	if existing, e := q.ListRebateSettlementsForProgram(ctx, p.ID); e == nil {
		for _, s := range existing {
			if s.PeriodKey == key {
				already[s.CustomerID] = true
			}
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return SettleResult{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	tq := gen.New(tx)

	res := SettleResult{PeriodKey: key, TotalRebate: "0"}
	for _, row := range rows {
		if already[row.CustomerID] {
			res.Skipped++
			continue
		}
		rate, _, ok := rebates.Applicable(row.Total, tiers)
		if !ok {
			res.Skipped++
			continue
		}
		amt, e := rebates.Rebate(row.Total, rate)
		if e != nil {
			res.Skipped++
			continue
		}
		cid := row.CustomerID
		cn, e := tq.CreateCreditNote(ctx, gen.CreateCreditNoteParams{CustomerID: cid, Amount: amt, Currency: p.Currency})
		if e != nil {
			return SettleResult{}, e
		}
		cnID := cn.ID
		if _, e := tq.CreateRebateSettlement(ctx, gen.CreateRebateSettlementParams{
			ProgramID: p.ID, CustomerID: cid, PeriodKey: key, QualifyingTotal: row.Total,
			RatePercent: rate, RebateAmount: amt, Currency: p.Currency, Status: "issued", CreditNoteID: &cnID,
		}); e != nil {
			return SettleResult{}, e
		}
		res.TotalRebate, _ = money.Sum(res.TotalRebate, amt)
		res.Settled++
	}
	if err := tx.Commit(ctx); err != nil {
		return SettleResult{}, err
	}
	return res, nil
}

// RebateSettleWorker runs SettleRebateArgs jobs off the queue.
type RebateSettleWorker struct {
	river.WorkerDefaults[SettleRebateArgs]
	Pool *pgxpool.Pool
}

func (w *RebateSettleWorker) Work(ctx context.Context, job *river.Job[SettleRebateArgs]) error {
	_, err := SettleRebateProgram(ctx, w.Pool, job.Args.ProgramID, job.Args.Ref)
	return err
}
