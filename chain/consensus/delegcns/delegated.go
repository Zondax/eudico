package delegcns

import (
	"context"
	"fmt"
	"strings"

	"github.com/Gurpartap/async"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/hashicorp/go-multierror"
	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	logging "github.com/ipfs/go-log/v2"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	cbg "github.com/whyrusleeping/cbor-gen"
	"go.opencensus.io/stats"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/network"
	bstore "github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/beacon"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/state"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/lib/sigs"
	"github.com/filecoin-project/lotus/metrics"
	blockadt "github.com/filecoin-project/specs-actors/actors/util/adt"
)

var _ consensus.Consensus = &Delegated{}

var log = logging.Logger("delegated-consensus")

type Delegated struct {
	// The interface for accessing and putting tipsets into local storage
	store *store.ChainStore

	// handle to the random beacon for verification
	beacon beacon.Schedule

	// the state manager handles making state queries
	sm *stmgr.StateManager

	verifier ffiwrapper.Verifier

	genesis *types.TipSet
}

var producer = func() address.Address {
	a, err := address.NewFromString("t0100")
	if err != nil {
		panic(err)
	}
	return a
}()

// Blocks that are more than MaxHeightDrift epochs above
// the theoretical max height based on systime are quickly rejected
const MaxHeightDrift = 5

func NewDelegatedConsensus(sm *stmgr.StateManager, beacon beacon.Schedule, verifier ffiwrapper.Verifier, genesis chain.Genesis) consensus.Consensus {
	return &Delegated{
		store:    sm.ChainStore(),
		beacon:   beacon,
		sm:       sm,
		verifier: verifier,
		genesis:  genesis,
	}
}

func (deleg *Delegated) ValidateBlock(ctx context.Context, b *types.FullBlock) (err error) {
	if err := blockSanityChecks(b.Header); err != nil {
		return xerrors.Errorf("incoming header failed basic sanity checks: %w", err)
	}

	h := b.Header

	baseTs, err := deleg.store.LoadTipSet(types.NewTipSetKey(h.Parents...))
	if err != nil {
		return xerrors.Errorf("load parent tipset failed (%s): %w", h.Parents, err)
	}

	// fast checks first
	if h.Height <= baseTs.Height() {
		return xerrors.Errorf("block height not greater than parent height: %d != %d", h.Height, baseTs.Height())
	}

	nulls := h.Height - (baseTs.Height() + 1)
	if tgtTs := baseTs.MinTimestamp() + build.BlockDelaySecs*uint64(nulls+1); h.Timestamp != tgtTs {
		return xerrors.Errorf("block has wrong timestamp: %d != %d", h.Timestamp, tgtTs)
	}

	now := uint64(build.Clock.Now().Unix())
	if h.Timestamp > now+build.AllowableClockDriftSecs {
		return xerrors.Errorf("block was from the future (now=%d, blk=%d): %w", now, h.Timestamp, consensus.ErrTemporal)
	}
	if h.Timestamp > now {
		log.Warn("Got block from the future, but within threshold", h.Timestamp, build.Clock.Now().Unix())
	}

	msgsCheck := async.Err(func() error {
		if b.Cid() == build.WhitelistedBlock {
			return nil
		}

		if err := deleg.checkBlockMessages(ctx, b, baseTs); err != nil {
			return xerrors.Errorf("block had invalid messages: %w", err)
		}
		return nil
	})

	minerCheck := async.Err(func() error {
		if err := deleg.minerIsValid(ctx, h.Miner, baseTs); err != nil {
			return xerrors.Errorf("minerIsValid failed: %w", err)
		}
		return nil
	})

	baseFeeCheck := async.Err(func() error {
		baseFee, err := deleg.store.ComputeBaseFee(ctx, baseTs)
		if err != nil {
			return xerrors.Errorf("computing base fee: %w", err)
		}
		if types.BigCmp(baseFee, b.Header.ParentBaseFee) != 0 {
			return xerrors.Errorf("base fee doesn't match: %s (header) != %s (computed)",
				b.Header.ParentBaseFee, baseFee)
		}
		return nil
	})
	pweight, err := Weight(context.TODO(), nil, baseTs)
	if err != nil {
		return xerrors.Errorf("getting parent weight: %w", err)
	}

	if types.BigCmp(pweight, b.Header.ParentWeight) != 0 {
		return xerrors.Errorf("parrent weight different: %s (header) != %s (computed)",
			b.Header.ParentWeight, pweight)
	}

	stateRootCheck := async.Err(func() error {
		stateroot, precp, err := deleg.sm.TipSetState(ctx, baseTs)
		if err != nil {
			return xerrors.Errorf("get tipsetstate(%d, %s) failed: %w", h.Height, h.Parents, err)
		}

		if stateroot != h.ParentStateRoot {
			msgs, err := deleg.store.MessagesForTipset(baseTs)
			if err != nil {
				log.Error("failed to load messages for tipset during tipset state mismatch error: ", err)
			} else {
				log.Warn("Messages for tipset with mismatching state:")
				for i, m := range msgs {
					mm := m.VMMessage()
					log.Warnf("Message[%d]: from=%s to=%s method=%d params=%x", i, mm.From, mm.To, mm.Method, mm.Params)
				}
			}

			return xerrors.Errorf("parent state root did not match computed state (%s != %s)", stateroot, h.ParentStateRoot)
		}

		if precp != h.ParentMessageReceipts {
			return xerrors.Errorf("parent receipts root did not match computed value (%s != %s)", precp, h.ParentMessageReceipts)
		}

		return nil
	})

	blockSigCheck := async.Err(func() error {
		if err := sigs.CheckBlockSignature(ctx, h, b.Header.Miner); err != nil {
			return xerrors.Errorf("check block signature failed: %w", err)
		}
		return nil
	})

	await := []async.ErrorFuture{
		minerCheck,
		blockSigCheck,
		msgsCheck,
		baseFeeCheck,
		stateRootCheck,
	}

	var merr error
	for _, fut := range await {
		if err := fut.AwaitContext(ctx); err != nil {
			merr = multierror.Append(merr, err)
		}
	}
	if merr != nil {
		mulErr := merr.(*multierror.Error)
		mulErr.ErrorFormat = func(es []error) string {
			if len(es) == 1 {
				return fmt.Sprintf("1 error occurred:\n\t* %+v\n\n", es[0])
			}

			points := make([]string, len(es))
			for i, err := range es {
				points[i] = fmt.Sprintf("* %+v", err)
			}

			return fmt.Sprintf(
				"%d errors occurred:\n\t%s\n\n",
				len(es), strings.Join(points, "\n\t"))
		}
		return mulErr
	}

	return nil
}

