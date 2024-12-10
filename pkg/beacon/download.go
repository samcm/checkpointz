package beacon

import (
	"context"
	"errors"
	"fmt"
	"time"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethpandaops/checkpointz/pkg/eth"
	perrors "github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func (d *Default) downloadServingCheckpoint(ctx context.Context, checkpoint *v1.Finality) error {
	if checkpoint == nil {
		return errors.New("checkpoint is nil")
	}

	if checkpoint.Finalized == nil {
		return errors.New("finalized checkpoint is nil")
	}

	sp, err := d.Spec()
	if err != nil {
		return fmt.Errorf("failed to fetch spec: %w", err)
	}

	fork, err := sp.ForkEpochs.CurrentFork(checkpoint.Finalized.Epoch)
	if err != nil {
		return fmt.Errorf("failed to get current fork: %w", err)
	}

	d.log.
		WithField("epoch", checkpoint.Finalized.Epoch).
		WithField("fork_name", fork.Name).
		Info("Downloading serving checkpoint")

	upstream, err := d.nodes.
		Ready(ctx).
		DataProviders(ctx).
		PastFinalizedCheckpoint(ctx, checkpoint). // Ensure we attempt to fetch the bundle from a node that knows about the checkpoint.
		RandomNode(ctx)
	if err != nil {
		return perrors.Wrap(err, "no data provider node available")
	}

	block, err := d.fetchBundle(ctx, checkpoint.Finalized.Root, upstream)
	if err != nil {
		return perrors.Wrap(err, "failed to fetch bundle")
	}

	// Validate that everything is ok to serve.
	// Lighthouse ref: https://lighthouse-book.sigmaprime.io/checkpoint-sync.html#alignment-requirements
	blockSlot, err := block.Slot()
	if err != nil {
		return fmt.Errorf("failed to get slot from block: %w", err)
	}

	if blockSlot%sp.SlotsPerEpoch != 0 {
		return fmt.Errorf("block slot is not aligned from an epoch boundary: %d", blockSlot)
	}

	d.servingBundle = checkpoint
	d.metrics.ObserveServingEpoch(checkpoint.Finalized.Epoch)

	d.log.WithFields(
		logrus.Fields{
			"epoch": checkpoint.Finalized.Epoch,
			"root":  fmt.Sprintf("%#x", checkpoint.Finalized.Root),
		},
	).Info("Serving a new finalized checkpoint bundle")

	return nil
}

func (d *Default) checkGenesis(ctx context.Context) error {
	// Don't bother checking for genesis state if we don't care about states.
	if !d.shouldDownloadStates() {
		return nil
	}

	// No-Op if we already have the genesis state stored.
	block, err := d.blocks.GetBySlot(phase0.Slot(0))
	if err == nil && block != nil {
		stateRoot, errr := block.StateRoot()
		if errr == nil {
			if st, er := d.states.GetByStateRoot(stateRoot); er == nil && st != nil {
				return nil
			}
		}
	}

	d.log.Debug("Fetching genesis state")

	readyNodes := d.nodes.Ready(ctx)
	if len(readyNodes) == 0 {
		return errors.New("no nodes ready")
	}

	// Grab the genesis root
	randomNode, err := readyNodes.RandomNode(ctx)
	if err != nil {
		return err
	}

	genesisBlock, err := randomNode.Beacon.FetchBlock(ctx, "genesis")
	if err != nil {
		return err
	}

	if genesisBlock == nil {
		return errors.New("invalid genesis block")
	}

	genesisBlockRoot, err := genesisBlock.Root()
	if err != nil {
		return err
	}

	upstream, err := d.nodes.Ready(ctx).DataProviders(ctx).RandomNode(ctx)
	if err != nil {
		return err
	}

	// Fetch the bundle
	if _, err := d.fetchBundle(ctx, genesisBlockRoot, upstream); err != nil {
		return err
	}

	d.log.WithFields(logrus.Fields{
		"root": fmt.Sprintf("%#x", genesisBlockRoot),
	}).Info("Fetched genesis bundle")

	return nil
}

