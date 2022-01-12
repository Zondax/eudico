package checkpointing

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/Zondax/multi-party-sig/pkg/math/curve"
	"github.com/Zondax/multi-party-sig/pkg/party"
	"github.com/Zondax/multi-party-sig/pkg/protocol"
	"github.com/Zondax/multi-party-sig/pkg/taproot"
	"github.com/Zondax/multi-party-sig/protocols/frost"
	"github.com/Zondax/multi-party-sig/protocols/frost/keygen"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/consensus/actors/mpower"
	"github.com/filecoin-project/lotus/chain/events"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/config"
	"github.com/filecoin-project/lotus/node/impl"
	"github.com/filecoin-project/lotus/node/modules/helpers"
	cbor "github.com/ipfs/go-ipld-cbor"
	logging "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.uber.org/fx"
	"golang.org/x/xerrors"
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
	// lock
	lk sync.Mutex

	// Generated public key
	pubkey []byte
	// taproot config
	config *keygen.TaprootConfig
	// miners
	minerSigners []peer.ID
	// new config generated
	newconfig *keygen.TaprootConfig
	// Previous tx
	ptxid string
	// Tweaked value
	tweakedValue []byte
	// minio config
	cpconfig *config.Checkpoint
	// minio client
	minioClient *minio.Client
	// Bitcoin latest checkpoint
	latestConfigCheckpoint types.TipSetKey
	// Is synced
	synced bool
	// height verified!
	height abi.ChainEpoch
}

func NewCheckpointSub(
	mctx helpers.MetricsCtx,
	lc fx.Lifecycle,
	host host.Host,
	pubsub *pubsub.PubSub,
	api impl.FullNodeAPI,
) (*CheckpointingSub, error) {

	ctx := helpers.LifecycleCtx(mctx, lc)
	// Starting checkpoint listener
	e, err := events.NewEvents(ctx, &api)
	if err != nil {
		return nil, err
	}

	fmt.Println("EUDICO PATH :", os.Getenv("EUDICO_PATH"))

	var ccfg config.FullNode
	result, err := config.FromFile(os.Getenv("EUDICO_PATH")+"/config.toml", &ccfg)
	if err != nil {
		return nil, err
	}

	cpconfig := result.(*config.FullNode).Checkpoint

	// initiate miners signers array
	var minerSigners []peer.ID

	synced := false
	var config *keygen.TaprootConfig
	// Load configTaproot
	_, err = os.Stat(os.Getenv("EUDICO_PATH") + "/share.toml")
	if err == nil {
		// If we have a share.toml containing the distributed key we load them
		synced = true
		content, err := os.ReadFile(os.Getenv("EUDICO_PATH") + "/share.toml")
		if err != nil {
			return nil, err
		}

		var configTOML TaprootConfigTOML
		_, err = toml.Decode(string(content), &configTOML)
		if err != nil {
			return nil, err
		}

		privateSharePath, err := hex.DecodeString(configTOML.PrivateShare)
		if err != nil {
			return nil, err
		}

		publickey, err := hex.DecodeString(configTOML.PublicKey)
		if err != nil {
			return nil, err
		}

		var privateShare curve.Secp256k1Scalar
		err = privateShare.UnmarshalBinary(privateSharePath)
		if err != nil {
			return nil, err
		}

		verificationShares := make(map[party.ID]*curve.Secp256k1Point)

		for key, vshare := range configTOML.VerificationShares {

			var p curve.Secp256k1Point
			pByte, err := hex.DecodeString(vshare.Share)
			if err != nil {
				return nil, err
			}
			err = p.UnmarshalBinary(pByte)
			if err != nil {
				return nil, err
			}
			verificationShares[party.ID(key)] = &p
		}

		config = &keygen.TaprootConfig{
			ID:                 party.ID(host.ID().String()),
			Threshold:          configTOML.Thershold,
			PrivateShare:       &privateShare,
			PublicKey:          publickey,
			VerificationShares: verificationShares,
		}

		for id := range config.VerificationShares {
			minerSigners = append(minerSigners, peer.ID(id))
		}
	}

	// Initialize minio client object.
	minioClient, err := minio.New(cpconfig.MinioHost, &minio.Options{
		Creds:  credentials.NewStaticV4(cpconfig.MinioAccessKeyID, cpconfig.MinioSecretAccessKey, ""),
		Secure: false,
	})
	if err != nil {
		return nil, err
	}

	return &CheckpointingSub{
		pubsub:       pubsub,
		topic:        nil,
		sub:          nil,
		host:         host,
		api:          &api,
		events:       e,
		ptxid:        "",
		config:       config,
		minerSigners: minerSigners,
		newconfig:    nil,
		cpconfig:     &cpconfig,
		minioClient:  minioClient,
		synced:       synced,
	}, nil
}

