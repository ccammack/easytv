package cec

import "time"

// Backend owns adapter access without HTTP or application-event dependencies.
type Backend interface {
	AdapterInfo() (AdapterInfo, error)
	Open(devicePath string) error
	Close() error
	SetPower(on bool) error
	ClaimActiveSource() error
	Poll() (*ReceivedMessage, *AdapterEvent, error)
	SelfID() (int, error)
}

type MockCECBackend struct {
	devicePath string
	selfID     int
}

func (m *MockCECBackend) Open(devicePath string) error {
	m.devicePath = devicePath
	m.selfID = 4
	return nil
}
func (m *MockCECBackend) Close() error             { return nil }
func (m *MockCECBackend) SetPower(bool) error      { return nil }
func (m *MockCECBackend) ClaimActiveSource() error { return nil }
func (m *MockCECBackend) SelfID() (int, error)     { return m.selfID, nil }
func (m *MockCECBackend) Poll() (*ReceivedMessage, *AdapterEvent, error) {
	time.Sleep(250 * time.Millisecond)
	return nil, nil, nil
}

func (m *MockCECBackend) AdapterInfo() (AdapterInfo, error) {
	return AdapterInfo{DevicePath: m.devicePath, LogicalAddresses: []int{m.selfID}}, nil
}
