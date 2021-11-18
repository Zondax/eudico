package mpower

import (
	"fmt"
	"io"
	"reflect"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/specs-actors/v3/actors/builtin"
	"github.com/filecoin-project/specs-actors/v3/actors/util/adt"
	"github.com/ipfs/go-cid"
	cbg "github.com/whyrusleeping/cbor-gen"
	"golang.org/x/xerrors"
)

type State struct {
	MinerCount int64

	// Claimed power for each miner.
	Claims cid.Cid // Map, HAMT[address]Claim

}

func ConstructState(store adt.Store) (*State, error) {
	emptyClaimsMapCid, err := adt.StoreEmptyMap(store, builtin.DefaultHamtBitwidth)
	if err != nil {
		return nil, xerrors.Errorf("failed to create empty map: %w", err)
	}

	return &State{
		Claims:     emptyClaimsMapCid,
		MinerCount: 0,
	}, nil
}

func (t *State) UnmarshalCBOR(r io.Reader) error {
	*t = State{}

	br := cbg.GetPeeker(r)
	scratch := make([]byte, 8)

	maj, extra, err := cbg.CborReadHeaderBuf(br, scratch)
	if err != nil {
		return err
	}
	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 2 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.MinerCount (int64) (int64)
	{
		maj, extra, err := cbg.CborReadHeaderBuf(br, scratch)
		var extraI int64
		if err != nil {
			return err
		}
		switch maj {
		case cbg.MajUnsignedInt:
			extraI = int64(extra)
			if extraI < 0 {
				return fmt.Errorf("int64 positive overflow")
			}
		case cbg.MajNegativeInt:
			extraI = int64(extra)
			if extraI < 0 {
				return fmt.Errorf("int64 negative oveflow")
			}
			extraI = -1 - extraI
		default:
			return fmt.Errorf("wrong type for int64 field: %d", maj)
		}

		t.MinerCount = int64(extraI)
	}

	// t.Claims (cid.Cid) (struct)

	{
		c, err := cbg.ReadCid(br)
		if err != nil {
			return xerrors.Errorf("failed to read cid field t.Claims: %w", err)
		}

		t.Claims = c
	}

	return nil
}

func init() {
	// Check that ChainEpoch is indeed a signed integer to confirm that epochKey is making the right interpretation.
	var e abi.ChainEpoch
	if reflect.TypeOf(e).Kind() != reflect.Int64 {
		panic("incorrect chain epoch encoding")
	}
}