func (d *Default) fetchHistoricalCheckpoints(ctx context.Context, checkpoint *v1.Finality) error {
	d.historicalMutex.Lock()
	defer d.historicalMutex.Unlock()

	sp, err := d.Spec()
	if err != nil {
		return errors.New("chain spec unavailable")
	}

	if d.genesis == nil {
		return errors.New("genesis time unavailable")
	}

	// Download the previous n epochs worth of epoch boundaries if they don't already exist
	upstream, err := d.nodes.
		Ready(ctx).
		DataProviders(ctx).
		PastFinalizedCheckpoint(ctx, checkpoint).
		RandomNode(ctx)
	if err != nil {
		return errors.New("no data provider node available")
	}

	slotsInScope := make(map[phase0.Slot]struct{})

	// We always care about the genesis slot.
	slotsInScope[0] = struct{}{}

	// historicalFailureLimit is the amount of times we'll try to download a block
	// before we permanently give up.
	historicalFailureLimit := 5

	// Calculate the epoch boundaries we need to fetch
	// We'll derive the current finalized slot and then work back in intervals of SLOTS_PER_EPOCH.
	currentSlot := uint64(checkpoint.Finalized.Epoch) * uint64(sp.SlotsPerEpoch)
	for i := uint64(1); i < uint64(d.config.HistoricalEpochCount); i++ {
		slot := phase0.Slot(currentSlot - i*uint64(sp.SlotsPerEpoch))
		slotsInScope[slot] = struct{}{}
	}

	for slot := range slotsInScope {
		failureCount, exists := d.historicalSlotFailures[slot]
		if !exists {
			d.historicalSlotFailures[slot] = 0
		}

		if failureCount >= historicalFailureLimit {
			continue
		}

		if _, err := d.blocks.GetBySlot(slot); err == nil {
			continue
		}

		if _, err := d.downloadBlock(ctx, slot, upstream); err != nil {
			failureCount++

			d.log.WithError(err).
				WithField("slot", eth.SlotAsString(slot)).
				WithField("failure_count", failureCount).
				Error("Failed to download historical block")
		}

		if failureCount == historicalFailureLimit {
			d.log.WithField("slot", eth.SlotAsString(slot)).
				WithField("failure_count", failureCount).
				Error("No longer attempting to download historical block - too many failures")
		}

		d.historicalSlotFailures[slot] = failureCount

		time.Sleep(50 * time.Millisecond)
	}

	// Cleanup any banned slots that we don't care about anymore to prevent leaking memory.
	for slot := range d.historicalSlotFailures {
		if _, exists := slotsInScope[slot]; !exists {
			delete(d.historicalSlotFailures, slot)
		}
	}

	return nil
}

func (d *Default) downloadBlock(ctx context.Context, slot phase0.Slot, upstream *Node) (*spec.VersionedSignedBeaconBlock, error) {
	// If we don't know genesis time yet, don't bother fetching blocks as
	// we won't be able to calculate an expiry.
	if d.genesis == nil {
		return nil, errors.New("genesis time not known")
	}

	// Same thing with the chain spec.
	_, err := d.Spec()
	if err != nil {
		return nil, errors.New("chain spec not known")
	}

	// Check if we already have the block.
	bl, err := d.blocks.GetBySlot(slot)
	if err == nil && bl != nil {
		return bl, nil
	}

	// Download the block from our upstream.
	block, err := upstream.Beacon.FetchBlock(ctx, eth.SlotAsString(slot))
	if err != nil {
		return nil, err
	}

	if block == nil {
		return nil, errors.New("invalid block")
	}

	stateRoot, err := block.StateRoot()
	if err != nil {
		return nil, err
	}

	root, err := block.Root()
	if err != nil {
		return nil, err
	}

	if err := d.storeBlock(ctx, block); err != nil {
		return nil, err
	}

	d.log.
		WithFields(logrus.Fields{
			"slot":       slot,
			"root":       eth.RootAsString(root),
			"state_root": eth.RootAsString(stateRoot),
			"node":       upstream.Config.Name,
		}).
		Infof("Downloaded and stored block for slot %d", slot)

	return block, nil
}

