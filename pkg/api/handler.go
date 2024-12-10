package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/ethpandaops/checkpointz/pkg/beacon"
	"github.com/ethpandaops/checkpointz/pkg/service/checkpointz"
	"github.com/ethpandaops/checkpointz/pkg/service/eth"
	"github.com/julienschmidt/httprouter"
	"github.com/sirupsen/logrus"
)

// Handler is an API handler that is responsible for negotiating with a HTTP api.
// All http-level concerns should be handled in this package, with the "namespaces" (eth/checkpointz)
// handling all business logic and dealing with concrete types.
type Handler struct {
	log logrus.FieldLogger

	eth           *eth.Handler
	checkpointz   *checkpointz.Handler
	publicURL     string
	brandName     string
	brandImageURL string

	metrics Metrics
}

func NewHandler(log logrus.FieldLogger, beac beacon.FinalityProvider, config *beacon.Config) *Handler {
	return &Handler{
		log: log.WithField("module", "api"),

		eth:           eth.NewHandler(log, beac, "checkpointz"),
		checkpointz:   checkpointz.NewHandler(log, beac),
		publicURL:     config.Frontend.PublicURL,
		brandName:     config.Frontend.BrandName,
		brandImageURL: config.Frontend.BrandImageURL,

		metrics: NewMetrics("http"),
	}
}

func (h *Handler) Register(ctx context.Context, router *httprouter.Router) error {
	router.GET("/eth/v1/beacon/genesis", h.wrappedHandler(h.handleEthV1BeaconGenesis))
	router.GET("/eth/v1/beacon/blocks/:block_id/root", h.wrappedHandler(h.handleEthV1BeaconBlocksRoot))
	router.GET("/eth/v1/beacon/states/:state_id/finality_checkpoints", h.wrappedHandler(h.handleEthV1BeaconStatesFinalityCheckpoints))
	router.GET("/eth/v1/beacon/deposit_snapshot", h.wrappedHandler(h.handleEthV1BeaconDepositSnapshot))
	router.GET("/eth/v1/beacon/blob_sidecars/:block_id", h.wrappedHandler(h.handleEthV1BeaconBlobSidecars))

	router.GET("/eth/v1/config/spec", h.wrappedHandler(h.handleEthV1ConfigSpec))
	router.GET("/eth/v1/config/deposit_contract", h.wrappedHandler(h.handleEthV1ConfigDepositContract))
	router.GET("/eth/v1/config/fork_schedule", h.wrappedHandler(h.handleEthV1ConfigForkSchedule))

	router.GET("/eth/v1/node/syncing", h.wrappedHandler(h.handleEthV1NodeSyncing))
	router.GET("/eth/v1/node/version", h.wrappedHandler(h.handleEthV1NodeVersion))
	router.GET("/eth/v1/node/peers", h.wrappedHandler(h.handleEthV1NodePeers))
	router.GET("/eth/v1/node/peer_count", h.wrappedHandler(h.handleEthV1NodePeerCount))

	router.GET("/eth/v2/beacon/blocks/:block_id", h.wrappedHandler(h.handleEthV2BeaconBlocks))

	router.GET("/eth/v2/debug/beacon/states/:state_id", h.wrappedHandler(h.handleEthV2DebugBeaconStates))

	router.GET("/checkpointz/v1/status", h.wrappedHandler(h.handleCheckpointzStatus))
	router.GET("/checkpointz/v1/beacon/slots", h.wrappedHandler(h.handleCheckpointzBeaconSlots))
	router.GET("/checkpointz/v1/beacon/slots/:slot", h.wrappedHandler(h.handleCheckpointzBeaconSlot))
	router.GET("/checkpointz/v1/ready", h.wrappedHandler(h.handleCheckpointzReady))

	return nil
}

func deriveRegisteredPath(request *http.Request, ps httprouter.Params) string {
	registeredPath := request.URL.Path
	for _, param := range ps {
		registeredPath = strings.Replace(registeredPath, param.Value, fmt.Sprintf(":%s", param.Key), 1)
	}

	return registeredPath
}

