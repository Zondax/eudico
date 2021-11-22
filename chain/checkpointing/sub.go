package checkpointing

import (
	"context"
	"fmt"

	"github.com/Zondax/multi-party-sig/pkg/party"
	"github.com/Zondax/multi-party-sig/pkg/protocol"
	"github.com/Zondax/multi-party-sig/protocols/frost"
	"github.com/Zondax/multi-party-sig/protocols/frost/keygen"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/events"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/impl"
	"github.com/filecoin-project/lotus/node/modules/helpers"
	logging "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p-core/host"
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
	// taproot config
	config *keygen.TaprootConfig
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
		// ZONDAX TODO
		// Activate checkpointing every 5 blocks
		if ts.Height()%5 == 0 {
			fmt.Println("Check point time")

			if c.config != nil {
				fmt.Println("We have a taproot config")

				c.CreateCheckpoint(ctx)
			}

		}

		return false, true, nil
	}

	changeHandler := func(oldTs, newTs *types.TipSet, states events.StateChange, curH abi.ChainEpoch) (more bool, err error) {
		log.Infow("State change detected for power actor")

		fmt.Println("Peers list:", c.topic.ListPeers())

		_id := c.host.ID().String()
		var _idsStrings []string

		_idsStrings = append(_idsStrings, _id)

		for _, p := range c.topic.ListPeers() {
			_idsStrings = append(_idsStrings, p.String())
		}

		var _ids []party.ID
		for _, p := range _idsStrings {
			_ids = append(_ids, party.ID(p))
		}

		ids := party.NewIDSlice(_ids)
		id := party.ID(_id)

		threshold := 2
		n := NewNetwork(ids, c.sub, c.topic)
		f := frost.KeygenTaproot(id, ids, threshold)

		handler, err := protocol.NewMultiHandler(f, []byte{1, 2, 3})

		fmt.Println(handler)

		if err != nil {
			fmt.Println(err)
			log.Fatal("Not working")
		}
		c.LoopHandler(ctx, handler, n)
		r, err := handler.Result()
		if err != nil {
			fmt.Println(err)
			log.Fatal("Not working neither")
		}
		fmt.Println("Result :", r)

		c.config = r.(*keygen.TaprootConfig)
		c.pubkey = c.config.PublicKey

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

		if len(c.pubkey) > 1 {
			return false, nil, nil
		}

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

func (c *CheckpointingSub) LoopHandler(ctx context.Context, h protocol.Handler, network *Network) {
	for {
		msg, ok := <-h.Listen()
		if !ok {
			network.Done()
			// the channel was closed, indicating that the protocol is done executing.
			fmt.Println("Should be good")
			return
		}
		network.Send(ctx, msg)

		for _, _ = range network.Parties() {
			msg = network.Next(ctx)
			h.Accept(msg)
		}
	}
}

func (c *CheckpointingSub) CreateCheckpoint(ctx context.Context) {

}

func BuildCheckpointingSub(mctx helpers.MetricsCtx, lc fx.Lifecycle, c *CheckpointingSub) {
	ctx := helpers.LifecycleCtx(mctx, lc)

	// Ping to see if bitcoind is available
	payload := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"ping\", \"params\": []}"

	result := jsonRPC(payload)
	if result == nil {
		// Should probably not panic here
		panic("Bitcoin node not available")
	}

	fmt.Println("Successfully pinged bitcoind")

	c.Start(ctx)

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			// Do we need to stop something here ?
			return nil
		},
	})

}
