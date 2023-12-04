package cli

import (
	"encoding/hex"
	"fmt"
	"math"
	"strings"

	sdkmath "cosmossdk.io/math"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/spf13/cobra"

	asig "github.com/babylonchain/babylon/crypto/schnorr-adaptor-signature"
	bbn "github.com/babylonchain/babylon/types"
	btcctypes "github.com/babylonchain/babylon/x/btccheckpoint/types"
	"github.com/babylonchain/babylon/x/btcstaking/types"
)

const (
	FlagMoniker         = "moniker"
	FlagIdentity        = "identity"
	FlagWebsite         = "website"
	FlagSecurityContact = "security-contact"
	FlagDetails         = "details"
	FlagCommissionRate  = "commission-rate"
)

// GetTxCmd returns the transaction commands for this module
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("%s transactions subcommands", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		NewCreateBTCValidatorCmd(),
		NewCreateBTCDelegationCmd(),
		NewAddCovenantSigCmd(),
		NewCreateBTCUndelegationCmd(),
		NewAddCovenantUnbondingSigsCmd(),
	)

	return cmd
}

func NewCreateBTCValidatorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-btc-validator [babylon_pk] [btc_pk] [pop]",
		Args:  cobra.ExactArgs(3),
		Short: "Create a BTC validator",
		Long: strings.TrimSpace(
			`Create a BTC validator.`, // TODO: example
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			fs := cmd.Flags()

			// get description
			moniker, _ := fs.GetString(FlagMoniker)
			identity, _ := fs.GetString(FlagIdentity)
			website, _ := fs.GetString(FlagWebsite)
			security, _ := fs.GetString(FlagSecurityContact)
			details, _ := fs.GetString(FlagDetails)
			description := stakingtypes.NewDescription(
				moniker,
				identity,
				website,
				security,
				details,
			)
			// get commission
			rateStr, _ := fs.GetString(FlagCommissionRate)
			rate, err := sdkmath.LegacyNewDecFromStr(rateStr)
			if err != nil {
				return err
			}

			// get Babylon PK
			babylonPKBytes, err := hex.DecodeString(args[0])
			if err != nil {
				return err
			}
			var babylonPK secp256k1.PubKey
			if err := babylonPK.Unmarshal(babylonPKBytes); err != nil {
				return err
			}

			// get BTC PK
			btcPK, err := bbn.NewBIP340PubKeyFromHex(args[1])
			if err != nil {
				return err
			}

			// get PoP
			pop, err := types.NewPoPFromHex(args[2])
			if err != nil {
				return err
			}

			msg := types.MsgCreateBTCValidator{
				Signer:      clientCtx.FromAddress.String(),
				Description: &description,
				Commission:  &rate,
				BabylonPk:   &babylonPK,
				BtcPk:       btcPK,
				Pop:         pop,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	}

	fs := cmd.Flags()
	fs.String(FlagMoniker, "", "The validator's (optional) moniker")
	fs.String(FlagWebsite, "", "The validator's (optional) website")
	fs.String(FlagSecurityContact, "", "The validator's (optional) security contact email")
	fs.String(FlagDetails, "", "The validator's (optional) details")
	fs.String(FlagIdentity, "", "The (optional) identity signature (ex. UPort or Keybase)")
	fs.String(FlagCommissionRate, "0", "The initial commission rate percentage")

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func parseLockTime(str string) (uint16, error) {
	num, ok := sdkmath.NewIntFromString(str)

	if !ok {
		return 0, fmt.Errorf("invalid staking time: %s", str)
	}

	if !num.IsUint64() {
		return 0, fmt.Errorf("staking time is not valid uint")
	}

	asUint64 := num.Uint64()

	if asUint64 > math.MaxUint16 {
		return 0, fmt.Errorf("staking time is too large. Max is %d", math.MaxUint16)
	}

	return uint16(asUint64), nil
}

func parseBtcAmount(str string) (btcutil.Amount, error) {
	num, ok := sdkmath.NewIntFromString(str)

	if !ok {
		return 0, fmt.Errorf("invalid staking value: %s", str)
	}

	if num.IsNegative() {
		return 0, fmt.Errorf("staking value is negative")
	}

	if !num.IsInt64() {
		return 0, fmt.Errorf("staking value is not valid uint")
	}

	asInt64 := num.Int64()

	return btcutil.Amount(asInt64), nil
}

func NewCreateBTCDelegationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-btc-delegation [babylon_pk] [btc_pk] [pop] [staking_tx_info] [val_pk] [staking_time] [staking_value] [slashing_tx] [delegator_sig]",
		Args:  cobra.ExactArgs(9),
		Short: "Create a BTC delegation",
		Long: strings.TrimSpace(
			`Create a BTC delegation.`, // TODO: example
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			// get Babylon PK
			babylonPKBytes, err := hex.DecodeString(args[0])
			if err != nil {
				return err
			}
			var babylonPK secp256k1.PubKey
			if err := babylonPK.Unmarshal(babylonPKBytes); err != nil {
				return err
			}

			// staker pk
			btcPK, err := bbn.NewBIP340PubKeyFromHex(args[1])

			if err != nil {
				return err
			}

			// get PoP
			pop, err := types.NewPoPFromHex(args[2])
			if err != nil {
				return err
			}

			// get staking tx info
			stakingTxInfo, err := btcctypes.NewTransactionInfoFromHex(args[3])
			if err != nil {
				return err
			}

			// TODO: Support multiple validators
			// get validator PK
			valPK, err := bbn.NewBIP340PubKeyFromHex(args[4])
			if err != nil {
				return err
			}

			// get staking time
			stakingTime, err := parseLockTime(args[5])

			if err != nil {
				return err
			}

			stakingValue, err := parseBtcAmount(args[6])

			if err != nil {
				return err
			}

			// get slashing tx
			slashingTx, err := types.NewBTCSlashingTxFromHex(args[7])
			if err != nil {
				return err
			}

			// get delegator sig
			delegatorSig, err := bbn.NewBIP340SignatureFromHex(args[8])
			if err != nil {
				return err
			}

			msg := types.MsgCreateBTCDelegation{
				Signer:       clientCtx.FromAddress.String(),
				BabylonPk:    &babylonPK,
				BtcPk:        btcPK,
				ValBtcPkList: []bbn.BIP340PubKey{*valPK},
				Pop:          pop,
				StakingTime:  uint32(stakingTime),
				StakingValue: int64(stakingValue),
				StakingTx:    stakingTxInfo,
				SlashingTx:   slashingTx,
				DelegatorSig: delegatorSig,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func NewAddCovenantSigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-covenant-sig [covenant_pk] [staking_tx_hash] [sig1] [sig2] ...",
		Args:  cobra.MinimumNArgs(3),
		Short: "Add a covenant signature",
		Long: strings.TrimSpace(
			`Add a covenant signature.`, // TODO: example
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			covPK, err := bbn.NewBIP340PubKeyFromHex(args[0])
			if err != nil {
				return fmt.Errorf("invalid public key: %w", err)
			}

			// get staking tx hash
			stakingTxHash := args[1]

			sigs := [][]byte{}
			for _, sigHex := range args[2:] {
				sig, err := asig.NewAdaptorSignatureFromHex(sigHex)
				if err != nil {
					return fmt.Errorf("invalid covenant signature: %w", err)
				}
				sigs = append(sigs, sig.MustMarshal())
			}

			msg := types.MsgAddCovenantSig{
				Signer:        clientCtx.FromAddress.String(),
				Pk:            covPK,
				StakingTxHash: stakingTxHash,
				Sigs:          sigs,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func NewCreateBTCUndelegationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-btc-undelegation [unbonding_tx] [slashing_tx] [unbonding_time] [unbonding_value] [delegator_sig]",
		Args:  cobra.ExactArgs(5),
		Short: "Create a BTC undelegation",
		Long: strings.TrimSpace(
			`Create a BTC undelegation.`, // TODO: example
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			// get staking tx
			_, unbondingTxBytes, err := bbn.NewBTCTxFromHex(args[0])
			if err != nil {
				return err
			}

			// get slashing tx
			slashingTx, err := types.NewBTCSlashingTxFromHex(args[1])
			if err != nil {
				return err
			}

			// get staking time
			unbondingTime, err := parseLockTime(args[2])

			if err != nil {
				return err
			}

			unbondingValue, err := parseBtcAmount(args[3])

			if err != nil {
				return err
			}

			// get delegator sig
			delegatorSig, err := bbn.NewBIP340SignatureFromHex(args[4])
			if err != nil {
				return err
			}

			msg := types.MsgBTCUndelegate{
				Signer:               clientCtx.FromAddress.String(),
				UnbondingTx:          unbondingTxBytes,
				UnbondingTime:        uint32(unbondingTime),
				UnbondingValue:       int64(unbondingValue),
				SlashingTx:           slashingTx,
				DelegatorSlashingSig: delegatorSig,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}

func NewAddCovenantUnbondingSigsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-covenant-unbonding-sigs [covenant_pk] [staking_tx_hash] [unbonding_tx_sg] [slashing_unbonding_tx_sig1] [slashing_unbonding_tx_sig2] ...",
		Args:  cobra.MinimumNArgs(4),
		Short: "Add covenant signatures for unbonding tx and slash unbonding tx",
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			// get covenant PK
			covPK, err := bbn.NewBIP340PubKeyFromHex(args[0])
			if err != nil {
				return err
			}

			// get staking tx hash
			stakingTxHash := args[1]

			// get covenant sigature for unbonding tx
			unbondingSig, err := bbn.NewBIP340SignatureFromHex(args[2])
			if err != nil {
				return err
			}

			slashingSigs := [][]byte{}
			for _, sigHex := range args[3:] {
				slashingSig, err := asig.NewAdaptorSignatureFromHex(sigHex)
				if err != nil {
					return fmt.Errorf("invalid covenant signature: %w", err)
				}
				slashingSigs = append(slashingSigs, slashingSig.MustMarshal())
			}

			msg := types.MsgAddCovenantUnbondingSigs{
				Signer:                  clientCtx.FromAddress.String(),
				Pk:                      covPK,
				StakingTxHash:           stakingTxHash,
				UnbondingTxSig:          unbondingSig,
				SlashingUnbondingTxSigs: slashingSigs,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
