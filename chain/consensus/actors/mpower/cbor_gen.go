package mpower

import (
	"io"

	cbg "github.com/whyrusleeping/cbor-gen"
	"golang.org/x/xerrors"
)

var lengthBufState = []byte{143}

func (t *State) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if _, err := w.Write(lengthBufState); err != nil {
		return err
	}

	scratch := make([]byte, 9)

	// t.MinerCount (int64) (int64)
	if t.MinerCount >= 0 {
		if err := cbg.WriteMajorTypeHeaderBuf(scratch, w, cbg.MajUnsignedInt, uint64(t.MinerCount)); err != nil {
			return err
		}
	} else {
		if err := cbg.WriteMajorTypeHeaderBuf(scratch, w, cbg.MajNegativeInt, uint64(-t.MinerCount-1)); err != nil {
			return err
		}
	}

	// t.Claims (cid.Cid) (struct)
	if err := cbg.WriteCidBuf(scratch, w, t.Claims); err != nil {
		return xerrors.Errorf("failed to write cid field t.Claims: %w", err)
	}

	return nil
}