func (h *Handler) wrappedHandler(handler func(ctx context.Context, r *http.Request, p httprouter.Params, contentType ContentType) (*HTTPResponse, error)) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		start := time.Now()

		contentType := NewContentTypeFromRequest(r)
		ctx := r.Context()
		registeredPath := deriveRegisteredPath(r, p)

		h.log.WithFields(logrus.Fields{
			"method":       r.Method,
			"path":         r.URL.Path,
			"content_type": contentType,
			"accept":       r.Header.Get("Accept"),
		}).Trace("Handling request")

		h.metrics.ObserveRequest(r.Method, registeredPath)

		response := &HTTPResponse{}

		var err error

		defer func() {
			h.metrics.ObserveResponse(r.Method, registeredPath, fmt.Sprintf("%v", response.StatusCode), contentType.String(), time.Since(start))
		}()

		response, err = handler(ctx, r, p, contentType)
		if err != nil {
			if writeErr := WriteErrorResponse(w, err.Error(), response.StatusCode); writeErr != nil {
				h.log.WithError(writeErr).Error("Failed to write error response")
			}

			return
		}

		data, err := response.MarshalAs(contentType)
		if err != nil {
			if writeErr := WriteErrorResponse(w, err.Error(), http.StatusInternalServerError); writeErr != nil {
				h.log.WithError(writeErr).Error("Failed to write error response")
			}

			return
		}

		for header, value := range response.Headers {
			w.Header().Set(header, value)
		}

		if err := WriteContentAwareResponse(w, data, contentType); err != nil {
			h.log.WithError(err).Error("Failed to write response")
		}
	}
}

func (h *Handler) handleEthV1BeaconGenesis(ctx context.Context, r *http.Request, p httprouter.Params, contentType ContentType) (*HTTPResponse, error) {
	if err := ValidateContentType(contentType, []ContentType{ContentTypeJSON}); err != nil {
		return NewUnsupportedMediaTypeResponse(nil), err
	}

	genesis, err := h.eth.BeaconGenesis(ctx)
	if err != nil {
		return NewInternalServerErrorResponse(nil), err
	}

	var rsp = NewSuccessResponse(ContentTypeResolvers{
		ContentTypeJSON: genesis.MarshalJSON,
	})

	rsp.SetCacheControl("public, s-max-age=30")

	return rsp, nil
}

func (h *Handler) handleEthV2BeaconBlocks(ctx context.Context, r *http.Request, p httprouter.Params, contentType ContentType) (*HTTPResponse, error) {
	if err := ValidateContentType(contentType, []ContentType{ContentTypeJSON, ContentTypeSSZ}); err != nil {
		return NewUnsupportedMediaTypeResponse(nil), err
	}

	blockID, err := eth.NewBlockIdentifier(p.ByName("block_id"))
	if err != nil {
		return NewBadRequestResponse(nil), err
	}

	block, err := h.eth.BeaconBlock(ctx, blockID)
	if err != nil {
		return NewInternalServerErrorResponse(nil), err
	}

	var rsp = &HTTPResponse{}

	switch block.Version {
	case spec.DataVersionPhase0:
		rsp = NewSuccessResponse(ContentTypeResolvers{
			ContentTypeJSON: block.Phase0.MarshalJSON,
			ContentTypeSSZ:  block.Phase0.MarshalSSZ,
		})
	case spec.DataVersionAltair:
		rsp = NewSuccessResponse(ContentTypeResolvers{
			ContentTypeJSON: block.Altair.MarshalJSON,
			ContentTypeSSZ:  block.Altair.MarshalSSZ,
		})
	case spec.DataVersionBellatrix:
		rsp = NewSuccessResponse(ContentTypeResolvers{
			ContentTypeJSON: block.Bellatrix.MarshalJSON,
			ContentTypeSSZ:  block.Bellatrix.MarshalSSZ,
		})
	case spec.DataVersionCapella:
		rsp = NewSuccessResponse(ContentTypeResolvers{
			ContentTypeJSON: block.Capella.MarshalJSON,
			ContentTypeSSZ:  block.Capella.MarshalSSZ,
		})
	case spec.DataVersionDeneb:
		rsp = NewSuccessResponse(ContentTypeResolvers{
			ContentTypeJSON: block.Deneb.MarshalJSON,
			ContentTypeSSZ:  block.Deneb.MarshalSSZ,
		})
	case spec.DataVersionElectra:
		rsp = NewSuccessResponse(ContentTypeResolvers{
			ContentTypeJSON: block.Electra.MarshalJSON,
			ContentTypeSSZ:  block.Electra.MarshalSSZ,
		})
	default:
		return NewInternalServerErrorResponse(nil), errors.New("unknown block version")
	}

	rsp.AddExtraData("version", block.Version.String())
	rsp.AddExtraData("execution_optimistic", "false")

	switch blockID.Type() {
	case eth.BlockIDRoot, eth.BlockIDGenesis, eth.BlockIDSlot:
		rsp.SetCacheControl("public, s-max-age=6000")
	case eth.BlockIDFinalized:
		// TODO(sam.calder-mason): This should be calculated using the Weak-Subjectivity period.
		rsp.SetCacheControl("public, s-max-age=30")
	case eth.BlockIDHead:
		rsp.SetCacheControl("public, s-max-age=30")
	}

	return rsp, nil
}

