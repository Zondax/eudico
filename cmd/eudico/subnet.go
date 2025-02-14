package main

import (
	"bytes"
	"fmt"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	big "github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/sca"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/urfave/cli/v2"
	cbg "github.com/whyrusleeping/cbor-gen"
	"golang.org/x/xerrors"
)

var subnetCmds = &cli.Command{
	Name:  "subnet",
	Usage: "Commands related with subneting",
	Subcommands: []*cli.Command{
		addCmd,
		joinCmd,
		listSubnetsCmd,
		mineCmd,
		leaveCmd,
		killCmd,
	},
}

var listSubnetsCmd = &cli.Command{
	Name:  "list-subnets",
	Usage: "list all subnets in the current network",
	Action: func(cctx *cli.Context) error {
		api, closer, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		ctx := lcli.ReqContext(cctx)

		ts, err := lcli.LoadTipSet(ctx, cctx, api)
		if err != nil {
			return err
		}

		var st sca.SCAState

		act, err := api.StateGetActor(ctx, sca.SubnetCoordActorAddr, ts.Key())
		if err != nil {
			return xerrors.Errorf("error getting actor state: %w", err)
		}
		bs := blockstore.NewAPIBlockstore(api)
		cst := cbor.NewCborStore(bs)
		s := adt.WrapStore(ctx, cst)
		if err := cst.Get(ctx, act.Head, &st); err != nil {
			return xerrors.Errorf("error getting subnet state: %w", err)
		}

		subnets, err := sca.ListSubnets(s, st)
		if err != nil {
			xerrors.Errorf("error getting list of subnets: %w", err)
		}
		for _, sh := range subnets {
			status := "Active"
			if sh.Status != 0 {
				status = "Inactive"
			}
			fmt.Printf("%s (%s): status=%v, stake=%v\n", sh.Cid, sh.ID, status, types.FIL(sh.Stake))
		}

		return nil
	},
}

var addCmd = &cli.Command{
	Name:      "add",
	Usage:     "Spawn a new subnet in network",
	ArgsUsage: "[stake amount]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "from",
			Usage: "optionally specify the account to send funds from",
		},
		&cli.StringFlag{
			Name:  "parent",
			Usage: "specify the ID of the parent subnet from which to add",
		},
		&cli.IntFlag{
			Name:  "consensus",
			Usage: "specify consensus for the subnet (0=delegated, 1=PoW)",
		},
		&cli.StringFlag{
			Name:  "name",
			Usage: "specify name for the subnet",
		},
		&cli.StringFlag{
			Name:  "delegminer",
			Usage: "optionally specify miner for delegated consensus",
		},
	},
	Action: func(cctx *cli.Context) error {

		api, closer, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		if cctx.Args().Len() != 0 {
			return lcli.ShowHelp(cctx, fmt.Errorf("'add' expects no arguments, just a set of flags"))
		}

		ctx := lcli.ReqContext(cctx)

		// Try to get default address first
		addr, _ := api.WalletDefaultAddress(ctx)
		if from := cctx.String("from"); from != "" {
			addr, err = address.NewFromString(from)
			if err != nil {
				return err
			}
		}

		consensus := 0
		if cctx.IsSet("consensus") {
			consensus = cctx.Int("consensus")
		}

		var name string
		if cctx.IsSet("name") {
			name = cctx.String("name")
		} else {
			return lcli.ShowHelp(cctx, fmt.Errorf("no name for subnet specified"))
		}

		parent := hierarchical.RootSubnet
		if cctx.IsSet("parent") {
			parent = hierarchical.SubnetID(cctx.String("parent"))
		}

		// FIXME: This is a horrible workaround to avoid delegminer from
		// not being set. But need to demo in 30 mins, so will fix it afterwards
		// (we all know I'll come across this comment in 2 years and laugh at it).
		delegminer := sca.SubnetCoordActorAddr
		if cctx.IsSet("delegminer") {
			d := cctx.String("delegminer")
			delegminer, err = address.NewFromString(d)
			if err != nil {
				return xerrors.Errorf("couldn't parse deleg miner address: %s", err)
			}
		} else if consensus == 0 {
			return lcli.ShowHelp(cctx, fmt.Errorf("no delegated miner for delegated consensus specified"))
		}
		minerStake := abi.NewStoragePower(1e8) // TODO: Make this value configurable in a flag/argument
		actorAddr, err := api.AddSubnet(ctx, addr, parent, name, uint64(consensus), minerStake, delegminer)
		if err != nil {
			return err
		}

		fmt.Printf("[*] subnet actor deployed as %v and new subnet availabe with ID=%v\n\n", actorAddr, hierarchical.NewSubnetID(parent, actorAddr))
		fmt.Printf("remember to join and register your subnet for it to be discoverable")
		return nil
	},
}

