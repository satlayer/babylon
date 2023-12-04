package btcstaking_test

import (
	"bytes"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/babylonchain/babylon/btcstaking"
	btctest "github.com/babylonchain/babylon/testutil/bitcoin"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/stretchr/testify/require"
)

type TestScenario struct {
	StakerKey            *btcec.PrivateKey
	ValidatorKeys        []*btcec.PrivateKey
	CovenantKeys         []*btcec.PrivateKey
	RequiredCovenantSigs uint32
	StakingAmount        btcutil.Amount
	StakingTime          uint16
}

func GenerateTestScenario(
	r *rand.Rand,
	t *testing.T,
	numValidatorKeys uint32,
	numCovenantKeys uint32,
	requiredCovenantSigs uint32,
	stakingAmount btcutil.Amount,
	stakingTime uint16,
) *TestScenario {
	stakerPrivKey, err := btcec.NewPrivateKey()
	require.NoError(t, err)

	validatorKeys := make([]*btcec.PrivateKey, numValidatorKeys)
	for i := uint32(0); i < numValidatorKeys; i++ {
		covenantPrivKey, err := btcec.NewPrivateKey()
		require.NoError(t, err)

		validatorKeys[i] = covenantPrivKey
	}

	covenantKeys := make([]*btcec.PrivateKey, numCovenantKeys)

	for i := uint32(0); i < numCovenantKeys; i++ {
		covenantPrivKey, err := btcec.NewPrivateKey()
		require.NoError(t, err)

		covenantKeys[i] = covenantPrivKey
	}

	return &TestScenario{
		StakerKey:            stakerPrivKey,
		ValidatorKeys:        validatorKeys,
		CovenantKeys:         covenantKeys,
		RequiredCovenantSigs: requiredCovenantSigs,
		StakingAmount:        stakingAmount,
		StakingTime:          stakingTime,
	}
}

func (t *TestScenario) CovenantPublicKeys() []*btcec.PublicKey {
	covenantPubKeys := make([]*btcec.PublicKey, len(t.CovenantKeys))

	for i, covenantKey := range t.CovenantKeys {
		covenantPubKeys[i] = covenantKey.PubKey()
	}

	return covenantPubKeys
}

func (t *TestScenario) ValidatorPublicKeys() []*btcec.PublicKey {
	validatorPubKeys := make([]*btcec.PublicKey, len(t.ValidatorKeys))

	for i, validatorKey := range t.ValidatorKeys {
		validatorPubKeys[i] = validatorKey.PubKey()
	}

	return validatorPubKeys
}

func TestSpendingTimeLockPath(t *testing.T) {
	r := rand.New(rand.NewSource(time.Now().Unix()))
	scenario := GenerateTestScenario(
		r,
		t,
		1,
		5,
		3,
		btcutil.Amount(2*10e8),
		5,
	)

	stakingInfo, err := btcstaking.BuildStakingInfo(
		scenario.StakerKey.PubKey(),
		scenario.ValidatorPublicKeys(),
		scenario.CovenantPublicKeys(),
		scenario.RequiredCovenantSigs,
		scenario.StakingTime,
		scenario.StakingAmount,
		&chaincfg.MainNetParams,
	)

	require.NoError(t, err)

	spendStakeTx := wire.NewMsgTx(2)
	spendStakeTx.AddTxIn(wire.NewTxIn(&wire.OutPoint{}, nil, nil))
	spendStakeTx.AddTxOut(
		&wire.TxOut{
			PkScript: []byte("doesn't matter"),
			// spend half of the staking amount
			Value: int64(scenario.StakingAmount.MulF64(0.5)),
		},
	)

	// to spend tx as staker, we need to set the sequence number to be >= stakingTimeBlocks
	spendStakeTx.TxIn[0].Sequence = uint32(scenario.StakingTime)

	si, err := stakingInfo.TimeLockPathSpendInfo()
	require.NoError(t, err)

	sig, err := btcstaking.SignTxWithOneScriptSpendInputFromTapLeaf(
		spendStakeTx,
		stakingInfo.StakingOutput,
		scenario.StakerKey,
		si.RevealedLeaf,
	)

	require.NoError(t, err)

	witness, err := si.CreateTimeLockPathWitness(sig)
	require.NoError(t, err)

	spendStakeTx.TxIn[0].Witness = witness

	prevOutputFetcher := stakingInfo.GetOutputFetcher()

	newEngine := func() (*txscript.Engine, error) {
		return txscript.NewEngine(
			stakingInfo.GetPkScript(),
			spendStakeTx, 0, txscript.StandardVerifyFlags, nil,
			txscript.NewTxSigHashes(spendStakeTx, prevOutputFetcher), stakingInfo.StakingOutput.Value,
			prevOutputFetcher,
		)
	}
	btctest.AssertEngineExecution(t, 0, true, newEngine)
}