func (h *Handler) handleEthV2DebugBeaconStates(ctx context.Context, r *http.Request, p httprouter.Params, contentType ContentType) (*HTTPResponse, error) {
	if err := ValidateContentType(contentType, []ContentType{ContentTypeSSZ}); err != nil {
		return NewUnsupportedMediaTypeResponse(nil), err
	}

	id, err := eth.NewStateIdentifier(p.ByName("state_id"))
	if err != nil {
		return NewBadRequestResponse(nil), err
	}

	state, err := h.eth.BeaconState(ctx, id)
	if err != nil {
		return NewInternalServerErrorResponse(nil), err
	}

	if state == nil {
		return NewInternalServerErrorResponse(nil), errors.New("state not found")
	}

	rsp := NewSuccessResponse(ContentTypeResolvers{
		ContentTypeSSZ: func() ([]byte, error) {
			switch state.Version {
			case spec.DataVersionPhase0:
				return state.Phase0.MarshalSSZ()
			case spec.DataVersionAltair:
				return state.Altair.MarshalSSZ()
			case spec.DataVersionBellatrix:
				return state.Bellatrix.MarshalSSZ()
			case spec.DataVersionCapella:
				return state.Capella.MarshalSSZ()
			case spec.DataVersionDeneb:
				return state.Deneb.MarshalSSZ()
			case spec.DataVersionElectra:
				return state.Electra.MarshalSSZ()
			default:
				return nil, fmt.Errorf("unknown state version: %s", state.Version.String())
			}
		},
	})

	switch id.Type() {
	case eth.StateIDSlot:
		rsp.SetCacheControl("public, s-max-age=6000")
	case eth.StateIDFinalized:
		rsp.SetCacheControl("public, s-max-age=180")
	case eth.StateIDRoot:
		rsp.SetCacheControl("public, s-max-age=6000")
	case eth.StateIDHead:
		rsp.SetCacheControl("public, s-max-age=30")
	}

	rsp.SetEthConsensusVersion(state.Version.String())

	return rsp, nil
}

func (h *Handler) handleEthV1ConfigSpec(ctx context.Context, r *http.Request, p httprouter.Params, contentType ContentType) (*HTTPResponse, error) {
	if err := ValidateContentType(contentType, []ContentType{ContentTypeJSON}); err != nil {
		return NewUnsupportedMediaTypeResponse(nil), err
	}

	sp, err := h.eth.ConfigSpec(ctx)
	if err != nil {
		return NewInternalServerErrorResponse(nil), err
	}

	var rsp = NewSuccessResponse(ContentTypeResolvers{
		ContentTypeJSON: func() ([]byte, error) {
			return json.Marshal(sp)
		},
	})

	rsp.SetCacheControl("public, s-max-age=30")

	return rsp, nil
}

