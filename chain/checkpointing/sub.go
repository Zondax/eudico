package checkpointing

import (
	"context"
	"fmt"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/events"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/impl"
	"github.com/filecoin-project/lotus/node/modules/helpers"
	logging "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p-core/host"
	peer "github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.uber.org/fx"
)

var log = logging.Logger("checkpointing")

type CheckpointingSub struct {
	host   host.Host
	pubsub *pubsub.PubSub
	// Topic for keygen
	topic *pubsub.Topic
	// Sub for keygen
	sub *pubsub.Subscription
	// This is the API for the fullNode in the root chain.
	api *impl.FullNodeAPI
	// Listener for events of the root chain.
	events *events.Events
	// Generated public key
	pubkey []byte
	// Are we about to generate a key
	genkey bool
}

func NewCheckpointSub(
	mctx helpers.MetricsCtx,
	lc fx.Lifecycle,
	host host.Host,
	pubsub *pubsub.PubSub,
	api impl.FullNodeAPI,
) (*CheckpointingSub, error) {

	ctx := helpers.LifecycleCtx(mctx, lc)
	// Starting shardSub to listen to events in the root chain.
	e, err := events.NewEvents(ctx, &api)
	if err != nil {
		return nil, err
	}
	return &CheckpointingSub{
		pubsub: pubsub,
		topic:  nil,
		sub:    nil,
		host:   host,
		api:    &api,
		events: e,
	}, nil
}

func (c *CheckpointingSub) listenCheckpointEvents(ctx context.Context) {

	checkFunc := func(ctx context.Context, ts *types.TipSet) (done bool, more bool, err error) {
		return false, true, nil
	}

	changeHandler := func(oldTs, newTs *types.TipSet, states events.StateChange, curH abi.ChainEpoch) (more bool, err error) {
		log.Infow("State change detected for power actor")

		fmt.Println("CHANGING!!!!!!!!!!!!!!!!!!!!!!!!!")

		err = c.topic.Publish(ctx, []byte("hi"))
		if err != nil {
			panic(err)
		}

		msg, err := c.sub.Next(ctx)
		if err != nil {
			panic(err)
		}

		fmt.Println("Message:", string(msg.Data))
		from, err := peer.IDFromBytes(msg.From)
		if err != nil {
			panic(err)
		}
		fmt.Println("From:", from)
		fmt.Println("Seqno:", msg.GetSeqno())
		fmt.Println("Peers list:", c.topic.ListPeers())

		msg, err = c.sub.Next(ctx)
		if err != nil {
			panic(err)
		}

		fmt.Println("Message:", string(msg.Data))
		fmt.Println("From:", msg.From)
		fmt.Println("Peers list:", c.topic.ListPeers())

		return true, nil
	}

	revertHandler := func(ctx context.Context, ts *types.TipSet) error {
		return nil
	}

	match := func(oldTs, newTs *types.TipSet) (bool, events.StateChange, error) {
		/*
				NOT WORKING WITHOUT THE MOCKED POWER ACTOR

			oldAct, err := c.api.StateGetActor(ctx, mpoweractor.MpowerActorAddr, oldTs.Key())
			if err != nil {
				return false, nil, err
			}
			newAct, err := c.api.StateGetActor(ctx, mpoweractor.MpowerActorAddr, newTs.Key())
			if err != nil {
				return false, nil, err
			}
		*/

		// This is not actually what we want. Just here to check.
		oldAct, err := c.api.ChainGetTipSet(ctx, oldTs.Key())
		if err != nil {
			return false, nil, err
		}
		newAct, err := c.api.ChainGetTipSet(ctx, newTs.Key())
		if err != nil {
			return false, nil, err
		}

		// ZONDAX TODO:
		// If Power Actors list has changed start DKG

		fmt.Println(oldAct)
		fmt.Println(newAct)

		// Only start when we have 3 peers
		if len(c.topic.ListPeers()) < 2 {
			return false, nil, nil
		}

		if c.genkey {
			return false, nil, nil
		}

		c.genkey = true

		return true, nil, nil
	}

	err := c.events.StateChanged(checkFunc, changeHandler, revertHandler, 5, 76587687658765876, match)
	if err != nil {
		return
	}
}

func (c *CheckpointingSub) Start(ctx context.Context) {
	c.listenCheckpointEvents(ctx)

	topic, err := c.pubsub.Join("keygen")
	if err != nil {
		panic(err)
	}
	c.topic = topic

	// and subscribe to it
	sub, err := topic.Subscribe()
	if err != nil {
		panic(err)
	}
	c.sub = sub
}

func BuildCheckpointingSub(mctx helpers.MetricsCtx, lc fx.Lifecycle, c *CheckpointingSub) {
	ctx := helpers.LifecycleCtx(mctx, lc)
	c.Start(ctx)

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			// Do we need to stop something here ?
			return nil
		},
	})

}
