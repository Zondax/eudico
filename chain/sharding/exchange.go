package sharding

import (
	"context"
	"time"

	cborutil "github.com/filecoin-project/go-cbor-util"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/exchange"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/hello"
	"github.com/gammazero/keymutex"
	"github.com/libp2p/go-eventbus"
	"github.com/libp2p/go-libp2p-core/event"
	inet "github.com/libp2p/go-libp2p-core/network"
	peer "github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	"golang.org/x/xerrors"
)

const (
	// BlockSyncProtoPrefix for shard block syn protocol
	BlockSyncProtoPrefix = "/fil/sync/blk/0.0.1/"
	// HelloProtoPrefix for shard block hello protocol
	HelloProtoPrefix = "/fil/hello/1.0.0/"
	// epochDiff determines the differences in epochs between
	// exchanges wit the peer for which we update their view
	// the chain. Peers may restart and run new hello handshakes
	// with the shard already created.
	epochDiff = 200
)

// helloService wraps information for hello service in shards
type helloService struct {
	svc *hello.Service
	// FIXME: We should probably garbage collect this map
	// periodically to prevent from growing indefinitely
	// with peers that no longer part of the shard.
	peers map[peer.ID]abi.ChainEpoch
	lk    *keymutex.KeyMutex
}

// Creates a new hello service for the shard
func (sh *Shard) newHelloService() {
	pid := protocol.ID(HelloProtoPrefix + sh.netName)
	sh.hello = &helloService{
		// NOTE: We added a NewShardHelloService to leverage the standard code for send hello.
		// I don't think we added complexity there, but if not bring all the required code here
		svc:   hello.NewShardHelloService(sh.host, sh.ch, sh.syncer, sh.cons, sh.pmgr, pid),
		peers: make(map[peer.ID]abi.ChainEpoch),
		lk:    keymutex.New(0),
	}
	log.Infow("Setting up hello protocol/service for shard with protocolID", "protocolID", pid)
	sh.host.SetStreamHandler(protocol.ID(HelloProtoPrefix+sh.netName), sh.handleHelloStream)
}

func (sh *Shard) helloBack(p peer.ID, epoch abi.ChainEpoch) {
	sh.hello.lk.Lock(p.String())
	defer sh.hello.lk.Unlock(p.String())
	prev, ok := sh.hello.peers[p]

	// If we haven't interacted with the peer yet, or
	// our previous communication was epochDiff ago,
	// update our view of the chain sending hello message.
	if !ok || (epoch-prev) > epochDiff {
		// Save hello to peer epoch.
		sh.hello.peers[p] = epoch
		// Send hello back
		sh.sendHello(sh.ctx, sh.hello.svc, p)
	}
}

// exchangeServer listening to sync requests from other peers sycners.
// Required to allow others to get in sync with the shard chain.
func (sh *Shard) exchangeServer() {
	srv := exchange.NewServer(sh.ch)
	sh.host.SetStreamHandler(protocol.ID(BlockSyncProtoPrefix+sh.netName), srv.HandleStream)
	log.Infow("Listening to exchange server with protocolID", "protocolID", BlockSyncProtoPrefix+sh.netName)
}

// create a new exchange client for the shard chain.
func (sh *Shard) exchangeClient(ctx context.Context) exchange.Client {
	log.Infow("Set up exchange client for shard with protocolID", "protocolID", BlockSyncProtoPrefix+sh.netName)
	return exchange.NewShardClient(ctx, sh.host, sh.pmgr, BlockSyncProtoPrefix+sh.netName)
}

// RunHello service. This methos is an adaptation of the one used
// for the root time in module/services.
func (sh *Shard) runHello(ctx context.Context) error {
	h := sh.host
	// Create hello service and register handler for shard
	sh.newHelloService()

	// Subscribe to new identifications with other peers
	sub, err := h.EventBus().Subscribe(new(event.EvtPeerIdentificationCompleted), eventbus.BufSize(1024))
	if err != nil {
		return xerrors.Errorf("failed to subscribe to event bus: %w", err)
	}

	// If we receive a new identification event run a hello message
	// with them, they may belong to our same sahrd.
	go func() {
		for evt := range sub.Out() {
			pic := evt.(event.EvtPeerIdentificationCompleted)
			go sh.sendHello(ctx, sh.hello.svc, pic.Peer)
		}
	}()

	// If we are running the service for the first time, perform an
	// initial hello handshake for this shard to sync the chain.
	conns := h.Network().Conns()
	for _, conn := range conns {
		// We do this synchronously to get the head before starting
		// pubsub topic. We need to sync first.
		// TODO: Use a channel for when a new head is received and abort,
		// we may have lots of peers.
		go sh.sendHello(ctx, sh.hello.svc, conn.RemotePeer())
	}
	return nil
}

