package app

import (
	"fmt"

	"github.com/ccammack/easytv/internal/cec"
)

// AdapterReader reads current kernel metadata without sending a CEC query.
type AdapterReader interface {
	AdapterInfo() (cec.AdapterInfo, error)
}

// CECMonitor has one serialized owner. It has no address cache or timer.
// Every frame is evaluated against metadata read for that frame.
type CECMonitor struct {
	app    *Coordinator
	reader AdapterReader
}

func NewCECMonitor(app *Coordinator, reader AdapterReader) (*CECMonitor, error) {
	if app == nil || reader == nil {
		return nil, fmt.Errorf("coordinator and adapter reader required")
	}
	return &CECMonitor{app: app, reader: reader}, nil
}

func (m *CECMonitor) Handle(message cec.ReceivedMessage) (string, error) {
	info, err := m.reader.AdapterInfo()
	if err != nil {
		return "", fmt.Errorf("read current CEC adapter metadata: %w", err)
	}
	return m.app.HandleCEC(message, info)
}
