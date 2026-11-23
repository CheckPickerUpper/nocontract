package balance

import (
	"context"
	"time"
)

// DelayedReadInput names the shopper, currency and the fixture's preparation delay.
type DelayedReadInput struct {
	ShopperID         int      `path:"shopper_id"`
	Currency          Currency `query:"currency"`
	DelayMilliseconds uint16   `query:"delay_milliseconds"`
}

// ReadDelayedBalance lets preparation finish after cancellation to exercise a late answer.
//
//nocontract:route GET /shoppers/{shopper_id}/delayed-balance
func (ledger Ledger) ReadDelayedBalance(requestContext context.Context, input DelayedReadInput) ReadOutcome {
	preparation := time.NewTimer(time.Duration(input.DelayMilliseconds) * time.Millisecond)
	defer preparation.Stop()
	select {
	case <-preparation.C:
	case <-requestContext.Done():
		<-preparation.C
	}
	return ledger.ReadBalance(requestContext, ReadInput{ShopperID: input.ShopperID, Currency: input.Currency})
}
