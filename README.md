# Checkpointz

Checkpointz simplifies the process of running an Ethereum Beacon Chain checkpoint sync endpoint.


> :warning: **Checkpointz is still in heavy development**. Use with caution. Using with a beacon node that is attached to a validator is currently not recommended.



![Homepage](screenshots/homepage.jpg)

----------
## Contents
* [Features](#features)
* [What is checkpoint sync?](#what-is-checkpoint-sync)
* [Supported Beacon clients](#supported-beacon-upstreams)
- [Usage](#usage)
  * [Configuration](#configuration)
    + [Simple example](#simple-example)
    + [Full mode](#full-mode)
    + [Disabled frontend](#disabled-frontend)
    + [Full example](#full-example)
  * [Getting Started](#getting-started)
    + [Download a release](#download-a-release)
    + [Docker](#docker)
      - [Images](#images)
    + [Kubernetes via Helm](#kubernetes-via-helm)
    + [Grafana](#grafana)
* [Contributing](#contributing)
  + [Running locally](#running-locally)
    - [Backend](#backend)
    - [Frontend](#frontend)
* [Contact](#contact)

----------

## Features
- Operating mode:
  - `light` - The default mode of operation. Provides enough data for users to use your instance to verify the state they got from somewhere else.
  - `full` - Provides all the functionality of `light` mode, with the additional ability to serve state requests for beacon nodes to checkpoint sync from.
- Web UI
  - Shows a table of historical epoch boundaries and their corresponding state/block roots for cross referencing.
  - Provides an in-built guide for users to get started with checkpoint sync with client-specific information.
  - Displays information about the configured upstreams.
- Resource reduction
  - Adds HTTP cache-control headers depending on the content
- DOS protection
  - Never routes an incoming request directly to an upstream beacon node
- Support for multiple upstream beacon nodes
  - Only serves a new finalized epoch once 50%+ of upstream beacon nodes agree
- Extensive Prometheus metrics

## What is checkpoint sync?
Checkpoint sync is an operation that lets fresh beacon nodes jump to the head of the chain by fetching the state from a trusted & synced beacon node. 

More info: https://notes.ethereum.org/sWeLohipS9GdgMugYn9VkQ

## Supported Beacon upstreams
|   |  Prysm |  Lighthouse | Nimbus |  Lodestar  | Teku |
|:---:|:---:|:---:|:---:|:---:|:---:|
|  Light mode | ✅  | ✅  |  ✅ | ✅  | ✅ |
|  Full mode |  with `--data-storage-mode archive` | ✅  | ✅ | ✅  |  with `--data-storage-mode archive` |

Note: Teku will require a resync from genesis if you enable `--data-storage-mode archive`.

Note: Prysm will require to start with `--enable-debug-rpc-endpoints` flag for Checkpointz full mode (debug endpoints are required).
# Usage
Checkpointz requires a config file. An example file can be found [here](https://github.com/ethpandaops/checkpointz/blob/master/example_config.yaml).

```
Checkpoint sync provider for Ethereum beacon nodes

Usage:
  checkpointz [flags]

Flags:
      --config string   config file (default is config.yaml) (default "config.yaml")
  -h, --help            help for checkpointz
```

## Configuration

Checkpointz relies entirely on a single `yaml` config file.

| Name | Default | Description |
| --- | --- | --- |
| global.listenAddr | `:5555` | The address the main http server will listen on |
| global.logging | `warn` | Log level (`panic`, `fatal`, `warn`, `info`, `debug`, `trace`) |
| global.metricsAddr | `:9090` | The address the metrics server will listen on |
| checkpointz.caches.blocks.max_items | `200` | Controls the amount of "block" items that can be stored by Checkpointz (minimum 3) |
| checkpointz.caches.states.max_items | `5` | Controls the amount of "state" items that can be stored by Checkpointz (minimum 3). These states are very large and this value will directly relate to memory usage. Anything higher than 10 is not recommended |
| checkpointz.mode | `light` | Controls the mode to run checkpointz in. `light` mode will only serve `blocks`, allowing users to use your Checkpointz as a cross reference. `full` will server `blocks` and `state`, allowing users to additonal use your Checkpointz as their state provider. When in full mode the upstream beacon should ONLY be tasked with serving checkpoint data (don't validate on this instance.) |
| checkpointz.historical_epoch_count | `20` | Controls the amount of historical epoch boundaries that Checkpointz will fetch and serve. |
| checkpointz.frontend.enabled | `true` | if the frontend should be enabled |
| checkpointz.frontend.brand_image_url |  | The brand logo to display on the frontend |
| checkpointz.frontend.brand_name | | The name of the brand to display on the frontend |
| checkpointz.frontend.public_url |  | The public URL of where the frontend will be served from |
| beacon.upstreams[].name |  | Shown in the frontend |
| beacon.upstreams[].address |  | The address of your beacon node. Note: NOT shown in the frontend |
| beacon.upstreams[].dataProvider |  | If true, Checkpointz will use this instance to fetch beacon blocks/state. If false, will only be used for finality checkpoints |

### Simple example

```yaml
# use defaults and add a single beacon upstream node

beacon:
  upstreams:
  - name: remote
    address: http://localhost:5052
    dataProvider: true
```

### Full mode

```yaml
checkpointz:
  mode: full

beacon:
  upstreams:
  - name: remote
    address: http://localhost:5052 # When in full mode the upstream beacon should ONLY be tasked with serving checkpoint data (don't validate on this instance.)
    dataProvider: true
```

### Disabled frontend

```yaml
checkpointz:
  frontend:
    enabled: false

beacon:
  upstreams:
  - name: remote
    address: http://localhost:5052
    dataProvider: true
```

### Full example

```yaml
global:
  # The address the main http server will listen on
  listenAddr: ":5555"
  # Log level (panic, fatal, warn, info, debug, trace)
  logging: "debug"
  # The address the metrics server will listen on
  metricsAddr: ":9090"

checkpointz:
  mode: light
  caches:
    blocks:
      # Controls the amount of "block" items that can be stored by Checkpointz (minimum 3)
      max_items: 200
    states:
      # Controls the amount of "state" items that can be stored by Checkpointz (minimum 3)
      # These starts a very large and this value will directly relate to memory usage. Anything higher than 
      # 10 is not recommended.
      max_items: 5
  historical_epoch_count: 20 # Controls the amount of historical epoch boundaries that Checkpointz will fetch and serve.
  frontend:
    # if the frontend should be enabled
    enabled: true
    # The brand logo to display on the frontend (optional)
    # brand_image_url: https://www.cdn.com/logo.png
    # The name of the brand to display on the frontend (optional)
    # brand_name: Brandname
    # The public URL of where the frontend will be served from (optional)
    # public_url: https://www.domain.com

beacon:
  # Upstreams configures the upstream beacon nodes to use.
  upstreams:
    # Shown in the frontend
  - name: remote
    # The address of your beacon node. Note: NOT shown in the frontend
    address: http://localhost:5052
    # If true, Checkpointz will use this instance to fetch beacon blocks/state. If false, will only be used for finality checkpoints.
    dataProvider: true
```

## Getting Started

### Download a release
Download the latest release from the [Releases page](https://github.com/ethpandaops/checkpointz/releases). Extract and run with:
```sh
./checkpointz --config your-config.yaml
```

### Docker
Available as a docker image at [ethpandaops/checkpointz](https://hub.docker.com/r/ethpandaops/checkpointz/tags)
#### Images
- `latest` - distroless, multiarch
- `latest-debian` - debian, multiarch
- `$version` - distroless, multiarch, pinned to a release (i.e. `0.4.0`)
- `$version-debian` - debian, multiarch, pinned to a release (i.e. `0.4.0-debian`)

**Quick start**
```
docker run -d  --name checkpointz -v $HOST_DIR_CHANGE_ME/config.yaml:/opt/checkpointz/config.yaml -p 9090:9090 -p 5555:5555 -it ethpandaops/checkpointz:latest --config /opt/checkpointz/config.yaml;
docker logs -f checkpointz;
```


### Kubernetes via Helm
[Read more](https://github.com/skylenet/ethereum-helm-charts/tree/master/charts/checkpointz)
```
helm repo add ethereum-helm-charts https://skylenet.github.io/ethereum-helm-charts

helm install checkpointz ethereum-helm-charts/checkpointz -f your_values.yaml
```
### Grafana
[Download the Checkpointz dashboard here](https://grafana.com/grafana/dashboards/16814-checkpointz)

**Building yourself (requires Go)**

1. Clone the repo
   ```sh
   go get github.com/ethpandaops/checkpointz
   ```
2. Change directories
   ```sh
   cd ./checkpointz
   ```
3. Build the binary
   ```sh  
    go build -o checkpointz .
   ```
4. Run the service
   ```sh  
    ./checkpointz
   ```

## Contributing

Contributions are greatly appreciated! Pull requests will be reviewed and merged promptly if you're interested in improving the Checkpointz!

1. Fork the project
2. Create your feature branch:
    - `git checkout -b feat/new-feature`
3. Commit your changes:
    - `git commit -m 'feat(profit): new feature`
4. Push to the branch:
    -`git push origin feat/new-feature`
5. Open a pull request

### Running locally
#### Backend
```sh
go run main.go --config your_config.yaml
```

#### Frontend

A basic frontend is provided in this project in [`./web`](https://github.com/ethpandaops/checkpointz/blob/master/example_config.yaml) directory which needs to be built before it can be served by the server, eg. `http://localhost:5555`.

The frontend can be built with the following command;
```bash
# install node modules and build
make build-web
```

Building frontend requires `npm` and `NodeJS` to be installed.


## Contact

Sam - [@samcmau](https://twitter.com/samcmau)

Andrew - [@savid](https://twitter.com/Savid)