type SignatureInfo struct {
	SignerPubKey *btcec.PublicKey
	Signature    *schnorr.Signature
}

func NewSignatureInfo(
	signerPubKey *btcec.PublicKey,
	signature *schnorr.Signature,
) *SignatureInfo {
	return &SignatureInfo{
		SignerPubKey: signerPubKey,
		Signature:    signature,
	}
}

// Helper function to sort all signatures in reverse lexicographical order of signing public keys
// this way signatures are ready to be used in multisig witness with corresponding public keys
func sortSignatureInfo(infos []*SignatureInfo) []*SignatureInfo {
	sortedInfos := make([]*SignatureInfo, len(infos))
	copy(sortedInfos, infos)
	sort.SliceStable(sortedInfos, func(i, j int) bool {
		keyIBytes := schnorr.SerializePubKey(sortedInfos[i].SignerPubKey)
		keyJBytes := schnorr.SerializePubKey(sortedInfos[j].SignerPubKey)
		return bytes.Compare(keyIBytes, keyJBytes) == 1
	})

	return sortedInfos
}

// generate list of signatures in valid order
func GenerateSignatures(
	t *testing.T,
	keys []*btcec.PrivateKey,
	tx *wire.MsgTx,
	stakingOutput *wire.TxOut,
	leaf txscript.TapLeaf,
) []*schnorr.Signature {

	var si []*SignatureInfo

	for _, key := range keys {
		pubKey := key.PubKey()
		sig, err := btcstaking.SignTxWithOneScriptSpendInputFromTapLeaf(
			tx,
			stakingOutput,
			key,
			leaf,
		)
		require.NoError(t, err)
		info := NewSignatureInfo(
			pubKey,
			sig,
		)
		si = append(si, info)
	}

	// sort signatures by public key
	sortedSigInfo := sortSignatureInfo(si)

	var sigs []*schnorr.Signature = make([]*schnorr.Signature, len(sortedSigInfo))

	for i, sigInfo := range sortedSigInfo {
		sig := sigInfo
		sigs[i] = sig.Signature
	}

	return sigs
}

func TestSpendingUnbondingPathCovenant35MultiSig(t *testing.T) {
	r := rand.New(rand.NewSource(time.Now().Unix()))

	// we are having here 3/5 covenant threshold sig
	scenario := GenerateTestScenario(
		r,
		t,
		1,
		5,
		3,
		btcutil.Amount(2*10e8),
		5,
	)

	stakingInfo, err := btcstaking.BuildStakingInfo(
		scenario.StakerKey.PubKey(),
		scenario.ValidatorPublicKeys(),
		scenario.CovenantPublicKeys(),
		scenario.RequiredCovenantSigs,
		scenario.StakingTime,
		scenario.StakingAmount,
		&chaincfg.MainNetParams,
	)

	require.NoError(t, err)

	spendStakeTx := wire.NewMsgTx(2)
	spendStakeTx.AddTxIn(wire.NewTxIn(&wire.OutPoint{}, nil, nil))
	spendStakeTx.AddTxOut(
		&wire.TxOut{
			PkScript: []byte("doesn't matter"),
			// spend half of the staking amount
			Value: int64(scenario.StakingAmount.MulF64(0.5)),
		},
	)

	si, err := stakingInfo.UnbondingPathSpendInfo()
	require.NoError(t, err)

	stakerSig, err := btcstaking.SignTxWithOneScriptSpendInputFromTapLeaf(
		spendStakeTx,
		stakingInfo.StakingOutput,
		scenario.StakerKey,
		si.RevealedLeaf,
	)

	require.NoError(t, err)

	// scenario where all keys are available
	covenantSigantures := GenerateSignatures(
		t,
		scenario.CovenantKeys,
		spendStakeTx,
		stakingInfo.StakingOutput,
		si.RevealedLeaf,
	)
	witness, err := si.CreateUnbondingPathWitness(covenantSigantures, stakerSig)
	require.NoError(t, err)
	spendStakeTx.TxIn[0].Witness = witness

	prevOutputFetcher := stakingInfo.GetOutputFetcher()

	newEngine := func() (*txscript.Engine, error) {
		return txscript.NewEngine(
			stakingInfo.GetPkScript(),
			spendStakeTx, 0, txscript.StandardVerifyFlags, nil,
			txscript.NewTxSigHashes(spendStakeTx, prevOutputFetcher), stakingInfo.StakingOutput.Value,
			prevOutputFetcher,
		)
	}
	btctest.AssertEngineExecution(t, 0, true, newEngine)

	numOfCovenantMembers := len(scenario.CovenantKeys)
	// with each loop iteration we remove one key from the list of signatures
	for i := 0; i < numOfCovenantMembers; i++ {
		numOfRemovedSignatures := i + 1

		covenantSigantures := GenerateSignatures(
			t,
			scenario.CovenantKeys,
			spendStakeTx,
			stakingInfo.StakingOutput,
			si.RevealedLeaf,
		)

		for j := 0; j <= i; j++ {
			// NOTE: Number provides signatures must match number of public keys in the script,
			// if we are missing some signatures those must be set to empty signature in witness
			covenantSigantures[j] = nil
		}

		witness, err := si.CreateUnbondingPathWitness(covenantSigantures, stakerSig)
		require.NoError(t, err)
		spendStakeTx.TxIn[0].Witness = witness

		if numOfCovenantMembers-numOfRemovedSignatures >= int(scenario.RequiredCovenantSigs) {
			// if we are above threshold execution should be successful
			btctest.AssertEngineExecution(t, 0, true, newEngine)
		} else {
			// we are below threshold execution should be unsuccessful
			btctest.AssertEngineExecution(t, 0, false, newEngine)
		}
	}
}