func (c *CheckpointingSub) listenCheckpointEvents(ctx context.Context) {

	checkFunc := func(ctx context.Context, ts *types.TipSet) (done bool, more bool, err error) {
		return false, true, nil
	}

	changeHandler := func(oldTs, newTs *types.TipSet, states events.StateChange, curH abi.ChainEpoch) (more bool, err error) {
		log.Infow("State change detected for power actor")

		return true, nil
	}

	revertHandler := func(ctx context.Context, ts *types.TipSet) error {
		return nil
	}

	match := func(oldTs, newTs *types.TipSet) (bool, events.StateChange, error) {
		c.lk.Lock()
		defer c.lk.Unlock()

		// verify we are synced
		st, err := c.api.SyncState(ctx)
		if err != nil {
			log.Errorf("unable to sync: %v", err)
			return false, nil, err
		}

		if !c.synced {
			// Are we synced ?
			if len(st.ActiveSyncs) > 0 &&
				st.ActiveSyncs[len(st.ActiveSyncs)-1].Height == newTs.Height() {

				fmt.Println("We are synced")
				// Yes then verify our checkpoint
				ts, err := c.api.ChainGetTipSet(ctx, c.latestConfigCheckpoint)
				if err != nil {
					log.Errorf("couldnt get tipset: %v", err)
					return false, nil, err

				}
				fmt.Println("We have a checkpoint up to height : ", ts.Height())
				c.synced = true
				c.height = ts.Height()
			} else {
				return false, nil, nil
			}
		}

		newAct, err := c.api.StateGetActor(ctx, mpower.PowerActorAddr, newTs.Key())
		if err != nil {
			return false, nil, err
		}

		oldAct, err := c.api.StateGetActor(ctx, mpower.PowerActorAddr, oldTs.Key())
		if err != nil {
			return false, nil, err
		}

		var oldSt, newSt mpower.State

		bs := blockstore.NewAPIBlockstore(c.api)
		cst := cbor.NewCborStore(bs)
		if err := cst.Get(ctx, oldAct.Head, &oldSt); err != nil {
			return false, nil, err
		}
		if err := cst.Get(ctx, newAct.Head, &newSt); err != nil {
			return false, nil, err
		}

		// Activate checkpointing every 30 blocks
		fmt.Println("Height:", newTs.Height())
		// NOTES: this will only work in delegated consensus
		// Wait for more tipset to valid the height and be sure it is valid
		if newTs.Height()%25 == 0 && (c.config != nil || c.newconfig != nil) {
			fmt.Println("Check point time")

			// Initiation and config should be happening at start
			cp := oldTs.Key().Bytes()

			// If we don't have a config we don't sign but update our config with key
			if c.config == nil {
				fmt.Println("We dont have a config")
				pubkey := c.newconfig.PublicKey

				pubkeyShort := genCheckpointPublicKeyTaproot(pubkey, cp)

				c.config = c.newconfig
				merkleRoot := hashMerkleRoot(pubkey, cp)
				c.tweakedValue = hashTweakedValue(pubkey, merkleRoot)
				c.pubkey = pubkeyShort
				c.newconfig = nil

			} else {
				var config string = hex.EncodeToString(cp) + "\n"
				for _, partyId := range c.orderParticipantsList() {
					config += partyId + "\n"
				}

				hash, err := CreateConfig([]byte(config))
				if err != nil {
					log.Errorf("couldnt create config: %v", err)
					return false, nil, err
				}

				// Push config to S3
				err = StoreConfig(ctx, c.minioClient, c.cpconfig.MinioBucketName, hex.EncodeToString(hash))
				if err != nil {
					log.Errorf("couldnt push config: %v", err)
					return false, nil, err
				}

				err = c.CreateCheckpoint(ctx, cp, hash)
				if err != nil {
					log.Errorf("couldnt create checkpoint: %v", err)
					return false, nil, err
				}
			}
		}

		// If Power Actors list has changed start DKG
		// Changes detected so generate new key
		if oldSt.MinerCount != newSt.MinerCount {
			fmt.Println("Generate new config")
			err := c.GenerateNewKeys(ctx, newSt.Miners)
			if err != nil {
				log.Errorf("error while generating new key: %v", err)
				// If generating new key failed, checkpointing should not be possible
			}

			return true, nil, nil
		}

		return false, nil, nil
	}

	err := c.events.StateChanged(checkFunc, changeHandler, revertHandler, 5, 76587687658765876, match)
	if err != nil {
		return
	}
}

