import { useMemo, useState } from 'react';

import { ExclamationTriangleIcon } from '@heroicons/react/20/solid';
import { useQuery } from '@tanstack/react-query';
import clsx from 'clsx';
import { format } from 'date-fns';
import ReactTimeAgo from 'react-time-ago';

import CopyToClipboard from '@components/CopyToClipboard';
import Loading from '@components/Loading';
import Tooltip from '@components/Tooltip';
import { APIBeaconSlotBlock, APIBeaconBlockMessage } from '@types';
import { truncateHash, hexToAscii } from '@utils';

export default function Slot(props: { slot: number }) {
  const [showFullHash, setShowFullHash] = useState(false);
  const { data, isLoading } = useQuery<
    APIBeaconSlotBlock,
    Error,
    APIBeaconSlotBlock,
    [string, { slot: number }]
  >(['beacon_slot', { slot: props.slot }], async ({ queryKey }) => {
    const [, { slot }] = queryKey;
    const res = await fetch(`/checkpointz/v1/beacon/slots/${slot}`);
    return res.json();
  });
  const block = useMemo<APIBeaconBlockMessage | undefined>(() => {
    switch (data?.data?.block?.Version?.toUpperCase()) {
      case 'ALTAIR':
        return data?.data?.block?.Altair;
      case 'BELLATRIX':
        return data?.data?.block?.Bellatrix;
      case 'CAPELLA':
        return data?.data?.block?.Capella;
      case 'DENEB':
        return data?.data?.block?.Deneb;
      case 'ELECTRA':
        return data?.data?.block?.Electra;
      case 'PHASE0':
        return data?.data?.block?.Phase0;
    }
  }, [data]);
  const blockHash = useMemo(() => {
    if (['ALTAIR', 'PHASE0'].includes(data?.data?.block?.Version?.toUpperCase() ?? '')) {
      return block?.message?.body?.eth1_data?.block_hash;
    }
    return (
      block?.message?.body?.eth1_data?.block_hash ??
      block?.message?.body?.execution_payload?.block_hash
    );
  }, [data, block]);
  const time = useMemo<string | undefined>(() => {
    if (!data?.data?.time?.start_time) return;
    return format(new Date(data?.data?.time?.start_time), 'PPP pp');
  }, [data]);
  if (isLoading)
    return (
      <div className="flex justify-center pt-10">
        <Loading />
      </div>
    );
  if (!block?.message)
    return (
      <div className="flex justify-center font-bold">
        <ExclamationTriangleIcon className="h-10 w-10 text-yellow-400 pr-1 " aria-hidden="true" />
        <span className="text-2xl pt-1">Something went wrong</span>
      </div>
    );
  return (
    <div className="bg-white shadow overflow-hidden sm:rounded-lg">
      <div className="border-t border-gray-200 px-4 py-5 sm:p-0">
        <dl className="sm:divide-y sm:divide-gray-200">
          <div className="py-4 sm:py-5 sm:grid sm:grid-cols-5 sm:gap-4 sm:px-6">
            <dt className="text-sm font-medium text-gray-500">Epoch</dt>
            <dd className="mt-1 text-sm text-gray-900 sm:mt-0 sm:col-span-4">
              {data?.data?.epoch}
            </dd>
          </div>
          <div className="py-4 sm:py-5 sm:grid sm:grid-cols-5 sm:gap-4 sm:px-6">
            <dt className="text-sm font-medium text-gray-500">Slot</dt>
            <dd className="mt-1 text-sm text-gray-900 sm:mt-0 sm:col-span-4">
              {block.message.slot}
            </dd>
          </div>
          <div className="py-4 sm:py-5 sm:grid sm:grid-cols-5 sm:gap-4 sm:px-6">
            <dt className="text-sm font-medium text-gray-500">Time</dt>
            <dd className="mt-1 text-sm text-gray-900 sm:mt-0 sm:col-span-4">
              {data?.data?.time?.start_time && time && (
                <>
                  <ReactTimeAgo date={new Date(data.data.time.start_time)} />
                  <span className="pl-1">({time})</span>
                </>
              )}
            </dd>
          </div>
          <div className="py-4 sm:py-5 sm:grid sm:grid-cols-5 sm:gap-4 sm:px-6">
            <dt className="text-sm font-medium text-gray-500">Proposer</dt>
            <dd className="mt-1 text-sm text-gray-900 sm:mt-0 sm:col-span-4">
              {block.message.proposer_index}
            </dd>
          </div>
          <div className="py-4 sm:py-5 sm:grid sm:grid-cols-5 sm:gap-4 sm:px-6">
            <dt className="text-sm font-medium text-gray-500">Block Root</dt>
            <dd className="mt-1 text-sm text-gray-900 sm:mt-0 sm:col-span-4">
              {blockHash && (
                <>
                  <span className="lg:hidden font-mono flex">
                    <Tooltip content={blockHash}>
                      <span className="relative top-1 group transition duration-300 cursor-pointer">
                        {truncateHash(blockHash)}
                        <span className="relative -top-0.5 block max-w-0 group-hover:max-w-full transition-all duration-500 h-0.5 bg-fuchsia-400"></span>
                      </span>
                    </Tooltip>
                    <CopyToClipboard text={blockHash} />
                  </span>
                  <span className="hidden lg:table-cell font-mono">
                    {blockHash}
                    <CopyToClipboard text={blockHash} />
                  </span>
                </>
              )}
            </dd>
          </div>
          <div className="py-4 sm:py-5 sm:grid sm:grid-cols-5 sm:gap-4 sm:px-6">
            <dt className="text-sm font-medium text-gray-500">Parent Root</dt>
            <dd className="mt-1 text-sm text-gray-900 sm:mt-0 sm:col-span-4">
              {block.message.parent_root && (
                <>
                  <span className="lg:hidden font-mono flex">
                    <Tooltip content={block.message.parent_root}>
                      <span className="relative top-1 group transition duration-300 cursor-pointer">
                        {truncateHash(block.message.parent_root)}
                        <span className="relative -top-0.5 block max-w-0 group-hover:max-w-full transition-all duration-500 h-0.5 bg-fuchsia-400"></span>
                      </span>
                    </Tooltip>
                    <CopyToClipboard text={block.message.parent_root} />
                  </span>
                  <span className="hidden lg:table-cell font-mono">
                    {block.message.parent_root}
                    <CopyToClipboard text={block.message.parent_root} />
                  </span>
                </>
              )}
            </dd>
          </div>
          <div className="py-4 sm:py-5 sm:grid sm:grid-cols-5 sm:gap-4 sm:px-6">
            <dt className="text-sm font-medium text-gray-500">State Root</dt>
            <dd className="mt-1 text-sm text-gray-900 sm:mt-0 sm:col-span-4">
              {block.message.state_root && (
                <>
                  <span className="lg:hidden font-mono flex">
                    <Tooltip content={block.message.state_root}>
                      <span className="relative top-1 group transition duration-300 cursor-pointer">
                        {truncateHash(block.message.state_root)}
                        <span className="relative -top-0.5 block max-w-0 group-hover:max-w-full transition-all duration-500 h-0.5 bg-fuchsia-400"></span>
                      </span>
                    </Tooltip>
                    <CopyToClipboard text={block.message.state_root} />
                  </span>
                  <span className="hidden lg:table-cell font-mono">
                    {block.message.state_root}
                    <CopyToClipboard text={block.message.state_root} />
                  </span>
                </>
              )}
            </dd>
          </div>
          <div className="py-4 sm:py-5 sm:grid sm:grid-cols-5 sm:gap-4 sm:px-6">
            <dt className="text-sm font-medium text-gray-500">Signature</dt>
            <dd className="mt-1 text-sm text-gray-900 sm:mt-0 sm:col-span-4 break-all font-mono">
              {block.signature}
            </dd>
          </div>
          <div className="py-4 sm:py-5 sm:grid sm:grid-cols-5 sm:gap-4 sm:px-6">
            <dt className="text-sm font-medium text-gray-500">Randao Reveal</dt>
            <dd className="mt-1 text-sm text-gray-900 sm:mt-0 sm:col-span-4 break-all font-mono">
              {block.message.body?.randao_reveal}
            </dd>
          </div>
          <div className="py-4 sm:py-5 sm:grid sm:grid-cols-5 sm:gap-4 sm:px-6">
            <dt className="text-sm font-medium text-gray-500">Graffiti</dt>
            <dd className="mt-1 text-sm text-gray-900 sm:mt-0 sm:col-span-4 break-all font-mono flex items-center justify-between ">
              {showFullHash ? (
                <>
                  <span className="font-mono">{block.message.body?.graffiti}</span>
                </>
              ) : (
                hexToAscii(block.message.body?.graffiti ?? '')
              )}
              <span className="whitespace-nowrap pl-1">
                <button
                  type="button"
                  className={clsx(
                    showFullHash
                      ? 'bg-fuchsia-400 text-fuchsia-100 hover:bg-fuchsia-500'
                      : 'shadow-inner bg-fuchsia-100 text-fuchsia-600 hover:bg-fuchsia-200',
                    'inline-flex items-center rounded-l-lg border border-transparent px-2.5 py-1.5 text-base font-semibold',
                  )}
                  onClick={() => setShowFullHash(false)}
                >
                  Ascii
                </button>
                <button
                  type="button"
                  className={clsx(
                    !showFullHash
                      ? 'bg-fuchsia-400 text-fuchsia-100 hover:bg-fuchsia-500'
                      : 'shadow-inner bg-fuchsia-100 text-fuchsia-600 hover:bg-fuchsia-200',
                    'inline-flex items-center rounded-r-lg border border-transparent px-2.5 py-1.5 text-base font-semibold',
                  )}
                  onClick={() => setShowFullHash(true)}
                >
                  Hex
                </button>
              </span>
            </dd>
          </div>
        </dl>
      </div>
    </div>
  );
}