func blockSanityChecks(h *types.BlockHeader) error {
	if h.ElectionProof != nil {
		return xerrors.Errorf("block must have nil election proof")
	}

	if h.Ticket != nil {
		return xerrors.Errorf("block must have nil ticket")
	}

	if h.BlockSig == nil {
		return xerrors.Errorf("block had nil signature")
	}

	if h.BLSAggregate == nil {
		return xerrors.Errorf("block had nil bls aggregate signature")
	}

	if h.Miner.Protocol() != address.SECP256K1 {
		return xerrors.Errorf("block had non-secp miner address")
	}

	if len(h.Parents) != 1 {
		return xerrors.Errorf("must have 1 parent")
	}

	return nil
}

// TODO: We should extract this somewhere else and make the message pool and miner use the same logic
func (deleg *Delegated) checkBlockMessages(ctx context.Context, b *types.FullBlock, baseTs *types.TipSet) error {
	{
		var sigCids []cid.Cid // this is what we get for people not wanting the marshalcbor method on the cid type
		var pubks [][]byte

		for _, m := range b.BlsMessages {
			sigCids = append(sigCids, m.Cid())

			pubk, err := deleg.sm.GetBlsPublicKey(ctx, m.From, baseTs)
			if err != nil {
				return xerrors.Errorf("failed to load bls public to validate block: %w", err)
			}

			pubks = append(pubks, pubk)
		}

		if err := consensus.VerifyBlsAggregate(ctx, b.Header.BLSAggregate, sigCids, pubks); err != nil {
			return xerrors.Errorf("bls aggregate signature was invalid: %w", err)
		}
	}

	nonces := make(map[address.Address]uint64)

	stateroot, _, err := deleg.sm.TipSetState(ctx, baseTs)
	if err != nil {
		return err
	}

	st, err := state.LoadStateTree(deleg.store.ActorStore(ctx), stateroot)
	if err != nil {
		return xerrors.Errorf("failed to load base state tree: %w", err)
	}

	nv := deleg.sm.GetNtwkVersion(ctx, b.Header.Height)
	pl := vm.PricelistByEpoch(baseTs.Height())
	var sumGasLimit int64
	checkMsg := func(msg types.ChainMsg) error {
		m := msg.VMMessage()

		// Phase 1: syntactic validation, as defined in the spec
		minGas := pl.OnChainMessage(msg.ChainLength())
		if err := m.ValidForBlockInclusion(minGas.Total(), nv); err != nil {
			return err
		}

		// ValidForBlockInclusion checks if any single message does not exceed BlockGasLimit
		// So below is overflow safe
		sumGasLimit += m.GasLimit
		if sumGasLimit > build.BlockGasLimit {
			return xerrors.Errorf("block gas limit exceeded")
		}

		// Phase 2: (Partial) semantic validation:
		// the sender exists and is an account actor, and the nonces make sense
		var sender address.Address
		if deleg.sm.GetNtwkVersion(ctx, b.Header.Height) >= network.Version13 {
			sender, err = st.LookupID(m.From)
			if err != nil {
				return err
			}
		} else {
			sender = m.From
		}

		if _, ok := nonces[sender]; !ok {
			// `GetActor` does not validate that this is an account actor.
			act, err := st.GetActor(sender)
			if err != nil {
				return xerrors.Errorf("failed to get actor: %w", err)
			}

			if !builtin.IsAccountActor(act.Code) {
				return xerrors.New("Sender must be an account actor")
			}
			nonces[sender] = act.Nonce
		}

		if nonces[sender] != m.Nonce {
			return xerrors.Errorf("wrong nonce (exp: %d, got: %d)", nonces[sender], m.Nonce)
		}
		nonces[sender]++

		return nil
	}

	// Validate message arrays in a temporary blockstore.
	tmpbs := bstore.NewMemory()
	tmpstore := blockadt.WrapStore(ctx, cbor.NewCborStore(tmpbs))

	bmArr := blockadt.MakeEmptyArray(tmpstore)
	for i, m := range b.BlsMessages {
		if err := checkMsg(m); err != nil {
			return xerrors.Errorf("block had invalid bls message at index %d: %w", i, err)
		}

		c, err := store.PutMessage(tmpbs, m)
		if err != nil {
			return xerrors.Errorf("failed to store message %s: %w", m.Cid(), err)
		}

		k := cbg.CborCid(c)
		if err := bmArr.Set(uint64(i), &k); err != nil {
			return xerrors.Errorf("failed to put bls message at index %d: %w", i, err)
		}
	}

	smArr := blockadt.MakeEmptyArray(tmpstore)
	for i, m := range b.SecpkMessages {
		if err := checkMsg(m); err != nil {
			return xerrors.Errorf("block had invalid secpk message at index %d: %w", i, err)
		}

		// `From` being an account actor is only validated inside the `vm.ResolveToKeyAddr` call
		// in `StateManager.ResolveToKeyAddress` here (and not in `checkMsg`).
		kaddr, err := deleg.sm.ResolveToKeyAddress(ctx, m.Message.From, baseTs)
		if err != nil {
			return xerrors.Errorf("failed to resolve key addr: %w", err)
		}

		if err := sigs.Verify(&m.Signature, kaddr, m.Message.Cid().Bytes()); err != nil {
			return xerrors.Errorf("secpk message %s has invalid signature: %w", m.Cid(), err)
		}

		c, err := store.PutMessage(tmpbs, m)
		if err != nil {
			return xerrors.Errorf("failed to store message %s: %w", m.Cid(), err)
		}
		k := cbg.CborCid(c)
		if err := smArr.Set(uint64(i), &k); err != nil {
			return xerrors.Errorf("failed to put secpk message at index %d: %w", i, err)
		}
	}

	bmroot, err := bmArr.Root()
	if err != nil {
		return err
	}

	smroot, err := smArr.Root()
	if err != nil {
		return err
	}

	mrcid, err := tmpstore.Put(ctx, &types.MsgMeta{
		BlsMessages:   bmroot,
		SecpkMessages: smroot,
	})
	if err != nil {
		return err
	}

	if b.Header.Messages != mrcid {
		return fmt.Errorf("messages didnt match message root in header")
	}

	// Finally, flush.
	return vm.Copy(ctx, tmpbs, deleg.store.ChainBlockstore(), mrcid)
}

