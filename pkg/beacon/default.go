package beacon

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/chuckpreslar/emission"
	"github.com/ethpandaops/beacon/pkg/beacon"
	"github.com/ethpandaops/beacon/pkg/beacon/api/types"
	"github.com/ethpandaops/beacon/pkg/beacon/state"
	"github.com/ethpandaops/checkpointz/pkg/beacon/checkpoints"
	"github.com/ethpandaops/checkpointz/pkg/beacon/node"
	"github.com/ethpandaops/checkpointz/pkg/beacon/store"
	"github.com/ethpandaops/checkpointz/pkg/eth"
	"github.com/ethpandaops/ethwallclock"
	"github.com/go-co-op/gocron"
	perrors "github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type Default struct {
	log logrus.FieldLogger

	config      *Config
	nodeConfigs []node.Config
	nodes       Nodes
	broker      *emission.Emitter

	head          *phase0.Checkpoint
	servingBundle *phase0.Checkpoint

	blocks           *store.Block
	states           *store.BeaconState
	depositSnapshots *store.DepositSnapshot

	spec    *state.Spec
	genesis *v1.Genesis

	historicalSlotFailures map[phase0.Slot]int

	servingMutex    sync.Mutex
	historicalMutex sync.Mutex
	majorityMutex   sync.Mutex

	metrics *Metrics
}

var _ FinalityProvider = (*Default)(nil)

var (
	topicFinalityHeadUpdated = "finality_head_updated"
)

const (
	// FinalityHaltedServingPeriod defines how long we will happily serve finality data for after the chain has stopped finality.
	// TODO(sam.calder-mason): Derive from weak subjectivity period.
	FinalityHaltedServingPeriod = 14 * 24 * time.Hour
)

func NewDefaultProvider(namespace string, log logrus.FieldLogger, nodes []node.Config, config *Config) FinalityProvider {
	return &Default{
		nodeConfigs: nodes,
		log:         log.WithField("module", "beacon/default"),
		nodes:       NewNodesFromConfig(log, nodes, namespace),
		config:      config,

		head:          nil,
		servingBundle: nil,

		historicalSlotFailures: make(map[phase0.Slot]int),

		broker:           emission.NewEmitter(),
		blocks:           store.NewBlock(log, config.Caches.Blocks, namespace),
		states:           store.NewBeaconState(log, config.Caches.States, namespace),
		depositSnapshots: store.NewDepositSnapshot(log, config.Caches.DepositSnapshots, namespace),

		servingMutex:    sync.Mutex{},
		historicalMutex: sync.Mutex{},
		majorityMutex:   sync.Mutex{},

		metrics: NewMetrics(namespace + "_beacon"),
	}
}

func (d *Default) Start(ctx context.Context) error {
	d.log.Infof("Starting Finality provider in %s mode", d.OperatingMode())

	d.metrics.ObserveOperatingMode(d.OperatingMode())

	if err := d.nodes.StartAll(ctx); err != nil {
		return err
	}

	go func() {
		for {
			// Wait until we have a single healthy node.
			_, err := d.nodes.Healthy(ctx).NotSyncing(ctx).RandomNode(ctx)
			if err != nil {
				d.log.WithError(err).Error("Waiting for a healthy, non-syncing node before beginning..")
				time.Sleep(time.Second * 5)

				continue
			}

			if err := d.startCrons(ctx); err != nil {
				d.log.WithError(err).Fatal("Failed to start crons")
			}

			break
		}
	}()

	// Subscribe to the nodes' finality updates.
	for _, node := range d.nodes {
		n := node

		logCtx := d.log.WithFields(logrus.Fields{
			"node": n.Config.Name,
		})

		n.Beacon.OnFinalityCheckpointUpdated(ctx, func(ctx context.Context, event *beacon.FinalityCheckpointUpdated) error {
			logCtx.WithFields(logrus.Fields{
				"epoch": event.Finality.Finalized.Epoch,
				"root":  fmt.Sprintf("%#x", event.Finality.Finalized.Root),
			}).Info("Node has a new finalized checkpoint")

			// Check if we have a new majority finality.
			if err := d.checkFinality(ctx); err != nil {
				logCtx.WithError(err).Error("Failed to check finality")

				return err
			}

			return d.checkForNewServingCheckpoint(ctx)
		})

		n.Beacon.OnReady(ctx, func(ctx context.Context, _ *beacon.ReadyEvent) error {
			n.Beacon.Wallclock().OnEpochChanged(func(epoch ethwallclock.Epoch) {
				time.Sleep(time.Second * 5)

				if _, err := node.Beacon.FetchFinality(ctx, "head"); err != nil {
					logCtx.WithError(err).Error("Failed to fetch finality after epoch transition")
				}

				if err := d.checkFinality(ctx); err != nil {
					logCtx.WithError(err).Error("Failed to check finality")
				}

				if err := d.checkForNewServingCheckpoint(ctx); err != nil {
					logCtx.WithError(err).Error("Failed to check for new serving checkpoint after epoch change")
				}
			})

			return nil
		})
	}

	return nil
}

