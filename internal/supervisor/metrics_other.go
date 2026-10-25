//go:build !linux

package supervisor

func sampleProcess(p *process) (*float64, *float64) { return nil, nil }
