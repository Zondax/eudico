// Code generated by github.com/whyrusleeping/cbor-gen. DO NOT EDIT.

package sca

import (
	"fmt"
	"io"
	"math"
	"sort"

	hierarchical "github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	cid "github.com/ipfs/go-cid"
	cbg "github.com/whyrusleeping/cbor-gen"
	xerrors "golang.org/x/xerrors"
)

var _ = xerrors.Errorf
var _ = cid.Undef
var _ = math.E
var _ = sort.Sort

var lengthBufSCAState = []byte{133}

func (t *SCAState) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if _, err := w.Write(lengthBufSCAState); err != nil {
		return err
	}

	scratch := make([]byte, 9)

	// t.Network (cid.Cid) (struct)

	if err := cbg.WriteCidBuf(scratch, w, t.Network); err != nil {
		return xerrors.Errorf("failed to write cid field t.Network: %w", err)
	}

	// t.NetworkName (hierarchical.SubnetID) (string)
	if len(t.NetworkName) > cbg.MaxLength {
		return xerrors.Errorf("Value in field t.NetworkName was too long")
	}

	if err := cbg.WriteMajorTypeHeaderBuf(scratch, w, cbg.MajTextString, uint64(len(t.NetworkName))); err != nil {
		return err
	}
	if _, err := io.WriteString(w, string(t.NetworkName)); err != nil {
		return err
	}

	// t.TotalSubnets (uint64) (uint64)

	if err := cbg.WriteMajorTypeHeaderBuf(scratch, w, cbg.MajUnsignedInt, uint64(t.TotalSubnets)); err != nil {
		return err
	}

	// t.MinStake (big.Int) (struct)
	if err := t.MinStake.MarshalCBOR(w); err != nil {
		return err
	}

	// t.Subnets (cid.Cid) (struct)

	if err := cbg.WriteCidBuf(scratch, w, t.Subnets); err != nil {
		return xerrors.Errorf("failed to write cid field t.Subnets: %w", err)
	}

	return nil
}

func (t *SCAState) UnmarshalCBOR(r io.Reader) error {
	*t = SCAState{}

	br := cbg.GetPeeker(r)
	scratch := make([]byte, 8)

	maj, extra, err := cbg.CborReadHeaderBuf(br, scratch)
	if err != nil {
		return err
	}
	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 5 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.Network (cid.Cid) (struct)

	{

		c, err := cbg.ReadCid(br)
		if err != nil {
			return xerrors.Errorf("failed to read cid field t.Network: %w", err)
		}

		t.Network = c

	}
	// t.NetworkName (hierarchical.SubnetID) (string)

	{
		sval, err := cbg.ReadStringBuf(br, scratch)
		if err != nil {
			return err
		}

		t.NetworkName = hierarchical.SubnetID(sval)
	}
	// t.TotalSubnets (uint64) (uint64)

	{

		maj, extra, err = cbg.CborReadHeaderBuf(br, scratch)
		if err != nil {
			return err
		}
		if maj != cbg.MajUnsignedInt {
			return fmt.Errorf("wrong type for uint64 field")
		}
		t.TotalSubnets = uint64(extra)

	}
	// t.MinStake (big.Int) (struct)

	{

		if err := t.MinStake.UnmarshalCBOR(br); err != nil {
			return xerrors.Errorf("unmarshaling t.MinStake: %w", err)
		}

	}
	// t.Subnets (cid.Cid) (struct)

	{

		c, err := cbg.ReadCid(br)
		if err != nil {
			return xerrors.Errorf("failed to read cid field t.Subnets: %w", err)
		}

		t.Subnets = c

	}
	return nil
}

var lengthBufSubnet = []byte{135}

func (t *Subnet) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if _, err := w.Write(lengthBufSubnet); err != nil {
		return err
	}

	scratch := make([]byte, 9)

	// t.Cid (cid.Cid) (struct)

	if err := cbg.WriteCidBuf(scratch, w, t.Cid); err != nil {
		return xerrors.Errorf("failed to write cid field t.Cid: %w", err)
	}

	// t.ID (hierarchical.SubnetID) (string)
	if len(t.ID) > cbg.MaxLength {
		return xerrors.Errorf("Value in field t.ID was too long")
	}

	if err := cbg.WriteMajorTypeHeaderBuf(scratch, w, cbg.MajTextString, uint64(len(t.ID))); err != nil {
		return err
	}
	if _, err := io.WriteString(w, string(t.ID)); err != nil {
		return err
	}

	// t.Parent (cid.Cid) (struct)

	if err := cbg.WriteCidBuf(scratch, w, t.Parent); err != nil {
		return xerrors.Errorf("failed to write cid field t.Parent: %w", err)
	}

	// t.ParentID (hierarchical.SubnetID) (string)
	if len(t.ParentID) > cbg.MaxLength {
		return xerrors.Errorf("Value in field t.ParentID was too long")
	}

	if err := cbg.WriteMajorTypeHeaderBuf(scratch, w, cbg.MajTextString, uint64(len(t.ParentID))); err != nil {
		return err
	}
	if _, err := io.WriteString(w, string(t.ParentID)); err != nil {
		return err
	}

	// t.Stake (big.Int) (struct)
	if err := t.Stake.MarshalCBOR(w); err != nil {
		return err
	}

	// t.Funds (cid.Cid) (struct)

	if err := cbg.WriteCidBuf(scratch, w, t.Funds); err != nil {
		return xerrors.Errorf("failed to write cid field t.Funds: %w", err)
	}

	// t.Status (sca.Status) (uint64)

	if err := cbg.WriteMajorTypeHeaderBuf(scratch, w, cbg.MajUnsignedInt, uint64(t.Status)); err != nil {
		return err
	}

	return nil
}

