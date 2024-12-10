import { useState, useMemo } from 'react';

import { MagnifyingGlassCircleIcon } from '@heroicons/react/24/outline';
import ReactTimeAgo from 'react-time-ago';

import CopyToClipboard from '@components/CopyToClipboard';
import Tooltip from '@components/Tooltip';
import FlagImage from '@images/flag.png';
import { APIBeaconSlot } from '@types';
import { truncateHash } from '@utils';

export default function CheckpointsTable(props: {
  latestEpoch?: string;
  slots: APIBeaconSlot[];
  onSlotClick?: (slot: APIBeaconSlot) => void;
  showCheckpoint?: boolean;
}) {
  const [search, setSearch] = useState('');
  const filteredSlots = useMemo(() => {
    if (!search) return props.slots;
    return props.slots.filter((slot) => {
      return (
        slot.slot.toString().includes(search.toLowerCase()) ||
        slot.epoch?.toString().includes(search.toLowerCase()) ||
        slot.block_root?.toLowerCase().includes(search.toLowerCase()) ||
        slot.state_root?.toLowerCase().includes(search.toLowerCase())
      );
    });
  }, [props.slots, search]);
  const onClick = (slot: APIBeaconSlot) => {
    props.onSlotClick?.(slot);
  };

  const onSearchChange = (e: React.ChangeEvent<HTMLInputElement>) => setSearch(e.target.value);
  return (
    <div className="px-4 sm:px-6 lg:px-8">
      <div className="mt-4 sm:flex sm:justify-end">
        <div className="hidden sm:flex flex-auto mr-4 p-2 rounded-md">
          <p className="mt-2 text-lg drop-shadow-lg text-gray-100 font-semibold self-end">
            A list of historical finalized epoch boundaries.{' '}
            {props.showCheckpoint && (
              <span>
                The checkpoint currently being served has the{' '}
                <img src={FlagImage} alt="flag" className="w-5 inline bg-white/20 rounded p-1" />{' '}
                icon.
              </span>
            )}
          </p>
        </div>
        <div className="bg-white/20 p-2 rounded-md">
          <label
            htmlFor="email"
            className="block text-lg drop-shadow-lg font-semibold text-gray-100"
          >
            Search
          </label>
          <div className="mt-1">
            <input
              type="email"
              name="email"
              id="email"
              className="shadow-lg block w-full sm:text-lg border-gray-300 rounded-md p-1"
              onChange={onSearchChange}
            />
          </div>
        </div>
      </div>
      <div className="mt-4 flex flex-col">
        <div className="-my-2 -mx-4 overflow-x-auto sm:-mx-6 lg:-mx-8">
          <div className="inline-block min-w-full py-2 align-middle md:px-6 lg:px-8">
            <div className="overflow-hidden shadow ring-1 ring-black ring-opacity-5 md:rounded-lg">
              <table className="min-w-full divide-y divide-gray-300">
                <thead className="bg-white/20">
                  <tr>
                    <th
                      scope="col"
                      className="hidden sm:table-cell drop-shadow-lg whitespace-nowrap sm:pl-6 sm:px-2 py-3.5 text-left text-sm sm:text-base font-bold text-gray-100"
                    >
                      Epoch
                    </th>
                    <th
                      scope="col"
                      className="whitespace-nowrap drop-shadow-lg pl-2 sm:pl-0 sm:px-2 py-3.5 text-left text-sm sm:text-base font-bold text-gray-100"
                    >
                      Slot
                    </th>
                    <th
                      scope="col"
                      className="whitespace-nowrap drop-shadow-lg sm:px-2 py-3.5 text-left text-sm sm:text-base font-bold text-gray-100"
                    >
                      Time
                    </th>
                    <th
                      scope="col"
                      className="whitespace-nowrap drop-shadow-lg sm:px-2 py-3.5 text-left text-sm sm:text-base font-bold text-gray-100"
                    >
                      State Root
                    </th>
                    <th
                      scope="col"
                      className="hidden sm:table-cell drop-shadow-lg whitespace-nowrap sm:px-2 py-3.5 text-left text-sm sm:text-base font-bold text-gray-100"
                    >
                      Block Root
                    </th>
                    <th scope="col" className="sm:px-2">
                      <span className="sr-only">View</span>
                    </th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-200 bg-white/10">
                  {filteredSlots.map((slot) => (
                    <tr key={slot.slot}>
                      <td className="hidden sm:table-cell sm:pl-6 sm:px-2 drop-shadow-lg whitespace-nowrap py-2 font-semibold text-sm sm:text-base text-gray-100">
                        {slot.epoch}
                        {props.showCheckpoint &&
                          props.latestEpoch &&
                          slot.epoch &&
                          slot.epoch === props.latestEpoch && (
                            <img
                              className="hidden sm:inline-block w-5 pl-2 -mt-1"
                              src={FlagImage}
                              alt="Latest checkpoint"
                            />
                          )}
                      </td>
                      <td className="whitespace-nowrap pl-2 sm:px-2 drop-shadow-lg sm:pl-0 py-2 font-semibold text-sm sm:text-base text-gray-100">
                        {slot.slot}
                        {props.latestEpoch && slot.epoch && slot.epoch === props.latestEpoch && (
                          <img
                            className="sm:hidden w-5 inline-block pl-2 -mt-1"
                            src={FlagImage}
                            alt="Latest checkpoint"
                          />
                        )}
                      </td>
                      <td className="whitespace-nowrap py-2 sm:px-2 drop-shadow-lg text-sm sm:text-base font-semibold text-gray-100">
                        {slot.time?.start_time && (
                          <>
                            <ReactTimeAgo
                              className="hidden lg:block"
                              date={new Date(slot.time.start_time)}
                            />
                            <ReactTimeAgo
                              className="lg:hidden"
                              date={new Date(slot.time.start_time)}
                              timeStyle="twitter"
                            />
                          </>
                        )}
                      </td>
                      <td className="whitespace-nowrap py-2 sm:px-2 drop-shadow-lg text-sm sm:text-base font-semibold text-gray-100">
                        {slot.state_root ? (
                          <>
                            <span className="xl:hidden font-mono cursor-pointer flex">
                              <Tooltip content={slot.state_root}>
                                <span className="relative top-1 group transition duration-300">
                                  {truncateHash(slot.state_root)}
                                  <span className="relative -top-0.5 block max-w-0 group-hover:max-w-full transition-all duration-500 h-0.5 bg-gray-100"></span>
                                </span>
                              </Tooltip>
                              <CopyToClipboard text={slot.state_root} inverted />
                            </span>
                            <span className="hidden xl:table-cell font-mono">
                              <span className="relative top-0.5">{slot.state_root}</span>
                              <CopyToClipboard text={slot.state_root} inverted />
                            </span>
                          </>
                        ) : (
                          ''
                        )}
                      </td>
                      <td className="hidden sm:table-cell whitespace-nowrap py-2 sm:px-2 drop-shadow-lg text-sm sm:text-base font-semibold text-gray-100">
                        {slot.block_root ? (
                          <>
                            <span className="2xl:hidden font-mono cursor-pointer flex">
                              <Tooltip content={slot.block_root}>
                                <span className="relative top-1 group transition duration-300">
                                  {truncateHash(slot.block_root)}
                                  <span className="relative -top-0.5 block max-w-0 group-hover:max-w-full transition-all duration-500 h-0.5 bg-gray-100"></span>
                                </span>
                              </Tooltip>
                              <CopyToClipboard text={slot.block_root} inverted />
                            </span>
                            <span className="hidden 2xl:table-cell font-mono">
                              <span className="relative top-0.5">{slot.block_root}</span>
                              <CopyToClipboard text={slot.block_root} inverted />
                            </span>
                          </>
                        ) : (
                          ''
                        )}
                      </td>
                      <td className="relative whitespace-nowrap text-right text-sm sm:text-base pr-2 lg:px-6 font-semibold">
                        {slot.block_root && (
                          <button className="align-top" onClick={() => onClick(slot)}>
                            <MagnifyingGlassCircleIcon className="h-7 w-7 stroke-gray-50 hover:stroke-gray-100" />
                          </button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