func (h *Handler) handleEthV1ConfigDepositContract(ctx context.Context, r *http.Request, p httprouter.Params, contentType ContentType) (*HTTPResponse, error) {
	if err := ValidateContentType(contentType, []ContentType{ContentTypeJSON}); err != nil {
		return NewUnsupportedMediaTypeResponse(nil), err
	}

	contract, err := h.eth.DepositContract(ctx)
	if err != nil {
		return NewInternalServerErrorResponse(nil), err
	}

	var rsp = NewSuccessResponse(ContentTypeResolvers{
		ContentTypeJSON: func() ([]byte, error) {
			return json.Marshal(contract)
		},
	})

	rsp.SetCacheControl("public, s-max-age=30")

	return rsp, nil
}

func (h *Handler) handleEthV1ConfigForkSchedule(ctx context.Context, r *http.Request, p httprouter.Params, contentType ContentType) (*HTTPResponse, error) {
	if err := ValidateContentType(contentType, []ContentType{ContentTypeJSON}); err != nil {
		return NewUnsupportedMediaTypeResponse(nil), err
	}

	forks, err := h.eth.ForkSchedule(ctx)
	if err != nil {
		return NewInternalServerErrorResponse(nil), err
	}

	var rsp = NewSuccessResponse(ContentTypeResolvers{
		ContentTypeJSON: func() ([]byte, error) {
			return json.Marshal(forks)
		},
	})

	rsp.SetCacheControl("public, s-max-age=30")

	return rsp, nil
}

func (h *Handler) handleEthV1NodeSyncing(ctx context.Context, r *http.Request, p httprouter.Params, contentType ContentType) (*HTTPResponse, error) {
	if err := ValidateContentType(contentType, []ContentType{ContentTypeJSON}); err != nil {
		return NewUnsupportedMediaTypeResponse(nil), err
	}

	syncing, err := h.eth.NodeSyncing(ctx)
	if err != nil {
		return NewInternalServerErrorResponse(nil), err
	}

	var rsp = NewSuccessResponse(ContentTypeResolvers{
		ContentTypeJSON: func() ([]byte, error) {
			return json.Marshal(syncing)
		},
	})

	rsp.SetCacheControl("public, s-max-age=10")

	return rsp, nil
}

func (h *Handler) handleEthV1NodeVersion(ctx context.Context, r *http.Request, p httprouter.Params, contentType ContentType) (*HTTPResponse, error) {
	if err := ValidateContentType(contentType, []ContentType{ContentTypeJSON}); err != nil {
		return NewUnsupportedMediaTypeResponse(nil), err
	}

	version, err := h.eth.NodeVersion(ctx)
	if err != nil {
		return NewInternalServerErrorResponse(nil), err
	}

	data := struct {
		Version string `json:"version"`
	}{Version: version}

	var rsp = NewSuccessResponse(ContentTypeResolvers{
		ContentTypeJSON: func() ([]byte, error) {
			return json.Marshal(data)
		},
	})

	rsp.SetCacheControl("public, s-max-age=60")

	return rsp, nil
}

func (h *Handler) handleEthV1NodePeerCount(ctx context.Context, r *http.Request, p httprouter.Params, contentType ContentType) (*HTTPResponse, error) {
	if err := ValidateContentType(contentType, []ContentType{ContentTypeJSON}); err != nil {
		return NewUnsupportedMediaTypeResponse(nil), err
	}

	peers, err := h.eth.Peers(ctx)
	if err != nil {
		return NewInternalServerErrorResponse(nil), err
	}

	data := struct {
		Connected     string `json:"connected"`
		Connecting    string `json:"connecting"`
		Disconnected  string `json:"disconnected"`
		Disconnecting string `json:"disconnecting"`
	}{
		Connected:     fmt.Sprintf("%d", len(peers.ByState("connected"))),
		Disconnected:  fmt.Sprintf("%d", len(peers.ByState("disconnected"))),
		Connecting:    fmt.Sprintf("%d", len(peers.ByState("connecting"))),
		Disconnecting: fmt.Sprintf("%d", len(peers.ByState("disconnecting"))),
	}

	var rsp = NewSuccessResponse(ContentTypeResolvers{
		ContentTypeJSON: func() ([]byte, error) {
			return json.Marshal(data)
		},
	})

	rsp.SetCacheControl("public, s-max-age=60")

	return rsp, nil
}