func (d *Default) startCrons(ctx context.Context) error {
	s := gocron.NewScheduler(time.Local)

	if _, err := s.Every("30s").Do(func() {
		if err := d.checkFinality(ctx); err != nil {
			d.log.WithError(err).Error("Failed to check finality")
		}
	}); err != nil {
		return err
	}

	if _, err := s.Every("10s").Do(func() {
		if err := d.checkBeaconSpec(ctx); err != nil {
			d.log.WithError(err).Error("Failed to check beacon chain spec")
		}
	}); err != nil {
		return err
	}

	if _, err := s.Every("3m").Do(func() {
		for _, node := range d.nodes.Healthy(ctx) {
			if _, err := node.Beacon.FetchFinality(ctx, "head"); err != nil {
				d.log.WithError(err).Error("Failed to fetch finality when polling")
			}
		}
	}); err != nil {
		return err
	}

	go func() {
		if err := d.startGenesisLoop(ctx); err != nil {
			d.log.WithError(err).Fatal("Failed to start genesis loop")
		}
	}()

	go func() {
		if err := d.startServingLoop(ctx); err != nil {
			d.log.WithError(err).Fatal("Failed to start serving loop")
		}
	}()

	go func() {
		if err := d.startHistoricalLoop(ctx); err != nil {
			d.log.WithError(err).Fatal("Failed to start historical loop")
		}
	}()

	s.StartAsync()

	return nil
}

func (d *Default) StartAsync(ctx context.Context) {
	go func() {
		if err := d.Start(ctx); err != nil {
			d.log.WithError(err).Error("Failed to start")
		}
	}()
}