var joinCmd = &cli.Command{
	Name:      "join",
	Usage:     "Join or add additional stake to a subnet",
	ArgsUsage: "[<stake amount>]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "from",
			Usage: "optionally specify the account to send funds from",
		},
		&cli.StringFlag{
			Name:  "subnet",
			Usage: "specify the id of the subnet to join",
			Value: hierarchical.RootSubnet.String(),
		},
	},
	Action: func(cctx *cli.Context) error {

		if cctx.Args().Len() != 1 {
			return lcli.ShowHelp(cctx, fmt.Errorf("'join' expects the amount of stake as an argument, and a set of flags"))
		}
		api, closer, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		ctx := lcli.ReqContext(cctx)

		// Try to get default address first
		addr, _ := api.WalletDefaultAddress(ctx)
		if from := cctx.String("from"); from != "" {
			addr, err = address.NewFromString(from)
			if err != nil {
				return err
			}
		}

		// If subnet not set use root. Otherwise, use flag value
		var subnet string
		if cctx.String("subnet") != hierarchical.RootSubnet.String() {
			subnet = cctx.String("subnet")
		}

		val, err := types.ParseFIL(cctx.Args().Get(0))
		if err != nil {
			return lcli.ShowHelp(cctx, fmt.Errorf("failed to parse amount: %w", err))
		}

		c, err := api.JoinSubnet(ctx, addr, big.Int(val), hierarchical.SubnetID(subnet))
		if err != nil {
			return err
		}
		fmt.Fprintf(cctx.App.Writer, "Successfully added stake to subnet in message: %s\n", c)
		return nil
	},
}

var mineCmd = &cli.Command{
	Name:      "mine",
	Usage:     "Start mining in a subnet",
	ArgsUsage: "[]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "from",
			Usage: "optionally specify the account to mine from",
		},
		&cli.StringFlag{
			Name:  "subnet",
			Usage: "specify the id of the subnet to mine",
			Value: hierarchical.RootSubnet.String(),
		},
		&cli.BoolFlag{
			Name:  "stop",
			Usage: "use this flag to determine if you want to start or stop mining",
		},
	},
	Action: func(cctx *cli.Context) error {

		if cctx.Args().Len() != 0 {
			return lcli.ShowHelp(cctx, fmt.Errorf("'mine' expects no arguments, just a set of flags"))
		}
		api, closer, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		ctx := lcli.ReqContext(cctx)

		// Try to get default address first
		addr, _ := api.WalletDefaultAddress(ctx)
		if from := cctx.String("from"); from != "" {
			addr, err = address.NewFromString(from)
			if err != nil {
				return err
			}
		}

		// Get actor ID for wallet to use for mining.
		walletID, err := api.StateLookupID(ctx, addr, types.EmptyTSK)
		if err != nil {
			return err
		}
		// If subnet not set use root. Otherwise, use flag value
		var subnet string
		if cctx.String("subnet") != hierarchical.RootSubnet.String() {
			subnet = cctx.String("subnet")
		}

		err = api.MineSubnet(ctx, walletID, hierarchical.SubnetID(subnet), cctx.Bool("stop"))
		if err != nil {
			return err
		}
		fmt.Fprintf(cctx.App.Writer, "Successfully started/stopped mining in subnet: %s\n", subnet)
		return nil
	},
}

var leaveCmd = &cli.Command{
	Name:      "leave",
	Usage:     "Leave a subnet",
	ArgsUsage: "[]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "from",
			Usage: "optionally specify the account to send message from",
		},
		&cli.StringFlag{
			Name:  "subnet",
			Usage: "specify the id of the subnet to mine",
			Value: hierarchical.RootSubnet.String(),
		},
	},
	Action: func(cctx *cli.Context) error {

		if cctx.Args().Len() != 0 {
			return lcli.ShowHelp(cctx, fmt.Errorf("'leave' expects no arguments, just a set of flags"))
		}
		api, closer, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		ctx := lcli.ReqContext(cctx)

		// Try to get default address first
		addr, _ := api.WalletDefaultAddress(ctx)
		if from := cctx.String("from"); from != "" {
			addr, err = address.NewFromString(from)
			if err != nil {
				return err
			}
		}

		// If subnet not set use root. Otherwise, use flag value
		var subnet string
		if cctx.String("subnet") != hierarchical.RootSubnet.String() {
			subnet = cctx.String("subnet")
		}

		c, err := api.LeaveSubnet(ctx, addr, hierarchical.SubnetID(subnet))
		if err != nil {
			return err
		}
		fmt.Fprintf(cctx.App.Writer, "Successfully left subnet in message: %s\n", c)
		return nil
	},
}

var killCmd = &cli.Command{
	Name:      "kill",
	Usage:     "Send kill signal to a subnet",
	ArgsUsage: "[]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "from",
			Usage: "optionally specify the account to send message from",
		},
		&cli.StringFlag{
			Name:  "subnet",
			Usage: "specify the id of the subnet to mine",
			Value: hierarchical.RootSubnet.String(),
		},
	},
	Action: func(cctx *cli.Context) error {

		if cctx.Args().Len() != 0 {
			return lcli.ShowHelp(cctx, fmt.Errorf("'kill' expects no arguments, just a set of flags"))
		}
		api, closer, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		ctx := lcli.ReqContext(cctx)

		// Try to get default address first
		addr, _ := api.WalletDefaultAddress(ctx)
		if from := cctx.String("from"); from != "" {
			addr, err = address.NewFromString(from)
			if err != nil {
				return err
			}
		}

		// If subnet not set use root. Otherwise, use flag value
		var subnet string
		if cctx.String("subnet") != hierarchical.RootSubnet.String() {
			subnet = cctx.String("subnet")
		}

		c, err := api.KillSubnet(ctx, addr, hierarchical.SubnetID(subnet))
		if err != nil {
			return err
		}
		fmt.Fprintf(cctx.App.Writer, "Successfully sent kill to subnet in message: %s\n", c)
		return nil
	},
}

func MustSerialize(i cbg.CBORMarshaler) []byte {
	buf := new(bytes.Buffer)
	if err := i.MarshalCBOR(buf); err != nil {
		panic(err)
	}
	return buf.Bytes()
}