func (deleg *Delegated) IsEpochBeyondCurrMax(epoch abi.ChainEpoch) bool {
	if deleg.genesis == nil {
		return false
	}

	now := uint64(build.Clock.Now().Unix())
	return epoch > (abi.ChainEpoch((now-deleg.genesis.MinTimestamp())/build.BlockDelaySecs) + MaxHeightDrift)
}

func (deleg *Delegated) minerIsValid(ctx context.Context, maddr address.Address, baseTs *types.TipSet) error {
	ida, err := deleg.sm.LookupID(ctx, maddr, baseTs)
	if err != nil {
		return xerrors.Errorf("failed to load power actor: %w", err)
	}

	if ida != producer {
		return xerrors.Errorf("bad miner")
	}

	return nil
}

func Weight(ctx context.Context, stateBs bstore.Blockstore, ts *types.TipSet) (types.BigInt, error) {
	if ts == nil {
		return types.NewInt(0), nil
	}

	return big.NewInt(int64(ts.Height() + 1)), nil
}

func (deleg *Delegated) ValidateBlockPubsub(ctx context.Context, self bool, msg *pubsub.Message) (pubsub.ValidationResult, string) {
	if self {
		return deleg.validateLocalBlock(ctx, msg)
	}

	// track validation time
	begin := build.Clock.Now()
	defer func() {
		log.Debugf("block validation time: %s", build.Clock.Since(begin))
	}()

	stats.Record(ctx, metrics.BlockReceived.M(1))

	recordFailureFlagPeer := func(what string) {
		// bv.Validate will flag the peer in that case
		panic(what)
	}

	blk, what, err := deleg.decodeAndCheckBlock(msg)
	if err != nil {
		log.Error("got invalid block over pubsub: ", err)
		recordFailureFlagPeer(what)
		return pubsub.ValidationReject, what
	}

	// validate the block meta: the Message CID in the header must match the included messages
	err = deleg.validateMsgMeta(ctx, blk)
	if err != nil {
		log.Warnf("error validating message metadata: %s", err)
		recordFailureFlagPeer("invalid_block_meta")
		return pubsub.ValidationReject, "invalid_block_meta"
	}

	reject, err := deleg.validateBlockHeader(ctx, blk.Header)
	if err != nil {
		if reject == "" {
			log.Warn("ignoring block msg: ", err)
			return pubsub.ValidationIgnore, reject
		}
		recordFailureFlagPeer(reject)
		return pubsub.ValidationReject, reject
	}

	// all good, accept the block
	msg.ValidatorData = blk
	stats.Record(ctx, metrics.BlockValidationSuccess.M(1))
	return pubsub.ValidationAccept, ""
}