func TestSpendingUnbondingPathSingleKeyCovenant(t *testing.T) {
	r := rand.New(rand.NewSource(time.Now().Unix()))

	// generate single key covenant
	scenario := GenerateTestScenario(
		r,
		t,
		1,
		1,
		1,
		btcutil.Amount(2*10e8),
		5,
	)

	stakingInfo, err := btcstaking.BuildStakingInfo(
		scenario.StakerKey.PubKey(),
		scenario.ValidatorPublicKeys(),
		scenario.CovenantPublicKeys(),
		scenario.RequiredCovenantSigs,
		scenario.StakingTime,
		scenario.StakingAmount,
		&chaincfg.MainNetParams,
	)

	require.NoError(t, err)

	spendStakeTx := wire.NewMsgTx(2)
	spendStakeTx.AddTxIn(wire.NewTxIn(&wire.OutPoint{}, nil, nil))
	spendStakeTx.AddTxOut(
		&wire.TxOut{
			PkScript: []byte("doesn't matter"),
			// spend half of the staking amount
			Value: int64(scenario.StakingAmount.MulF64(0.5)),
		},
	)

	si, err := stakingInfo.UnbondingPathSpendInfo()
	require.NoError(t, err)

	stakerSig, err := btcstaking.SignTxWithOneScriptSpendInputFromTapLeaf(
		spendStakeTx,
		stakingInfo.StakingOutput,
		scenario.StakerKey,
		si.RevealedLeaf,
	)
	require.NoError(t, err)

	// scenario where all keys are available
	covenantSigantures := GenerateSignatures(
		t,
		scenario.CovenantKeys,
		spendStakeTx,
		stakingInfo.StakingOutput,
		si.RevealedLeaf,
	)
	witness, err := si.CreateUnbondingPathWitness(covenantSigantures, stakerSig)
	require.NoError(t, err)
	spendStakeTx.TxIn[0].Witness = witness

	prevOutputFetcher := stakingInfo.GetOutputFetcher()

	newEngine := func() (*txscript.Engine, error) {
		return txscript.NewEngine(
			stakingInfo.GetPkScript(),
			spendStakeTx, 0, txscript.StandardVerifyFlags, nil,
			txscript.NewTxSigHashes(spendStakeTx, prevOutputFetcher), stakingInfo.StakingOutput.Value,
			prevOutputFetcher,
		)
	}
	btctest.AssertEngineExecution(t, 0, true, newEngine)
}

