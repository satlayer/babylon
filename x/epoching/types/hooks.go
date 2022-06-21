package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// combine multiple Epoching hooks, all hook functions are run in array sequence
var _ EpochingHooks = &MultiEpochingHooks{}

type MultiEpochingHooks []EpochingHooks

func NewMultiEpochingHooks(hooks ...EpochingHooks) MultiEpochingHooks {
	return hooks
}

func (h MultiEpochingHooks) AfterEpochBegins(ctx sdk.Context, epoch sdk.Uint) error {
	for i := range h {
		if err := h[i].AfterEpochBegins(ctx, epoch); err != nil {
			return err
		}
	}

	return nil
}

func (h MultiEpochingHooks) AfterEpochEnds(ctx sdk.Context, epoch sdk.Uint) error {
	for i := range h {
		if err := h[i].AfterEpochEnds(ctx, epoch); err != nil {
			return err
		}
	}

	return nil
}