func (c *CheckpointingSub) Start(ctx context.Context) error {
	topic, err := c.pubsub.Join("keygen")
	if err != nil {
		return err
	}
	c.topic = topic

	// and subscribe to it
	// INCREASE THE BUFFER SIZE BECAUSE IT IS ONLY 32 !
	// https://github.com/libp2p/go-libp2p-pubsub/blob/v0.5.4/pubsub.go#L1222
	sub, err := topic.Subscribe(pubsub.WithBufferSize(1000))
	if err != nil {
		return err
	}
	c.sub = sub

	c.listenCheckpointEvents(ctx)

	return nil
}

func (c *CheckpointingSub) GenerateNewKeys(ctx context.Context, participants []string) error {

	//idsStrings := c.newOrderParticipantsList()
	idsStrings := participants
	sort.Strings(idsStrings)

	fmt.Println("Participants list :", idsStrings)

	ids := c.formIDSlice(idsStrings)

	id := party.ID(c.host.ID().String())

	threshold := (len(idsStrings) / 2) + 1
	fmt.Println(threshold)
	n := NewNetwork(c.sub, c.topic)
	f := frost.KeygenTaproot(id, ids, threshold)

	handler, err := protocol.NewMultiHandler(f, []byte{1, 2, 3})
	if err != nil {
		return err
	}
	LoopHandler(ctx, handler, n)
	r, err := handler.Result()
	if err != nil {
		return err
	}
	fmt.Println("Result :", r)

	var ok bool
	c.newconfig, ok = r.(*keygen.TaprootConfig)
	if !ok {
		return xerrors.Errorf("state change propagated is the wrong type")
	}

	return nil
}

func (c *CheckpointingSub) CreateCheckpoint(ctx context.Context, cp, data []byte) error {
	taprootAddress, err := pubkeyToTapprootAddress(c.pubkey)
	if err != nil {
		return err
	}

	pubkey := c.config.PublicKey
	if c.newconfig != nil {
		pubkey = c.newconfig.PublicKey
	}

	pubkeyShort := genCheckpointPublicKeyTaproot(pubkey, cp)
	newTaprootAddress, err := pubkeyToTapprootAddress(pubkeyShort)
	if err != nil {
		return err
	}

	idsStrings := c.orderParticipantsList()
	fmt.Println("Participants list :", idsStrings)
	fmt.Println("Precedent tx", c.ptxid)
	ids := c.formIDSlice(idsStrings)

	if c.ptxid == "" {
		fmt.Println("Missing precedent txid")
		taprootScript := getTaprootScript(c.pubkey)
		fmt.Println(hex.EncodeToString(c.pubkey))
		success := addTaprootToWallet(c.cpconfig.BitcoinHost, taprootScript)
		if !success {
			return xerrors.Errorf("failed to add taproot address to wallet")
		}

		ptxid, err := walletGetTxidFromAddress(c.cpconfig.BitcoinHost, taprootAddress)
		if err != nil {
			return err
		}
		c.ptxid = ptxid
		fmt.Println("Found precedent txid:", c.ptxid)
	}

	index := 0
	value, scriptPubkeyBytes := getTxOut(c.cpconfig.BitcoinHost, c.ptxid, index)

	if scriptPubkeyBytes[0] != 0x51 {
		fmt.Println("Wrong txout")
		index = 1
		value, scriptPubkeyBytes = getTxOut(c.cpconfig.BitcoinHost, c.ptxid, index)
	}
	newValue := value - c.cpconfig.Fee

	payload := "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"createrawtransaction\", \"params\": [[{\"txid\":\"" + c.ptxid + "\",\"vout\": " + strconv.Itoa(index) + ", \"sequence\": 4294967295}], [{\"" + newTaprootAddress + "\": \"" + fmt.Sprintf("%.2f", newValue) + "\"}, {\"data\": \"" + hex.EncodeToString(data) + "\"}]]}"
	result := jsonRPC(c.cpconfig.BitcoinHost, payload)
	if result == nil {
		return xerrors.Errorf("cant create new transaction")
	}

	rawTransaction := result["result"].(string)

	tx, err := hex.DecodeString(rawTransaction)
	if err != nil {
		return err
	}

	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(value*100000000))
	utxo := append(buf[:], []byte{34}...)
	utxo = append(utxo, scriptPubkeyBytes...)

	hashedTx, err := TaprootSignatureHash(tx, utxo, 0x00)
	if err != nil {
		return err
	}

	/*
	 * Orchestrate the signing message
	 */

	fmt.Println("Starting signing")
	f := frost.SignTaprootWithTweak(c.config, ids, hashedTx[:], c.tweakedValue[:])
	n := NewNetwork(c.sub, c.topic)
	handler, err := protocol.NewMultiHandler(f, hashedTx[:])
	if err != nil {
		return err
	}
	LoopHandler(ctx, handler, n)
	r, err := handler.Result()
	if err != nil {
		return err
	}
	fmt.Println("Result :", r)

	// if signing is a success we register the new value
	merkleRoot := hashMerkleRoot(pubkey, cp)
	c.tweakedValue = hashTweakedValue(pubkey, merkleRoot)
	c.pubkey = pubkeyShort
	// If new config used
	if c.newconfig != nil {
		c.config = c.newconfig
		c.newconfig = nil
	}

	c.ptxid = ""

	// Only first one broadcast the transaction ?
	// Actually all participants can broadcast the transcation. It will be the same everywhere.
	rawtx := prepareWitnessRawTransaction(rawTransaction, r.(taproot.Signature))

	payload = "{\"jsonrpc\": \"1.0\", \"id\":\"wow\", \"method\": \"sendrawtransaction\", \"params\": [\"" + rawtx + "\"]}"
	result = jsonRPC(c.cpconfig.BitcoinHost, payload)
	if result["error"] != nil {
		return xerrors.Errorf("failed to broadcast transaction")
	}

	/* Need to keep this to build next one */
	newtxid := result["result"].(string)
	fmt.Println("New Txid:", newtxid)
	c.ptxid = newtxid

	return nil
}