func (h *Handler) handleEthV1NodePeers(ctx context.Context, r *http.Request, p httprouter.Params, contentType ContentType) (*HTTPResponse, error) {
	if err := ValidateContentType(contentType, []ContentType{ContentTypeJSON}); err != nil {
		return NewUnsupportedMediaTypeResponse(nil), err
	}

	peers, err := h.eth.Peers(ctx)
	if err != nil {
		return NewInternalServerErrorResponse(nil), err
	}

	var rsp = NewSuccessResponse(ContentTypeResolvers{
		ContentTypeJSON: func() ([]byte, error) {
			return json.Marshal(peers)
		},
	})

	rsp.SetCacheControl("public, s-max-age=60")

	return rsp, nil
}

func (h *Handler) handleCheckpointzStatus(ctx context.Context, r *http.Request, p httprouter.Params, contentType ContentType) (*HTTPResponse, error) {
	if err := ValidateContentType(contentType, []ContentType{ContentTypeJSON}); err != nil {
		return NewUnsupportedMediaTypeResponse(nil), err
	}

	status, err := h.checkpointz.V1Status(ctx, checkpointz.NewStatusRequest())
	if err != nil {
		return NewInternalServerErrorResponse(nil), err
	}

	status.PublicURL = h.publicURL
	status.BrandName = h.brandName
	status.BrandImageURL = h.brandImageURL

	rsp := NewSuccessResponse(ContentTypeResolvers{
		ContentTypeJSON: func() ([]byte, error) {
			return json.Marshal(status)
		},
	})

	rsp.SetCacheControl("public, s-max-age=5")

	return rsp, nil
}

func (h *Handler) handleCheckpointzReady(ctx context.Context, r *http.Request, p httprouter.Params, contentType ContentType) (*HTTPResponse, error) {
	if err := ValidateContentType(contentType, []ContentType{ContentTypeJSON}); err != nil {
		return NewUnsupportedMediaTypeResponse(nil), err
	}

	status, err := h.checkpointz.V1Status(ctx, checkpointz.NewStatusRequest())
	if err != nil {
		return NewInternalServerErrorResponse(nil), err
	}

	if status.Finality == nil || status.Finality.Finalized == nil {
		return NewInternalServerErrorResponse(nil), errors.New("no finalized checkpoint")
	}

	rsp := NewSuccessResponse(ContentTypeResolvers{
		ContentTypeJSON: func() ([]byte, error) {
			return json.Marshal(`true`)
		},
	})

	return rsp, nil
}

func (h *Handler) handleCheckpointzBeaconSlots(ctx context.Context, r *http.Request, p httprouter.Params, contentType ContentType) (*HTTPResponse, error) {
	if err := ValidateContentType(contentType, []ContentType{ContentTypeJSON}); err != nil {
		return NewUnsupportedMediaTypeResponse(nil), err
	}

	slots, err := h.checkpointz.V1BeaconSlots(ctx, checkpointz.NewBeaconSlotsRequest())
	if err != nil {
		return NewInternalServerErrorResponse(nil), err
	}

	rsp := NewSuccessResponse(ContentTypeResolvers{
		ContentTypeJSON: func() ([]byte, error) {
			return json.Marshal(slots)
		},
	})

	rsp.SetCacheControl("public, s-max-age=5")

	return rsp, nil
}

func (h *Handler) handleCheckpointzBeaconSlot(ctx context.Context, r *http.Request, p httprouter.Params, contentType ContentType) (*HTTPResponse, error) {
	if err := ValidateContentType(contentType, []ContentType{ContentTypeJSON}); err != nil {
		return NewUnsupportedMediaTypeResponse(nil), err
	}

	slot, err := eth.NewSlotFromString(p.ByName("slot"))
	if err != nil {
		return NewBadRequestResponse(nil), err
	}

	slots, err := h.checkpointz.V1BeaconSlot(ctx, checkpointz.NewBeaconSlotRequest(slot))
	if err != nil {
		return NewInternalServerErrorResponse(nil), err
	}

	rsp := NewSuccessResponse(ContentTypeResolvers{
		ContentTypeJSON: func() ([]byte, error) {
			return json.Marshal(slots)
		},
	})

	rsp.SetCacheControl("public, s-max-age=5")

	return rsp, nil
}

