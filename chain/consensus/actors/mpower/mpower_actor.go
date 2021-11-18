package mpower

import (
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/cbor"
	"github.com/filecoin-project/go-state-types/exitcode"
	"github.com/filecoin-project/specs-actors/actors/builtin"
	"github.com/filecoin-project/specs-actors/actors/runtime"
	"github.com/filecoin-project/specs-actors/actors/util/adt"
	"github.com/ipfs/go-cid"
)

type Runtime = runtime.Runtime

// MpowerActorAddr is initialized in genesis with the
// address t065
var MpowerActorAddr = func() address.Address {
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
