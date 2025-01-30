import { useState, useMemo } from 'react';

import { ExclamationTriangleIcon } from '@heroicons/react/20/solid';
import { useQuery } from '@tanstack/react-query';

import Loading from '@components/Loading';
import useStatus from '@hooks/status';
import SlotSlideout from '@parts/slot/SlotSlideout';
import { APIBeaconSlots, APIBeaconSlot } from '@types';

import CheckpointsTable from './CheckpointsTable';

export default function Checkpoints() {
  const [slot, setSlot] = useState<APIBeaconSlot | undefined>(undefined);
  const { data, isLoading, error } = useQuery<APIBeaconSlots, Error>(
    ['beacon_slots'],
    async () => {
      const res = await fetch('/checkpointz/v1/beacon/slots');
      return res.json();
    },
    { refetchInterval: 60_000 },
  );
  const { data: statusData } = useStatus({ refetchInterval: 60_000 });
  const latestEpoch = useMemo(() => {
    const finalizedEpoch = statusData?.data?.finality?.finalized?.epoch;
    if (!finalizedEpoch) return;
    return finalizedEpoch;
  }, [statusData]);

  if (isLoading)
    return (
      <div className="flex justify-center pt-10">
        <Loading />
      </div>
    );
  if (!data && error)
    return (
      <div className="flex justify-center">
        <div className="flex justify-center bg-white/20 mt-5 py-5 px-10 font-semibold rounded-lg text-gray-100">
          <ExclamationTriangleIcon className="h-6 w-6 text-yellow-400 pr-1" aria-hidden="true" />
          Something went wrong
        </div>
      </div>
    );
  return (
    <>
      <SlotSlideout slot={slot?.slot} onClose={() => setSlot(undefined)} />
      <CheckpointsTable
        slots={Object.values(data?.data?.slots ?? {})}
        latestEpoch={latestEpoch}
        showCheckpoint={statusData?.data?.operating_mode === 'full'}
        onSlotClick={setSlot}
      />
    </>
  );
}
