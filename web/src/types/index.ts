export interface APIStatus {
  data: {
    upstreams: Record<string, APIUpstream>;
    finality?: APICheckpoints;
    public_url?: string;
    brand_name?: string;
    brand_image_url?: string;
    operating_mode?: 'light' | 'full';
    version?: {
      full?: string;
      git_commit?: string;
      release?: string;
      short?: string;
    };
  };
}

export interface APISlotTime {
  start_time: string;
  end_time: string;
}

export interface APICheckpoints {
  finalized?: APICheckpoint;
  current_justified?: APICheckpoint;
  previous_justified?: APICheckpoint;
}

export interface APICheckpoint {
  epoch: string;
  root: string;
}

export interface APIUpstream {
  name: string;
  healthy: boolean;
  network_name?: string;
  finality?: APICheckpoints;
}

export interface APIBeaconSlot {
  slot: number;
  block_root?: string;
  state_root?: string;
  epoch?: string;
  time?: APISlotTime;
}

export interface APIBeaconSlots {
  data: {
    slots: APIBeaconSlot[];
  };
}

export interface APIBeaconBlockMessageBody {
  randao_reveal?: string;
  execution_payload?: {
    block_hash?: string;
    block_number?: string;
  };
  eth1_data?: {
    block_hash?: string;
  };
  graffiti?: string;
}

export interface APIBeaconBlockMessage {
  message?: {
    parent_root?: string;
    proposer_index?: string;
    slot?: string;
    state_root?: string;
    body?: APIBeaconBlockMessageBody;
  };
  signature?: string;
}

export interface APIBeaconBlock {
  Version: 'BELLATRIX' | 'ALTAIR' | 'PHASE0' | 'CAPELLA' | 'DENEB' | 'ELECTRA';
  Altair?: APIBeaconBlockMessage;
  Bellatrix?: APIBeaconBlockMessage;
  Capella?: APIBeaconBlockMessage;
  Deneb?: APIBeaconBlockMessage;
  Phase0?: APIBeaconBlockMessage;
  Electra?: APIBeaconBlockMessage;
}

export interface APIBeaconSlotBlock {
  data: {
    block?: APIBeaconBlock;
    epoch?: number;
    time?: APISlotTime;
  };
}
