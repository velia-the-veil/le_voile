//go:build windows

package tun

import (
	"errors"
	"fmt"
	"os"
	"sync"

	wgtun "golang.zx2c4.com/wireguard/tun"
)

// New crée une interface Wintun via wintun.dll (signée Microsoft). Extrait
// la DLL embarquée vers %ProgramData%/LeVoile/wintun.dll au premier appel
// (idempotent, compare SHA-256). Requiert LocalSystem ou élévation admin.
func New(name string, mtu int) (Device, error) {
	if err := validateParams(name, mtu); err != nil {
		return nil, err
	}
	if err := ensureWintunDLL(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	dev, err := wgtun.CreateTUN(name, mtu)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return nil, fmt.Errorf("%w: %v", ErrPermission, err)
		}
		return nil, fmt.Errorf("tun: CreateTUN: %w", err)
	}
	effName, err := dev.Name()
	if err != nil {
		dev.Close()
		return nil, fmt.Errorf("tun: Name: %w", err)
	}
	effMTU, err := dev.MTU()
	if err != nil {
		dev.Close()
		return nil, fmt.Errorf("tun: MTU: %w", err)
	}
	return &wgDevice{inner: dev, name: effName, mtu: effMTU}, nil
}

type wgDevice struct {
	inner  wgtun.Device
	name   string
	mtu    int
	closed bool
	mu     sync.Mutex
}

func (d *wgDevice) Read(buf []byte) (int, error) {
	bufs := [][]byte{buf}
	sizes := []int{0}
	n, err := d.inner.Read(bufs, sizes, 0)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, nil
	}
	return sizes[0], nil
}

func (d *wgDevice) Write(pkt []byte) (int, error) {
	bufs := [][]byte{pkt}
	n, err := d.inner.Write(bufs, 0)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, nil
	}
	return len(pkt), nil
}

func (d *wgDevice) Name() string { return d.name }
func (d *wgDevice) MTU() int     { return d.mtu }

func (d *wgDevice) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	d.closed = true
	return d.inner.Close()
}