func (h *Handler) handleEthV1BeaconStatesFinalityCheckpoints(ctx context.Context, r *http.Request, p httprouter.Params, contentType ContentType) (*HTTPResponse, error) {
	if err := ValidateContentType(contentType, []ContentType{ContentTypeJSON}); err != nil {
		return NewUnsupportedMediaTypeResponse(nil), err
	}

	id, err := eth.NewStateIdentifier(p.ByName("state_id"))
	if err != nil {
		return NewBadRequestResponse(nil), err
	}

	finality, err := h.eth.FinalityCheckpoints(ctx, id)
	if err != nil {
		return NewInternalServerErrorResponse(nil), err
	}

	rsp := NewSuccessResponse(ContentTypeResolvers{
		ContentTypeJSON: func() ([]byte, error) {
			return json.Marshal(finality)
		},
	})

	switch id.Type() {
	case eth.StateIDFinalized, eth.StateIDHead:
		rsp.SetCacheControl("public, s-max-age=5")
	}

	return rsp, nil
}

func (h *Handler) handleEthV1BeaconBlocksRoot(ctx context.Context, r *http.Request, p httprouter.Params, contentType ContentType) (*HTTPResponse, error) {
	if err := ValidateContentType(contentType, []ContentType{ContentTypeJSON}); err != nil {
		return NewUnsupportedMediaTypeResponse(nil), err
	}

	id, err := eth.NewBlockIdentifier(p.ByName("block_id"))
	if err != nil {
		return NewBadRequestResponse(nil), err
	}

	root, err := h.eth.BlockRoot(ctx, id)
	if err != nil {
		return NewInternalServerErrorResponse(nil), err
	}

	wrapped := struct {
		Root string `json:"root"`
	}{
		Root: fmt.Sprintf("%x", root),
	}

	return NewSuccessResponse(ContentTypeResolvers{
		ContentTypeJSON: func() ([]byte, error) {
			return json.Marshal(wrapped)
		},
	}), nil
}

func (h *Handler) handleEthV1BeaconDepositSnapshot(ctx context.Context, r *http.Request, p httprouter.Params, contentType ContentType) (*HTTPResponse, error) {
	if err := ValidateContentType(contentType, []ContentType{ContentTypeJSON}); err != nil {
		return NewUnsupportedMediaTypeResponse(nil), err
	}

	snapshot, err := h.eth.DepositSnapshot(ctx)
	if err != nil {
		return NewInternalServerErrorResponse(nil), err
	}

	return NewSuccessResponse(ContentTypeResolvers{
		ContentTypeJSON: func() ([]byte, error) {
			return json.Marshal(snapshot)
		},
	}), nil
}

func (h *Handler) handleEthV1BeaconBlobSidecars(ctx context.Context, r *http.Request, p httprouter.Params, contentType ContentType) (*HTTPResponse, error) {
	if err := ValidateContentType(contentType, []ContentType{ContentTypeJSON}); err != nil {
		return NewUnsupportedMediaTypeResponse(nil), err
	}

	id, err := eth.NewBlockIdentifier(p.ByName("block_id"))
	if err != nil {
		return NewBadRequestResponse(nil), err
	}

	queryParams := r.URL.Query()
	indicesRaw := queryParams["indices"]

	indices := make([]int, 0, len(indicesRaw))

	for _, index := range indicesRaw {
		converted, errr := strconv.Atoi(index)
		if errr != nil {
			return NewBadRequestResponse(nil), errr
		}

		indices = append(indices, converted)
	}

	sidecars, err := h.eth.BlobSidecars(ctx, id, indices)
	if err != nil {
		return NewInternalServerErrorResponse(nil), err
	}

	rsp := NewSuccessResponse(ContentTypeResolvers{
		ContentTypeJSON: func() ([]byte, error) {
			return json.Marshal(sidecars)
		},
	})

	switch id.Type() {
	case eth.BlockIDFinalized, eth.BlockIDRoot:
		rsp.SetCacheControl("public, s-max-age=6000")
	default:
		rsp.SetCacheControl("public, s-max-age=15")
	}

	return rsp, nil
}
