package tools

import (
	"errors"
	"net/http"
	"net/netip"
	"slices"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

const lanPort = 5201
const lanDuration = 2 * time.Minute

type lanSession struct {
	address string
	expires time.Time
	stop    func()
}

type lanView struct {
	Addresses []string  `json:"addresses"`
	Address   string    `json:"address,omitempty"`
	Expires   time.Time `json:"expires_at,omitempty"`
	Port      int       `json:"port"`
}

func (h *HTTP) lanView() (lanView, error) {
	if h.LANAddresses == nil {
		return lanView{}, errors.New("LAN addresses are unavailable")
	}
	addresses, err := h.LANAddresses()
	if err != nil {
		return lanView{}, err
	}
	slices.Sort(addresses)
	view := lanView{Addresses: addresses, Port: lanPort}
	h.lanMu.Lock()
	if h.lan != nil {
		view.Address, view.Expires = h.lan.address, h.lan.expires
	}
	h.lanMu.Unlock()
	return view, nil
}

func (h *HTTP) lanStatus(w http.ResponseWriter, _ *http.Request) {
	view, err := h.lanView()
	if err != nil {
		core.WriteError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	core.WriteJSON(w, http.StatusOK, view)
}

func (h *HTTP) startLAN(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Address string `json:"address"`
	}
	if err := core.DecodeJSON(w, r, &request); err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	addr, err := netip.ParseAddr(request.Address)
	if err != nil || !addr.Is4() || addr.String() != request.Address {
		core.WriteError(w, http.StatusBadRequest, "choose a LAN IPv4 address")
		return
	}
	view, err := h.lanView()
	if err != nil {
		core.WriteError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if !slices.Contains(view.Addresses, request.Address) {
		core.WriteError(w, http.StatusUnprocessableEntity, "address is not a configured LAN address")
		return
	}
	h.lanMu.Lock()
	defer h.lanMu.Unlock()
	if h.lan != nil {
		core.WriteError(w, http.StatusConflict, "a LAN test server is already running")
		return
	}
	start := h.StartIperf
	if start == nil {
		start = startIperf
	}
	stop, done, err := start(request.Address, lanPort)
	if err != nil {
		core.WriteError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	session := &lanSession{address: request.Address, expires: time.Now().Add(lanDuration), stop: stop}
	h.lan = session
	time.AfterFunc(lanDuration, func() { h.endLAN(session) })
	go func() { <-done; h.endLAN(session) }()
	core.WriteJSON(w, http.StatusOK, lanView{Addresses: view.Addresses, Address: session.address, Expires: session.expires, Port: lanPort})
}

func (h *HTTP) endLAN(session *lanSession) {
	h.lanMu.Lock()
	defer h.lanMu.Unlock()
	if h.lan == session {
		session.stop()
		h.lan = nil
	}
}

func (h *HTTP) stopLAN(w http.ResponseWriter, _ *http.Request) {
	h.lanMu.Lock()
	if h.lan != nil {
		h.lan.stop()
		h.lan = nil
	}
	h.lanMu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTP) Close() {
	h.lanMu.Lock()
	if h.lan != nil {
		h.lan.stop()
		h.lan = nil
	}
	h.lanMu.Unlock()
}
