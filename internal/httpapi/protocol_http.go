package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/ccammack/easytv/internal/cec"
)

// CECMessageReceivedEvent represents one received packet, not inferred state.
type CECMessageReceivedEvent struct {
	ID         string `json:"id"`
	Event      string `json:"event"`
	DeviceName string `json:"device_name"`
	cec.ReceivedMessage
}

func (s *Server) adapterHandler(w http.ResponseWriter, r *http.Request) {
	if s.backend == nil {
		http.Error(w, "CEC unavailable", http.StatusServiceUnavailable)
		return
	}
	info, err := s.backend.AdapterInfo(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	response := struct {
		DeviceName string `json:"device_name"`
		cec.AdapterInfo
	}{DeviceName: s.deviceName, AdapterInfo: info}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}