func (d *Default) startGenesisLoop(ctx context.Context) error {
	if err := d.checkGenesis(ctx); err != nil {
		d.log.WithError(err).Error("Failed to check for genesis bundle")
	}

	if err := d.checkGenesisTime(ctx); err != nil {
		d.log.WithError(err).Error("Failed to check genesis time")
	}

	for {
		select {
		case <-time.After(time.Second * 15):
			if err := d.checkGenesisTime(ctx); err != nil {
				d.log.WithError(err).Error("Failed to check genesis time")
			}

			if err := d.checkGenesis(ctx); err != nil {
				d.log.WithError(err).Error("Failed to check for genesis")
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (d *Default) startHistoricalLoop(ctx context.Context) error {
	for {
		select {
		case <-time.After(time.Second * 15):
			if d.head == nil {
				continue
			}

			if err := d.fetchHistoricalCheckpoints(ctx, d.head); err != nil {
				d.log.WithError(err).Error("Failed to fetch historical checkpoints")
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (d *Default) startServingLoop(ctx context.Context) error {
	if err := d.checkForNewServingCheckpoint(ctx); err != nil {
		d.log.WithError(err).Error("Failed to check for serving checkpoint")
	}

	for {
		select {
		case <-time.After(time.Second * 5):
			if err := d.checkForNewServingCheckpoint(ctx); err != nil {
				d.log.WithError(err).Error("Failed to check for new serving checkpoint")

				time.Sleep(time.Second * 15)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (d *Default) checkForNewServingCheckpoint(ctx context.Context) error {
	d.servingMutex.Lock()
	defer d.servingMutex.Unlock()

	// Don't bother checking if we don't know the head yet.
	if d.head == nil {
		return errors.New("head finality is unknown")
	}

	logCtx := d.log.WithFields(logrus.Fields{
		"head_epoch": d.head.Epoch,
		"head_root":  fmt.Sprintf("%#x", d.head.Root),
	})

	// If we don't have a serving bundle already, download one.
	if d.servingBundle == nil {
		logCtx.Info("No serving bundle available, downloading and serving any available historical checkpoint")

		return d.downloadServingCheckpoint(ctx, d.head)
	}

	// If the head has moved on, download a new serving bundle.
	if d.servingBundle.Epoch != d.head.Epoch {
		logCtx.
			WithField("serving_epoch", d.servingBundle.Epoch).
			WithField("serving_root", fmt.Sprintf("%#x", d.servingBundle.Root)).
			Info("Head finality has advanced, downloading new serving bundle")

		return d.downloadServingCheckpoint(ctx, d.head)
	}

	return nil
}

func (d *Default) serveInitialCheckpoint(ctx context.Context, head *phase0.Checkpoint) error {
	/*
		We're booting up for the first time and need to serve a checkpoint.
		We'll attempt to serve the "head" checkpoint, but if that fails we'll
		work our way back through epochs until we find a checkpoint that we can serve.
	*/

	if head == nil {
		return errors.New("head checkpoint is nil")
	}

	// If we have a head checkpoint, we'll attempt to serve it.
	if err := d.downloadServingCheckpoint(ctx, head); err == nil {
		return nil

	}

	// We'll go back as far as our historical epoch is configured, ensuring we don't go below epoch 0.
	for i := 0; i < d.config.HistoricalEpochCount; i++ {
		proposedEpoch := int64(head.Epoch) - int64(i)
		if proposedEpoch < 0 {
			break
		}

		logCtx := d.log.WithFields(logrus.Fields{
			"proposed_epoch":     proposedEpoch,
			"distance_from_head": i,
			"cause":              "initial_serve",
		})

		epoch := phase0.Epoch(proposedEpoch)

		// We need to grab the root of the slot at the start of the epoch.
		slot := phase0.Slot(uint64(epoch) / uint64(d.spec.SlotsPerEpoch))

		upstream, err := d.nodes.
			Ready(ctx).
			DataProviders(ctx).
			HasFinalizedCheckpoint(ctx, head). // Ensure we attempt to fetch the bundle from a node that knows about the checkpoint.
			RandomNode(ctx)
		if err != nil {
			logCtx.WithError(err).Error("Failed to find a node")

			continue
		}

		block, err := upstream.Beacon.FetchBlock(ctx, eth.SlotAsString(slot))
		if err != nil {
			logCtx.WithError(err).Error("Failed to fetch block")

			continue
		}

		root, err := block.Root()
		if err != nil {
			logCtx.WithError(err).Error("Failed to get block root")

			continue
		}

		// We'll attempt to serve the checkpoint.
		if err := d.downloadServingCheckpoint(ctx, &phase0.Checkpoint{
			Epoch: epoch,
			Root:  root,
		}); err == nil {
			return nil
		}
	}

	return errors.New("failed to serve any checkpoint")
}

func (d *Default) Healthy(ctx context.Context) (bool, error) {
	if len(d.nodes.Healthy(ctx)) == 0 {
		return false, nil
	}

	return true, nil
}

func (d *Default) Peers(ctx context.Context) (types.Peers, error) {
	peers := types.Peers{}

	for _, node := range d.nodes {
		status := "connected"

		if node.Beacon.Status().Syncing() || !node.Beacon.Status().Healthy() {
			status = "disconnected"
		}

		peers = append(peers, types.Peer{
			PeerID:    node.Config.Name,
			State:     status,
			Direction: "outbound",
		})
	}

	return peers, nil
}

func (d *Default) Syncing(ctx context.Context) (*v1.SyncState, error) {
	syncing := len(d.nodes.Healthy(ctx).Syncing(ctx)) == len(d.nodes.Healthy(ctx))

	syncState := &v1.SyncState{
		IsSyncing:    syncing,
		HeadSlot:     0,
		SyncDistance: 0,
	}

	sp, err := d.Spec(ctx)
	if err != nil {
		return syncState, err
	}

	if sp == nil {
		return syncState, errors.New("spec unknown")
	}

	if d.head != nil && d.head != nil {
		syncState.HeadSlot = phase0.Slot(d.head.Epoch) * sp.SlotsPerEpoch
	}

	if d.servingBundle != nil && d.servingBundle != nil {
		syncState.SyncDistance = syncState.HeadSlot - phase0.Slot(d.servingBundle.Epoch)*sp.SlotsPerEpoch
	}

	return syncState, nil
}

func (d *Default) Finalized(ctx context.Context) (*phase0.Checkpoint, error) {
	if d.servingBundle == nil {
		return nil, errors.New("finalized checkpoint is unknown")
	}

	return d.servingBundle, nil
}

func (d *Default) Head(ctx context.Context) (*phase0.Checkpoint, error) {
	if d.head == nil {
		return nil, errors.New("head checkpoint is unknown")
	}

	return d.head, nil
}

func (d *Default) Genesis(ctx context.Context) (*v1.Genesis, error) {
	if d.genesis == nil {
		return nil, errors.New("genesis bundle not yet available")
	}

	return d.genesis, nil
}

func (d *Default) Spec(ctx context.Context) (*state.Spec, error) {
	if d.spec == nil {
		return nil, errors.New("config spec not yet available")
	}

	return d.spec, nil
}

func (d *Default) OperatingMode() OperatingMode {
	return d.config.Mode
}

func (d *Default) shouldDownloadStates() bool {
	return d.OperatingMode() == OperatingModeFull
}

func (d *Default) checkFinality(ctx context.Context) error {
	d.majorityMutex.Lock()
	defer d.majorityMutex.Unlock()

	aggFinality := []*v1.Finality{}
	readyNodes := d.nodes.Ready(ctx)

	for _, node := range readyNodes {
		finality, err := node.Beacon.Finality()
		if err != nil {
			d.log.Infof("Failed to get finality from node %s", node.Config.Name)

			continue
		}

		aggFinality = append(aggFinality, finality)
	}

	majority, err := checkpoints.NewMajorityDecider().Decide(aggFinality)
	if err != nil {
		return perrors.Wrap(err, "failed to decide majority finality")
	}

	if d.head == nil || d.head == nil || d.head.Root != majority.Finalized.Root {
		d.head = majority.Finalized

		d.publishFinalityCheckpointHeadUpdated(ctx, majority)

		d.log.
			WithField("epoch", majority.Finalized.Epoch).
			WithField("root", fmt.Sprintf("%#x", majority.Finalized.Root)).
			Info("New finalized head checkpoint")

		d.metrics.ObserveHeadEpoch(majority.Finalized.Epoch)
	}

	return nil
}

func (d *Default) checkBeaconSpec(ctx context.Context) error {
	// No-Op if we already have a beacon spec
	if d.spec != nil {
		return nil
	}

	d.log.Debug("Fetching beacon spec")

	upstream, err := d.nodes.Ready(ctx).DataProviders(ctx).RandomNode(ctx)
	if err != nil {
		return err
	}

	s, err := upstream.Beacon.Spec()
	if err != nil {
		return err
	}

	// store the beacon state spec
	d.spec = s

	d.log.Info("Fetched beacon spec")

	return nil
}

func (d *Default) checkGenesisTime(ctx context.Context) error {
	// No-Op if we already have a genesis time
	if d.genesis != nil {
		return nil
	}

	d.log.Debug("Fetching genesis time")

	upstream, err := d.nodes.Ready(ctx).DataProviders(ctx).RandomNode(ctx)
	if err != nil {
		return err
	}

	g, err := upstream.Beacon.Genesis()
	if err != nil {
		return err
	}

	// store the genesis time
	d.genesis = g

	d.log.Info("Fetched genesis time")

	return nil
}

func (d *Default) OnFinalityCheckpointHeadUpdated(ctx context.Context, cb func(ctx context.Context, checkpoint *v1.Finality) error) {
	d.broker.On(topicFinalityHeadUpdated, func(checkpoint *v1.Finality) {
		if err := cb(ctx, checkpoint); err != nil {
			d.log.WithError(err).Error("Failed to handle finality updated")
		}
	})
}

func (d *Default) publishFinalityCheckpointHeadUpdated(ctx context.Context, checkpoint *v1.Finality) {
	d.broker.Emit(topicFinalityHeadUpdated, checkpoint)
}

func (d *Default) GetBlockBySlot(ctx context.Context, slot phase0.Slot) (*spec.VersionedSignedBeaconBlock, error) {
	block, err := d.blocks.GetBySlot(slot)
	if err != nil {
		return nil, err
	}

	if block == nil {
		return nil, errors.New("block not found")
	}

	return block, nil
}

func (d *Default) GetBlockByRoot(ctx context.Context, root phase0.Root) (*spec.VersionedSignedBeaconBlock, error) {
	block, err := d.blocks.GetByRoot(root)
	if err != nil {
		return nil, err
	}

	if block == nil {
		return nil, errors.New("block not found")
	}

	return block, nil
}

func (d *Default) GetBlockByStateRoot(ctx context.Context, stateRoot phase0.Root) (*spec.VersionedSignedBeaconBlock, error) {
	block, err := d.blocks.GetByStateRoot(stateRoot)
	if err != nil {
		return nil, err
	}

	if block == nil {
		return nil, errors.New("block not found")
	}

	return block, nil
}

func (d *Default) GetBeaconStateBySlot(ctx context.Context, slot phase0.Slot) (*[]byte, error) {
	block, err := d.GetBlockBySlot(ctx, slot)
	if err != nil {
		return nil, err
	}

	stateRoot, err := block.StateRoot()
	if err != nil {
		return nil, err
	}

	return d.states.GetByStateRoot(stateRoot)
}

func (d *Default) GetBeaconStateByStateRoot(ctx context.Context, stateRoot phase0.Root) (*[]byte, error) {
	return d.states.GetByStateRoot(stateRoot)
}

func (d *Default) GetBeaconStateByRoot(ctx context.Context, root phase0.Root) (*[]byte, error) {
	block, err := d.GetBlockByRoot(ctx, root)
	if err != nil {
		return nil, err
	}

	stateRoot, err := block.StateRoot()
	if err != nil {
		return nil, err
	}

	return d.states.GetByStateRoot(stateRoot)
}

func (d *Default) storeBlock(ctx context.Context, block *spec.VersionedSignedBeaconBlock) error {
	if d.spec == nil {
		return errors.New("beacon chain spec is unknown")
	}

	if d.genesis == nil {
		return errors.New("genesis time is unknown")
	}

	if block == nil {
		return errors.New("block is nil")
	}

	root, err := block.Root()
	if err != nil {
		return err
	}

	exists, err := d.blocks.GetByRoot(root)
	if err == nil && exists != nil {
		return nil
	}

	slot, err := block.Slot()
	if err != nil {
		return err
	}

	expiresAt := time.Now().Add(FinalityHaltedServingPeriod)

	if slot == phase0.Slot(0) {
		expiresAt = time.Now().Add(999999 * time.Hour)
	}

	if err := d.blocks.Add(block, expiresAt); err != nil {
		return err
	}

	return nil
}

func (d *Default) UpstreamsStatus(ctx context.Context) (map[string]*UpstreamStatus, error) {
	rsp := make(map[string]*UpstreamStatus)

	for _, node := range d.nodes {
		rsp[node.Config.Name] = &UpstreamStatus{
			Name:    node.Config.Name,
			Healthy: false,
		}

		rsp[node.Config.Name].Healthy = node.Beacon.Status().Healthy()

		//nolint:gocritic // invalid
		if spec, err := node.Beacon.Spec(); err == nil {
			network := spec.ConfigName
			if network == "" {
				// Fall back to our static map.
				network = eth.GetNetworkName(spec.DepositChainID)
			}

			rsp[node.Config.Name].NetworkName = network
		}

		finality, err := node.Beacon.Finality()
		if err != nil {
			continue
		}

		if finality == nil {
			continue
		}

		rsp[node.Config.Name].Finality = finality
	}

	return rsp, nil
}

func (d *Default) ListFinalizedSlots(ctx context.Context) ([]phase0.Slot, error) {
	slots := []phase0.Slot{}
	if d.spec == nil {
		return slots, errors.New("no beacon chain spec available")
	}

	finality, err := d.Head(ctx)
	if err != nil {
		return slots, err
	}

	if finality == nil {
		return slots, errors.New("no finalized checkpoint available")
	}

	latestSlot := phase0.Slot(uint64(finality.Epoch) * uint64(d.spec.SlotsPerEpoch))

	for i, val := uint64(latestSlot), uint64(latestSlot)-uint64(d.spec.SlotsPerEpoch)*uint64(d.config.HistoricalEpochCount); i > val; i -= uint64(d.spec.SlotsPerEpoch) {
		slots = append(slots, phase0.Slot(i))
	}

	return slots, nil
}

func (d *Default) GetEpochBySlot(ctx context.Context, slot phase0.Slot) (phase0.Epoch, error) {
	if d.spec == nil {
		return phase0.Epoch(0), errors.New("no upstream beacon state spec available")
	}

	return phase0.Epoch(uint64(slot) / uint64(d.spec.SlotsPerEpoch)), nil
}

func (d *Default) PeerCount(ctx context.Context) (uint64, error) {
	return uint64(len(d.nodes.Healthy(ctx).NotSyncing(ctx))), nil
}

func (d *Default) GetSlotTime(ctx context.Context, slot phase0.Slot) (eth.SlotTime, error) {
	SlotTime := eth.SlotTime{}

	if d.spec == nil {
		return SlotTime, errors.New("no upstream beacon state spec available")
	}

	if d.genesis == nil {
		return SlotTime, errors.New("genesis time is unknown")
	}

	return eth.CalculateSlotTime(slot, d.genesis.GenesisTime, d.spec.SecondsPerSlot.AsDuration()), nil
}

func (d *Default) GetDepositSnapshot(ctx context.Context, epoch phase0.Epoch) (*types.DepositSnapshot, error) {
	return d.depositSnapshots.GetByEpoch(epoch)
}
