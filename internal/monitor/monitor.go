package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/mdlayher/arp"
)

type Device struct {
	Name  string
	IP    netip.Addr
	MAC   net.HardwareAddr
	Group string
}

type Result struct {
	Device   Device
	Online   bool
	LastSeen *time.Time
	Error    error
}

type Monitor struct {
	iface      *net.Interface
	timeout    time.Duration
	retryCount int
	retryDelay time.Duration
	logger     *slog.Logger
}

func New(interfaceName string, timeout time.Duration, retryCount int, retryDelay time.Duration, logger *slog.Logger) (*Monitor, error) {
	iface, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get interface %s: %w", interfaceName, err)
	}

	return &Monitor{
		iface:      iface,
		timeout:    timeout,
		retryCount: retryCount,
		retryDelay: retryDelay,
		logger:     logger,
	}, nil
}

func (m *Monitor) Probe(ctx context.Context, devices []Device) []Result {
	results := make([]Result, len(devices))

	var wg sync.WaitGroup
	sem := make(chan struct{}, 10) // concurrency limit

	for i, dev := range devices {
		wg.Add(1)
		go func(idx int, d Device) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			results[idx] = m.probeDevice(ctx, d)
		}(i, dev)
	}

	wg.Wait()
	return results
}

func (m *Monitor) probeDevice(ctx context.Context, dev Device) Result {
	for attempt := 0; attempt <= m.retryCount; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return Result{Device: dev, Error: ctx.Err()}
			case <-time.After(m.retryDelay):
			}
		}

		online, mac, err := m.sendARP(ctx, dev.IP)
		if err != nil {
			m.logger.Debug("ARP probe failed",
				"device", dev.Name,
				"ip", dev.IP,
				"attempt", attempt+1,
				"error", err)
			continue
		}

		if online {
			now := time.Now().UTC()
			result := Result{
				Device:   dev,
				Online:   true,
				LastSeen: &now,
			}
			if mac != nil {
				result.Device.MAC = mac
			}
			return result
		}
	}

	return Result{Device: dev, Online: false}
}

func (m *Monitor) sendARP(ctx context.Context, ip netip.Addr) (bool, net.HardwareAddr, error) {
	client, err := arp.Dial(m.iface)
	if err != nil {
		return false, nil, fmt.Errorf("ARP dial failed: %w", err)
	}
	defer client.Close()

	deadline := time.Now().Add(m.timeout)
	if err := client.SetDeadline(deadline); err != nil {
		return false, nil, fmt.Errorf("set deadline failed: %w", err)
	}

	mac, err := client.Resolve(ip)
	if err != nil {
		// Timeout means no response = device offline
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return false, nil, nil
		}
		return false, nil, fmt.Errorf("ARP resolve failed: %w", err)
	}

	return true, mac, nil
}

func ParseDevice(name, ipStr, macStr, group string) (Device, error) {
	ip, err := netip.ParseAddr(ipStr)
	if err != nil {
		return Device{}, fmt.Errorf("invalid IP address: %s", ipStr)
	}
	if !ip.Is4() {
		return Device{}, fmt.Errorf("not an IPv4 address: %s", ipStr)
	}

	dev := Device{
		Name:  name,
		IP:    ip,
		Group: group,
	}

	if macStr != "" {
		mac, err := net.ParseMAC(macStr)
		if err != nil {
			return Device{}, fmt.Errorf("invalid MAC address %s: %w", macStr, err)
		}
		dev.MAC = mac
	}

	return dev, nil
}