func (d *Default) fetchBundle(ctx context.Context, root phase0.Root, upstream *Node) (*spec.VersionedSignedBeaconBlock, error) {
	d.log.Infof("Fetching bundle from node %s with root %#x", upstream.Config.Name, root)

	block, err := d.blocks.GetByRoot(root)
	if err != nil || block == nil {
		// Download the block.
		block, err = upstream.Beacon.FetchBlock(ctx, fmt.Sprintf("%#x", root))
		if err != nil {
			return nil, err
		}

		if block == nil {
			return nil, errors.New("block is nil")
		}
	}

	stateRoot, err := block.StateRoot()
	if err != nil {
		return nil, fmt.Errorf("failed to get state root from block: %w", err)
	}

	blockRoot, err := block.Root()
	if err != nil {
		return nil, fmt.Errorf("failed to get block root from block: %w", err)
	}

	if blockRoot != root {
		return nil, errors.New("block root does not match")
	}

	slot, err := block.Slot()
	if err != nil {
		return nil, fmt.Errorf("failed to get slot from block: %w", err)
	}

	d.log.
		WithField("slot", slot).
		WithField("root", fmt.Sprintf("%#x", blockRoot)).
		WithField("state_root", fmt.Sprintf("%#x", stateRoot)).
		Info("Fetched beacon block")

	err = d.storeBlock(ctx, block)
	if err != nil {
		return nil, fmt.Errorf("failed to store block: %w", err)
	}

	if d.shouldDownloadStates() {
		// Download and store beacon state
		if err = d.downloadAndStoreBeaconState(ctx, stateRoot, slot, upstream); err != nil {
			return nil, fmt.Errorf("failed to download and store beacon state: %w", err)
		}
	}

	sp, err := d.Spec()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch spec: %w", err)
	}

	epoch := phase0.Epoch(slot / sp.SlotsPerEpoch)

	if slot != phase0.Slot(0) {
		// Download and store deposit snapshots
		if err = d.downloadAndStoreDepositSnapshot(ctx, epoch, upstream); err != nil {
			d.log.WithError(err).
				Warn("failed to download and store deposit snapshot")
		}
	}

	denebFork, err := sp.ForkEpochs.GetByName("DENEB")
	if err == nil && denebFork != nil {
		if denebFork.Active(epoch) {
			// Download and store blob sidecars
			if err := d.downloadAndStoreBlobSidecars(ctx, slot, upstream); err != nil {
				return nil, fmt.Errorf("failed to download and store blob sidecars: %w", err)
			}
		}
	}

	d.log.WithFields(logrus.Fields{
		"block_root": eth.RootAsString(root),
		"state_root": eth.RootAsString(stateRoot),
	}).Infof("Successfully fetched bundle from %s", upstream.Config.Name)

	return block, nil
}

func (d *Default) downloadAndStoreBeaconState(ctx context.Context, stateRoot phase0.Root, slot phase0.Slot, node *Node) error {
	// If the state already exists, don't bother downloading it again.
	existingState, err := d.states.GetByStateRoot(stateRoot)
	if err == nil && existingState != nil {
		return nil
	}

	beaconState, err := node.Beacon.FetchBeaconState(ctx, eth.SlotAsString(slot))
	if err != nil {
		return fmt.Errorf("failed to fetch beacon state: %w", err)
	}

	if beaconState == nil {
		return errors.New("beacon state is nil")
	}

	expiresAt := time.Now().Add(FinalityHaltedServingPeriod)
	if slot == phase0.Slot(0) {
		expiresAt = time.Now().Add(999999 * time.Hour)
	}

	if err := d.states.Add(stateRoot, beaconState, expiresAt, slot); err != nil {
		return fmt.Errorf("failed to store beacon state: %w", err)
	}

	return nil
}

func (d *Default) downloadAndStoreDepositSnapshot(ctx context.Context, epoch phase0.Epoch, node *Node) error {
	// Check if we already have the deposit snapshot.
	if _, err := d.depositSnapshots.GetByEpoch(epoch); err == nil {
		return nil
	}

	// Download the deposit snapshot from our upstream.
	depositSnapshot, err := node.Beacon.FetchDepositSnapshot(ctx)
	if err != nil {
		return err
	}

	if depositSnapshot == nil {
		return errors.New("invalid deposit snapshot")
	}

	// These are small so store them for a month. Max items will most likely purge it before then.
	// Mostly just guarding against periods of non-finality; we won't have new items to purge the old ones which
	// is a good thing here.
	expiresAt := time.Now().Add(672 * time.Hour)

	if err := d.depositSnapshots.Add(epoch, depositSnapshot, expiresAt); err != nil {
		return fmt.Errorf("failed to store deposit snapshot: %w", err)
	}

	d.log.
		WithFields(logrus.Fields{
			"epoch": epoch,
			"node":  node.Config.Name,
		}).
		Infof("Downloaded and stored deposit snapshot for epoch %d", epoch)

	return nil
}

func (d *Default) downloadAndStoreBlobSidecars(ctx context.Context, slot phase0.Slot, node *Node) error {
	// Check if we already have the blob sidecars.
	if _, err := d.blobSidecars.GetBySlot(slot); err == nil {
		return nil
	}

	// Download the blob sidecars from our upstream.
	blobSidecars, err := node.Beacon.FetchBeaconBlockBlobs(ctx, eth.SlotAsString(slot))
	if err != nil {
		return err
	}

	if blobSidecars == nil {
		return errors.New("invalid blob sidecars")
	}

	// Store for the FinalityHaltedServingPeriod to ensure we have them in case of non-finality.
	// We'll let the store handle purging old items.
	expiresAt := time.Now().Add(FinalityHaltedServingPeriod)

	if err := d.blobSidecars.Add(slot, blobSidecars, expiresAt); err != nil {
		return fmt.Errorf("failed to store blob sidecars: %w", err)
	}

	d.log.
		WithFields(logrus.Fields{
			"slot": slot,
			"node": node.Config.Name,
		}).
		Infof("Downloaded and stored blob sidecar for slot %d", slot)

	return nil
}
