package main

import (
	"os"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/checkpointing"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/delegcns"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical"
	"github.com/filecoin-project/lotus/chain/consensus/hierarchical/actors/subnet"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	lcli "github.com/filecoin-project/lotus/cli"
	cliutil "github.com/filecoin-project/lotus/cli/util"
	"github.com/filecoin-project/lotus/node"
)

var delegatedCmd = &cli.Command{
	Name:  "delegated",
	Usage: "Delegated consensus testbed",
	Subcommands: []*cli.Command{
		delegatedGenesisCmd,
		delegatedMinerCmd,

		daemonCmd(node.Options(
			node.Override(new(consensus.Consensus), delegcns.NewDelegatedConsensus),
			node.Override(new(store.WeightFunc), delegcns.Weight),
			node.Override(new(stmgr.Executor), delegcns.TipSetExecutor()),
			node.Override(new(stmgr.UpgradeSchedule), delegcns.DefaultUpgradeSchedule()),

			// Start checkpoint sub
			node.Override(new(*checkpointing.CheckpointingSub), checkpointing.NewCheckpointSub),
			node.Override(StartCheckpointingSubKey, checkpointing.BuildCheckpointingSub),
		)),
	},
}

var delegatedGenesisCmd = &cli.Command{
	Name:      "genesis",
	Usage:     "Generate genesis for delegated consensus",
	ArgsUsage: "[miner secpk addr] [outfile]",
	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() != 2 {
			return xerrors.Errorf("expected 2 arguments")
		}

		miner, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return xerrors.Errorf("parsing miner address: %w", err)
		}
		if miner.Protocol() != address.SECP256K1 {
			return xerrors.Errorf("must be secp address")
		}

		memks := wallet.NewMemKeyStore()
		w, err := wallet.NewWallet(memks)
		if err != nil {
			return err
		}
		vreg, err := w.WalletNew(cctx.Context, types.KTBLS)
		if err != nil {
			return err
		}
		rem, err := w.WalletNew(cctx.Context, types.KTBLS)
		if err != nil {
			return err
		}

		f, err := os.OpenFile(cctx.Args().Get(1), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return err
		}

		if err := subnet.WriteGenesis(hierarchical.RootSubnet, subnet.Delegated, miner, vreg, rem, uint64(time.Now().Unix()), f); err != nil {
			return xerrors.Errorf("write genesis car: %w", err)
		}

		log.Warnf("WRITING GENESIS FILE AT %s", f.Name())

		if err := f.Close(); err != nil {
			return err
		}

		return nil
	},
}

var delegatedMinerCmd = &cli.Command{
	Name:  "miner",
	Usage: "run delegated conesensus miner",
	Action: func(cctx *cli.Context) error {
		api, closer, err := lcli.GetFullNodeAPIV1(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := cliutil.ReqContext(cctx)
		return delegcns.Mine(ctx, api)
	},
}