func (c *CheckpointingSub) newOrderParticipantsList() []string {
	id := c.host.ID().String()
	var ids []string

	ids = append(ids, id)

	for _, peerID := range c.topic.ListPeers() {
		ids = append(ids, peerID.String())
	}

	sort.Strings(ids)

	return ids
}

func (c *CheckpointingSub) orderParticipantsList() []string {
	var ids []string
	for id := range c.config.VerificationShares {
		ids = append(ids, string(id))
	}

	sort.Strings(ids)

	return ids
}

func (c *CheckpointingSub) formIDSlice(ids []string) party.IDSlice {
	var _ids []party.ID
	for _, p := range ids {
		_ids = append(_ids, party.ID(p))
	}

	idsSlice := party.NewIDSlice(_ids)

	return idsSlice
}

func BuildCheckpointingSub(mctx helpers.MetricsCtx, lc fx.Lifecycle, c *CheckpointingSub) {
	ctx := helpers.LifecycleCtx(mctx, lc)

	// Ping to see if bitcoind is available
	success := bitcoindPing(c.cpconfig.BitcoinHost)
	if !success {
		// Should probably not panic here
		log.Errorf("bitcoin node not available")
		return
	}

	fmt.Println("Successfully pinged bitcoind")

	// Get first checkpoint from block 0
	ts, err := c.api.ChainGetGenesis(ctx)
	if err != nil {
		log.Errorf("couldnt get genesis tipset: %v", err)
		return
	}
	cidBytes := ts.Key().Bytes()
	publickey, err := hex.DecodeString(c.cpconfig.PublicKey)
	if err != nil {
		log.Errorf("couldnt decode public key: %v", err)
		return
	}

	btccp, err := GetLatestCheckpoint(c.cpconfig.BitcoinHost, publickey, cidBytes)
	if err != nil {
		log.Errorf("couldnt decode public key: %v", err)
		return
	}

	cp, err := GetConfig(ctx, c.minioClient, c.cpconfig.MinioBucketName, btccp.cid)

	if cp != "" {
		cpBytes, err := hex.DecodeString(cp)
		if err != nil {
			log.Errorf("couldnt decode checkpoint: %v", err)
			return
		}
		c.latestConfigCheckpoint, err = types.TipSetKeyFromBytes(cpBytes)
		if err != nil {
			log.Errorf("couldnt get tipset key from checkpoint: %v", err)
			return
		}
	}

	if c.config != nil {
		// save public key
		c.pubkey = genCheckpointPublicKeyTaproot(c.config.PublicKey, cidBytes)

		address, _ := pubkeyToTapprootAddress(c.pubkey)
		fmt.Println(address)

		// Save tweaked value
		merkleRoot := hashMerkleRoot(c.config.PublicKey, cidBytes)
		c.tweakedValue = hashTweakedValue(c.config.PublicKey, merkleRoot)
	}

	err = c.Start(ctx)
	if err != nil {
		log.Errorf("couldn't start checkpointing module: %v", err)
	}

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			// Do we need to stop something here ?
			return nil
		},
	})

}