func (t *Subnet) UnmarshalCBOR(r io.Reader) error {
	*t = Subnet{}

	br := cbg.GetPeeker(r)
	scratch := make([]byte, 8)

	maj, extra, err := cbg.CborReadHeaderBuf(br, scratch)
	if err != nil {
		return err
	}
	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 7 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.Cid (cid.Cid) (struct)

	{

		c, err := cbg.ReadCid(br)
		if err != nil {
			return xerrors.Errorf("failed to read cid field t.Cid: %w", err)
		}

		t.Cid = c

	}
	// t.ID (hierarchical.SubnetID) (string)

	{
		sval, err := cbg.ReadStringBuf(br, scratch)
		if err != nil {
			return err
		}

		t.ID = hierarchical.SubnetID(sval)
	}
	// t.Parent (cid.Cid) (struct)

	{

		c, err := cbg.ReadCid(br)
		if err != nil {
			return xerrors.Errorf("failed to read cid field t.Parent: %w", err)
		}

		t.Parent = c

	}
	// t.ParentID (hierarchical.SubnetID) (string)

	{
		sval, err := cbg.ReadStringBuf(br, scratch)
		if err != nil {
			return err
		}

		t.ParentID = hierarchical.SubnetID(sval)
	}
	// t.Stake (big.Int) (struct)

	{

		if err := t.Stake.UnmarshalCBOR(br); err != nil {
			return xerrors.Errorf("unmarshaling t.Stake: %w", err)
		}

	}
	// t.Funds (cid.Cid) (struct)

	{

		c, err := cbg.ReadCid(br)
		if err != nil {
			return xerrors.Errorf("failed to read cid field t.Funds: %w", err)
		}

		t.Funds = c

	}
	// t.Status (sca.Status) (uint64)

	{

		maj, extra, err = cbg.CborReadHeaderBuf(br, scratch)
		if err != nil {
			return err
		}
		if maj != cbg.MajUnsignedInt {
			return fmt.Errorf("wrong type for uint64 field")
		}
		t.Status = Status(extra)

	}
	return nil
}

var lengthBufFundParams = []byte{129}

func (t *FundParams) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if _, err := w.Write(lengthBufFundParams); err != nil {
		return err
	}

	// t.Value (big.Int) (struct)
	if err := t.Value.MarshalCBOR(w); err != nil {
		return err
	}
	return nil
}

func (t *FundParams) UnmarshalCBOR(r io.Reader) error {
	*t = FundParams{}

	br := cbg.GetPeeker(r)
	scratch := make([]byte, 8)

	maj, extra, err := cbg.CborReadHeaderBuf(br, scratch)
	if err != nil {
		return err
	}
	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 1 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.Value (big.Int) (struct)

	{

		if err := t.Value.UnmarshalCBOR(br); err != nil {
			return xerrors.Errorf("unmarshaling t.Value: %w", err)
		}

	}
	return nil
}

var lengthBufAddSubnetReturn = []byte{129}

func (t *AddSubnetReturn) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if _, err := w.Write(lengthBufAddSubnetReturn); err != nil {
		return err
	}

	scratch := make([]byte, 9)

	// t.Cid (cid.Cid) (struct)

	if err := cbg.WriteCidBuf(scratch, w, t.Cid); err != nil {
		return xerrors.Errorf("failed to write cid field t.Cid: %w", err)
	}

	return nil
}

func (t *AddSubnetReturn) UnmarshalCBOR(r io.Reader) error {
	*t = AddSubnetReturn{}

	br := cbg.GetPeeker(r)
	scratch := make([]byte, 8)

	maj, extra, err := cbg.CborReadHeaderBuf(br, scratch)
	if err != nil {
		return err
	}
	if maj != cbg.MajArray {
		return fmt.Errorf("cbor input should be of type array")
	}

	if extra != 1 {
		return fmt.Errorf("cbor input had wrong number of fields")
	}

	// t.Cid (cid.Cid) (struct)

	{

		c, err := cbg.ReadCid(br)
		if err != nil {
			return xerrors.Errorf("failed to read cid field t.Cid: %w", err)
		}

		t.Cid = c

	}
	return nil
}