func (deleg *Delegated) validateLocalBlock(ctx context.Context, msg *pubsub.Message) (pubsub.ValidationResult, string) {
	stats.Record(ctx, metrics.BlockPublished.M(1))

	if size := msg.Size(); size > 1<<20-1<<15 {
		log.Errorf("ignoring oversize block (%dB)", size)
		return pubsub.ValidationIgnore, "oversize_block"
	}

	blk, what, err := deleg.decodeAndCheckBlock(msg)
	if err != nil {
		log.Errorf("got invalid local block: %s", err)
		return pubsub.ValidationIgnore, what
	}

	msg.ValidatorData = blk
	stats.Record(ctx, metrics.BlockValidationSuccess.M(1))
	return pubsub.ValidationAccept, ""
}

func (deleg *Delegated) decodeAndCheckBlock(msg *pubsub.Message) (*types.BlockMsg, string, error) {
	blk, err := types.DecodeBlockMsg(msg.GetData())
	if err != nil {
		return nil, "invalid", xerrors.Errorf("error decoding block: %w", err)
	}

	if count := len(blk.BlsMessages) + len(blk.SecpkMessages); count > build.BlockMessageLimit {
		return nil, "too_many_messages", fmt.Errorf("block contains too many messages (%d)", count)
	}

	// make sure we have a signature
	if blk.Header.BlockSig == nil {
		return nil, "missing_signature", fmt.Errorf("block without a signature")
	}

	return blk, "", nil
}

func (deleg *Delegated) validateMsgMeta(ctx context.Context, msg *types.BlockMsg) error {
	// TODO there has to be a simpler way to do this without the blockstore dance
	// block headers use adt0
	store := blockadt.WrapStore(ctx, cbor.NewCborStore(bstore.NewMemory()))
	bmArr := blockadt.MakeEmptyArray(store)
	smArr := blockadt.MakeEmptyArray(store)

	for i, m := range msg.BlsMessages {
		c := cbg.CborCid(m)
		if err := bmArr.Set(uint64(i), &c); err != nil {
			return err
		}
	}

	for i, m := range msg.SecpkMessages {
		c := cbg.CborCid(m)
		if err := smArr.Set(uint64(i), &c); err != nil {
			return err
		}
	}

	bmroot, err := bmArr.Root()
	if err != nil {
		return err
	}

	smroot, err := smArr.Root()
	if err != nil {
		return err
	}

	mrcid, err := store.Put(store.Context(), &types.MsgMeta{
		BlsMessages:   bmroot,
		SecpkMessages: smroot,
	})

	if err != nil {
		return err
	}

	if msg.Header.Messages != mrcid {
		return fmt.Errorf("messages didn't match root cid in header")
	}

	return nil
}

func (deleg *Delegated) validateBlockHeader(ctx context.Context, b *types.BlockHeader) (rejectReason string, err error) {
	baseTs := deleg.store.GetHeaviestTipSet()

	if err := deleg.minerIsValid(ctx, b.Miner, baseTs); err != nil {
		return err.Error(), err
	}

	err = sigs.CheckBlockSignature(ctx, b, b.Miner)
	if err != nil {
		log.Errorf("block signature verification failed: %s", err)
		return "signature_verification_failed", err
	}

	return "", nil
}
