package mpower

import (
	"bytes"

	"github.com/filecoin-project/go-address"
	addr "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/cbor"
	"github.com/filecoin-project/go-state-types/exitcode"
	initact "github.com/filecoin-project/lotus/chain/consensus/actors/init"
	"github.com/filecoin-project/specs-actors/actors/builtin"
	power0 "github.com/filecoin-project/specs-actors/actors/builtin/power"
	"github.com/filecoin-project/specs-actors/actors/runtime"
	"github.com/filecoin-project/specs-actors/actors/util/adt"
	"github.com/ipfs/go-cid"
)

type Runtime = runtime.Runtime

// MpowerActorAddr is initialized in genesis with the
// address t065
var MpowerActorAddr = func() addr.Address {
	a, err := address.NewIDAddress(65)
	if err != nil {
		panic(err)
	}
	return a
}()

type MpowerActor struct{}

func (a MpowerActor) Exports() []interface{} {
	return []interface{}{
		builtin.MethodConstructor: a.Constructor,
		2:                         a.CreateMiner,
	}
}

func (a MpowerActor) Code() cid.Cid {
	return builtin.StoragePowerActorCodeID
}

func (a MpowerActor) IsSingleton() bool {
	return true
}

func (a MpowerActor) State() cbor.Er {
	return new(State)
}

var _ runtime.VMActor = MpowerActor{}

type MinerConstructorParams struct {
	OwnerAddr           addr.Address
	WorkerAddr          addr.Address
	ControlAddrs        []addr.Address
	WindowPoStProofType abi.RegisteredPoStProof
	PeerId              abi.PeerID
	Multiaddrs          []abi.Multiaddrs
}

////////////////////////////////////////////////////////////////////////////////
// Actor methods
////////////////////////////////////////////////////////////////////////////////

func (a MpowerActor) Constructor(rt Runtime, _ *abi.EmptyValue) *abi.EmptyValue {
	rt.ValidateImmediateCallerIs(builtin.SystemActorAddr)

	st, err := ConstructState(adt.AsStore(rt))
	builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to construct state")
	rt.StateCreate(st)
	return nil
}

type CreateMinerParams struct {
	Owner               addr.Address
	Worker              addr.Address
	WindowPoStProofType abi.RegisteredPoStProof
	Peer                abi.PeerID
	Multiaddrs          []abi.Multiaddrs
}

//type CreateMinerReturn struct {
//	IDAddress     addr.Address // The canonical ID-based address for the actor.
//	RobustAddress addr.Address // A more expensive but re-org-safe address for the newly created actor.
//}
type CreateMinerReturn = power0.CreateMinerReturn

func (a MpowerActor) CreateMiner(rt Runtime, params *CreateMinerParams) *CreateMinerReturn {
	rt.ValidateImmediateCallerType(builtin.CallerTypesSignable...)

	ctorParams := MinerConstructorParams{
		OwnerAddr:           params.Owner,
		WorkerAddr:          params.Worker,
		WindowPoStProofType: params.WindowPoStProofType,
		PeerId:              params.Peer,
		Multiaddrs:          params.Multiaddrs,
	}
	ctorParamBuf := new(bytes.Buffer)
	err := ctorParams.MarshalCBOR(ctorParamBuf)
	builtin.RequireNoErr(rt, err, exitcode.ErrSerialization, "failed to serialize miner constructor params %v", ctorParams)

	var addresses initact.ExecReturn
	code := rt.Send(
		builtin.InitActorAddr,
		builtin.MethodsInit.Exec,
		&initact.ExecParams{
			CodeCID:           builtin.StorageMinerActorCodeID,
			ConstructorParams: ctorParamBuf.Bytes(),
		},
		rt.ValueReceived(), // Pass on any value to the new actor.
		&addresses,
	)
	builtin.RequireSuccess(rt, code, "failed to init new actor")

	var st State
	rt.StateTransaction(&st, func() {
		claims, err := adt.AsMap(adt.AsStore(rt), st.Claims)
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to load claims")

		st.MinerCount += 1

		st.Claims, err = claims.Root()
		builtin.RequireNoErr(rt, err, exitcode.ErrIllegalState, "failed to flush claims")
	})
	return &CreateMinerReturn{
		IDAddress:     addresses.IDAddress,
		RobustAddress: addresses.RobustAddress,
	}
}
