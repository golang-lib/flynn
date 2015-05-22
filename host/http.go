package main

import (
	"errors"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/host/volume/api"
	"github.com/flynn/flynn/host/volume/manager"
	"github.com/flynn/flynn/pinkerton"
	"github.com/flynn/flynn/pinkerton/layer"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/sse"
	"github.com/tav/golly/log"
)

type Host struct {
	state   *State
	backend Backend
}

func (h *Host) StopJob(id string) error {
	job := h.state.GetJob(id)
	if job == nil {
		return errors.New("host: unknown job")
	}
	switch job.Status {
	case host.StatusStarting:
		h.state.SetForceStop(id)
		return nil
	case host.StatusRunning:
		return h.backend.Stop(id)
	default:
		return errors.New("host: job is already stopped")
	}
}

func (h *Host) SignalJob(id string, sig int) error {
	job := h.state.GetJob(id)
	if job == nil {
		return errors.New("host: unknown job")
	}
	return h.backend.Signal(id, sig)
}

func (h *Host) streamEvents(id string, w http.ResponseWriter) error {
	ch := h.state.AddListener(id)
	defer h.state.RemoveListener(id, ch)
	sse.ServeStream(w, ch, nil)
	return nil
}

type jobAPI struct {
	host *Host

	connectDiscoverd func(string) error
}

func (h *jobAPI) ListJobs(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		if err := h.host.streamEvents("all", w); err != nil {
			httphelper.Error(w, err)
		}
		return
	}
	res := h.host.state.Get()

	httphelper.JSON(w, 200, res)
}

func (h *jobAPI) GetJob(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id := ps.ByName("id")

	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		if err := h.host.streamEvents(id, w); err != nil {
			httphelper.Error(w, err)
		}
		return
	}
	job := h.host.state.GetJob(id)
	httphelper.JSON(w, 200, job)
}

func (h *jobAPI) StopJob(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id := ps.ByName("id")
	if err := h.host.StopJob(id); err != nil {
		httphelper.Error(w, err)
		return
	}
	w.WriteHeader(200)
}

func (h *jobAPI) SignalJob(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	sig := ps.ByName("signal")
	if sig == "" {
		httphelper.ValidationError(w, "sig", "must not be empty")
		return
	}
	sigInt, err := strconv.Atoi(sig)
	if err != nil {
		httphelper.ValidationError(w, "sig", "must be an integer")
		return
	}
	id := ps.ByName("id")
	if err := h.host.SignalJob(id, sigInt); err != nil {
		httphelper.Error(w, err)
		return
	}
	w.WriteHeader(200)
}

func (h *jobAPI) PullImages(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	tufDB, err := extractTufDB(r)
	if err != nil {
		httphelper.Error(w, err)
		return
	}
	defer os.Remove(tufDB)

	info := make(chan layer.PullInfo)
	stream := sse.NewStream(w, info, nil)
	go stream.Serve()

	if err := pinkerton.PullImages(
		tufDB,
		r.URL.Query().Get("repository"),
		r.URL.Query().Get("driver"),
		r.URL.Query().Get("root"),
		info,
	); err != nil {
		stream.CloseWithError(err)
		return
	}

	stream.Wait()
}

func (h *jobAPI) AddJob(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	job := &host.Job{}
	if err := httphelper.DecodeJSON(r, job); err != nil {
		httphelper.Error(w, err)
		return
	}
	// TODO(titanous): validate UUID
	job.ID = ps.ByName("id")

	go func() {
		// TODO(titanous): ratelimit this goroutine?
		if err := backend.Run(job, nil); err != nil {
			state.SetStatusFailed(job.ID, err)
		}
	}()

	// TODO(titanous): return 201 Accepted
	httphelper.JSON(w, 200, struct{}{})
}

func (h *jobAPI) ConfigureDiscoverd(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var config struct {
		URL string `json:"url"`
	}
	if err := httphelper.DecodeJSON(r, &config); err != nil {
		httphelper.Error(w, err)
		return
	}
	go func() {
		if err := h.connectDiscoverd(config.URL); err != nil {
			log.Error(err)
			return
		}
	}()
}

func (h *jobAPI) ConfigureNetworking(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var config NetworkConfig
	if err := httphelper.DecodeJSON(r, &config); err != nil {
		shutdown.Fatal(err)
	}

	go func() {
		if _, err := h.backend.ConfigureNetworking(config); err != nil {
			shutdown.Fatal(err)
		}
	}()
}

func extractTufDB(r *http.Request) (string, error) {
	defer r.Body.Close()
	tmp, err := ioutil.TempFile("", "tuf-db")
	if err != nil {
		return "", err
	}
	defer tmp.Close()
	if _, err := io.Copy(tmp, r.Body); err != nil {
		return "", err
	}
	return tmp.Name(), nil
}

func (h *jobAPI) RegisterRoutes(r *httprouter.Router) error {
	r.GET("/host/jobs", h.ListJobs)
	r.GET("/host/jobs/:id", h.GetJob)
	r.PUT("/host/jobs/:id", h.AddJob)
	r.DELETE("/host/jobs/:id", h.StopJob)
	r.PUT("/host/jobs/:id/signal/:signal", h.SignalJob)
	r.POST("/host/pull-images", h.PullImages)
	r.PUT("/host/discoverd", h.ConfigureDiscoverd)
	r.PUT("/host/network", h.ConfigureNetworking)
	return nil
}

func serveHTTP(host *Host, attach *attachHandler, clus *cluster.Client, vman *volumemanager.Manager, connectDiscoverd func(string) error) error {
	l, err := net.Listen("tcp", ":1113")
	if err != nil {
		return nil, err
	}

	r := httprouter.New()

	r.POST("/attach", attach.ServeHTTP)

	jobAPI := &jobAPI{host, connectDiscoverd}
	jobAPI.RegisterRoutes(r)
	volAPI := volumeapi.NewHTTPAPI(clus, vman)
	volAPI.RegisterRoutes(r)

	return http.Serve(l, httphelper.ContextInjector("host", httphelper.NewRequestLogger(r)))
}