func TestSpendingSlashingPathCovenant35MultiSig(t *testing.T) {
	r := rand.New(rand.NewSource(time.Now().Unix()))

	// we are having here 3/5 covenant threshold sig
	scenario := GenerateTestScenario(
		r,
		t,
		1,
		5,
		3,
		btcutil.Amount(2*10e8),
		5,
	)

	stakingInfo, err := btcstaking.BuildStakingInfo(
		scenario.StakerKey.PubKey(),
		scenario.ValidatorPublicKeys(),
		scenario.CovenantPublicKeys(),
		scenario.RequiredCovenantSigs,
		scenario.StakingTime,
		scenario.StakingAmount,
		&chaincfg.MainNetParams,
	)

	require.NoError(t, err)

	spendStakeTx := wire.NewMsgTx(2)
	spendStakeTx.AddTxIn(wire.NewTxIn(&wire.OutPoint{}, nil, nil))
	spendStakeTx.AddTxOut(
		&wire.TxOut{
			PkScript: []byte("doesn't matter"),
			// spend half of the staking amount
			Value: int64(scenario.StakingAmount.MulF64(0.5)),
		},
	)

	si, err := stakingInfo.SlashingPathSpendInfo()
	require.NoError(t, err)

	// generate staker signature, covenant signatures, and validator signature
	stakerSig, err := btcstaking.SignTxWithOneScriptSpendInputFromTapLeaf(
		spendStakeTx,
		stakingInfo.StakingOutput,
		scenario.StakerKey,
		si.RevealedLeaf,
	)
	require.NoError(t, err)
	covenantSigantures := GenerateSignatures(
		t,
		scenario.CovenantKeys,
		spendStakeTx,
		stakingInfo.StakingOutput,
		si.RevealedLeaf,
	)
	validatorSig, err := btcstaking.SignTxWithOneScriptSpendInputFromTapLeaf(
		spendStakeTx,
		stakingInfo.StakingOutput,
		scenario.ValidatorKeys[0],
		si.RevealedLeaf,
	)
	require.NoError(t, err)

	witness, err := si.CreateSlashingPathWitness(
		covenantSigantures,
		[]*schnorr.Signature{validatorSig},
		stakerSig,
	)
	require.NoError(t, err)
	spendStakeTx.TxIn[0].Witness = witness

	// now as we have validator signature execution should succeed
	prevOutputFetcher := stakingInfo.GetOutputFetcher()
	newEngine := func() (*txscript.Engine, error) {
		return txscript.NewEngine(
			stakingInfo.GetPkScript(),
			spendStakeTx, 0, txscript.StandardVerifyFlags, nil,
			txscript.NewTxSigHashes(spendStakeTx, prevOutputFetcher), stakingInfo.StakingOutput.Value,
			prevOutputFetcher,
		)
	}
	btctest.AssertEngineExecution(t, 0, true, newEngine)
}

func TestSpendingSlashingPathCovenant35MultiSigValidatorRestaking(t *testing.T) {
	r := rand.New(rand.NewSource(time.Now().Unix()))

	// we have 3 out of 5 covenant committee, and we are restaking to 2 validators
	scenario := GenerateTestScenario(
		r,
		t,
		2,
		5,
		3,
		btcutil.Amount(2*10e8),
		5,
	)

	stakingInfo, err := btcstaking.BuildStakingInfo(
		scenario.StakerKey.PubKey(),
		scenario.ValidatorPublicKeys(),
		scenario.CovenantPublicKeys(),
		scenario.RequiredCovenantSigs,
		scenario.StakingTime,
		scenario.StakingAmount,
		&chaincfg.MainNetParams,
	)

	require.NoError(t, err)

	spendStakeTx := wire.NewMsgTx(2)
	spendStakeTx.AddTxIn(wire.NewTxIn(&wire.OutPoint{}, nil, nil))
	spendStakeTx.AddTxOut(
		&wire.TxOut{
			PkScript: []byte("doesn't matter"),
			// spend half of the staking amount
			Value: int64(scenario.StakingAmount.MulF64(0.5)),
		},
	)

	si, err := stakingInfo.SlashingPathSpendInfo()
	require.NoError(t, err)

	// generate staker signature, covenant signatures, and validator signature
	stakerSig, err := btcstaking.SignTxWithOneScriptSpendInputFromTapLeaf(
		spendStakeTx,
		stakingInfo.StakingOutput,
		scenario.StakerKey,
		si.RevealedLeaf,
	)
	require.NoError(t, err)

	// only use 3 out of 5 covenant signatures
	covenantSigantures := GenerateSignatures(
		t,
		scenario.CovenantKeys,
		spendStakeTx,
		stakingInfo.StakingOutput,
		si.RevealedLeaf,
	)
	covenantSigantures[0] = nil
	covenantSigantures[1] = nil

	// only use one of the validator signatures
	// script should still be valid as we require only one validator signature
	// to be present
	validatorsSignatures := GenerateSignatures(
		t,
		scenario.ValidatorKeys,
		spendStakeTx,
		stakingInfo.StakingOutput,
		si.RevealedLeaf,
	)
	validatorsSignatures[0] = nil

	witness, err := si.CreateSlashingPathWitness(covenantSigantures, validatorsSignatures, stakerSig)
	require.NoError(t, err)
	spendStakeTx.TxIn[0].Witness = witness

	prevOutputFetcher := stakingInfo.GetOutputFetcher()
	newEngine := func() (*txscript.Engine, error) {
		return txscript.NewEngine(
			stakingInfo.GetPkScript(),
			spendStakeTx, 0, txscript.StandardVerifyFlags, nil,
			txscript.NewTxSigHashes(spendStakeTx, prevOutputFetcher), stakingInfo.StakingOutput.Value,
			prevOutputFetcher,
		)
	}
	btctest.AssertEngineExecution(t, 0, true, newEngine)
}
