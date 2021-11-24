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

var lengthBufMinerConstructorParams = []byte{134}

func (t *MinerConstructorParams) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if _, err := w.Write(lengthBufMinerConstructorParams); err != nil {
		return err
	}

	scratch := make([]byte, 9)

	// t.OwnerAddr (address.Address) (struct)
	if err := t.OwnerAddr.MarshalCBOR(w); err != nil {
		return err
	}

	// t.WorkerAddr (address.Address) (struct)
	if err := t.WorkerAddr.MarshalCBOR(w); err != nil {
		return err
	}

	// t.ControlAddrs ([]address.Address) (slice)
	if len(t.ControlAddrs) > cbg.MaxLength {
		return xerrors.Errorf("Slice value in field t.ControlAddrs was too long")
	}

	if err := cbg.WriteMajorTypeHeaderBuf(scratch, w, cbg.MajArray, uint64(len(t.ControlAddrs))); err != nil {
		return err
	}
	for _, v := range t.ControlAddrs {
		if err := v.MarshalCBOR(w); err != nil {
			return err
		}
	}

	// t.WindowPoStProofType (abi.RegisteredPoStProof) (int64)
	if t.WindowPoStProofType >= 0 {
		if err := cbg.WriteMajorTypeHeaderBuf(scratch, w, cbg.MajUnsignedInt, uint64(t.WindowPoStProofType)); err != nil {
			return err
		}
	} else {
		if err := cbg.WriteMajorTypeHeaderBuf(scratch, w, cbg.MajNegativeInt, uint64(-t.WindowPoStProofType-1)); err != nil {
			return err
		}
	}

	// t.PeerId ([]uint8) (slice)
	if len(t.PeerId) > cbg.ByteArrayMaxLen {
		return xerrors.Errorf("Byte array in field t.PeerId was too long")
	}

	if err := cbg.WriteMajorTypeHeaderBuf(scratch, w, cbg.MajByteString, uint64(len(t.PeerId))); err != nil {
		return err
	}

	if _, err := w.Write(t.PeerId[:]); err != nil {
		return err
	}

	// t.Multiaddrs ([][]uint8) (slice)
	if len(t.Multiaddrs) > cbg.MaxLength {
		return xerrors.Errorf("Slice value in field t.Multiaddrs was too long")
	}

	if err := cbg.WriteMajorTypeHeaderBuf(scratch, w, cbg.MajArray, uint64(len(t.Multiaddrs))); err != nil {
		return err
	}
	for _, v := range t.Multiaddrs {
		if len(v) > cbg.ByteArrayMaxLen {
			return xerrors.Errorf("Byte array in field v was too long")
		}

		if err := cbg.WriteMajorTypeHeaderBuf(scratch, w, cbg.MajByteString, uint64(len(v))); err != nil {
			return err
		}

		if _, err := w.Write(v[:]); err != nil {
			return err
		}
	}
	return nil
}