// Send hello message to a peer updating with the state of our shard chain.
// It directly uses the same message format than the root's chain hello protocol.
func (sh *Shard) sendHello(ctx context.Context, svc *hello.Service, p peer.ID) {
	h := sh.host
	pid := protocol.ID(HelloProtoPrefix + sh.netName)
	log.Debugw("Saying hello to peer", "peerID", p)
	if err := svc.SayHello(ctx, p); err != nil {
		protos, _ := h.Peerstore().GetProtocols(p)
		agent, _ := h.Peerstore().Get(p, "AgentVersion")
		if protosContains(protos, string(pid)) {
			log.Warnw("failed to say hello", "error", err, "peer", p, "supported", protos, "agent", agent)
		} else {
			log.Debugw("failed to say hello", "error", err, "peer", p, "supported", protos, "agent", agent)
		}
		return
	}
}
func protosContains(protos []string, search string) bool {
	for _, p := range protos {
		if p == search {
			return true
		}
	}
	return false
}

// Handle hello stream for shards. It is slightly different
// from the one used in the root chain.
// Instead of running a sync'ed hello RTT, we listen to hello messages
// and according to the other end's chainEpoch we determine if we need
// to update their view of the chain or not.
func (sh *Shard) handleHelloStream(s inet.Stream) {
	var hmsg hello.HelloMessage
	if err := cborutil.ReadCborRPC(s, &hmsg); err != nil {
		log.Errorw("failed to read hello message, disconnecting", "error", err)
		_ = s.Conn().Close()
		return
	}
	arrived := build.Clock.Now()

	log.Debugw("genesis from hello",
		"tipset", hmsg.HeaviestTipSet,
		"peer", s.Conn().RemotePeer(),
		"hash", hmsg.GenesisHash)

	// If we don't have the same genesis abort the exchange. We are not able to sync
	// We may not even be in the same shard and running the same chain.
	if hmsg.GenesisHash != sh.syncer.Genesis.Cids()[0] {
		log.Errorf("other peer has different genesis! (rcv: %s, mine: %s)", hmsg.GenesisHash, sh.syncer.Genesis.Cids()[0])
		_ = s.Conn().Close()
		return
	}

	// Measure latency
	go func() {
		defer s.Close() //nolint:errcheck

		sent := build.Clock.Now()
		msg := &hello.LatencyMessage{
			TArrival: arrived.UnixNano(),
			TSent:    sent.UnixNano(),
		}
		if err := cborutil.WriteCborRPC(s, msg); err != nil {
			log.Errorf("error while responding to latency: %v", err)
		}
	}()

	// If it's been a while since we interacted with the peer in the shard,
	// send a hello message to update their view of the chain.
	sh.helloBack(s.Conn().RemotePeer(), hmsg.HeaviestTipSetHeight)

	// FIXME: I don't think this check is necessary for shards, we proactively respond with
	// helloBack.
	protos, err := sh.host.Peerstore().GetProtocols(s.Conn().RemotePeer())
	if err != nil {
		log.Warnf("got error from peerstore.GetProtocols: %s", err)
	}
	if len(protos) == 0 {
		log.Warn("other peer hasnt completed libp2p identify, waiting a bit")
		// TODO: this better
		build.Clock.Sleep(time.Millisecond * 300)
	}

	if sh.pmgr.Mgr != nil {
		sh.pmgr.Mgr.AddFilecoinPeer(s.Conn().RemotePeer())
	}

	// Get tipset from the other end
	ts, err := sh.syncer.FetchTipSet(context.Background(), s.Conn().RemotePeer(), types.NewTipSetKey(hmsg.HeaviestTipSet...))
	if err != nil {
		log.Errorf("failed to fetch tipset from peer during hello: %+v", err)
		return
	}

	// If the tipset is over 0, start syncing.
	// NOTE: Check if it is worth syncing even when our chain is not in 0.
	// We may be far behind and is worth notifying our syncer.
	// What if we join a shard again for which we kept the state?
	if ts.TipSet().Height() > 0 {
		sh.host.ConnManager().TagPeer(s.Conn().RemotePeer(), "fcpeer", 10)

		// don't bother informing about genesis
		log.Infof("Got new tipset through Hello: %s from %s", ts.Cids(), s.Conn().RemotePeer())
		sh.syncer.InformNewHead(s.Conn().RemotePeer(), ts)
	}
}
