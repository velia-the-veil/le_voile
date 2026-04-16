//go:build linux

package tun

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

// CleanupOrphan détruit une interface TUN résiduelle portant le nom donné
// si elle existe, via netlink (RTM_DELLINK). Opération rapide (< 5s, NFR17)
// et idempotente : retourne nil si l'interface n'existe pas.
//
// Appelée par l'orchestrateur service AVANT tun.New pour gérer le cas où
// un précédent cycle aurait laissé une interface persistente (crash avant
// Close, kill -9, etc.).
func CleanupOrphan(name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("tun: nom invalide %q", name)
	}
	// /sys/class/net/<name> disparaît dès que l'interface est supprimée ;
	// c'est une check O(1) plus rapide que d'ouvrir netlink pour rien.
	if _, err := os.Stat(filepath.Join("/sys/class/net", name)); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("tun: stat /sys/class/net/%s: %w", name, err)
	}

	deadline := time.Now().Add(5 * time.Second)
	if err := deleteLinkByName(name, deadline); err != nil {
		return fmt.Errorf("tun: cleanup %s: %w", name, err)
	}
	return nil
}

// deleteLinkByName envoie un RTM_DELLINK avec IFLA_IFNAME sur un socket
// NETLINK_ROUTE. Requiert CAP_NET_ADMIN. Retourne nil si l'interface n'existe
// déjà plus (ENODEV).
func deleteLinkByName(name string, deadline time.Time) error {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_ROUTE)
	if err != nil {
		return fmt.Errorf("netlink socket: %w", err)
	}
	defer unix.Close(fd)

	sa := &unix.SockaddrNetlink{Family: unix.AF_NETLINK}
	if err := unix.Bind(fd, sa); err != nil {
		return fmt.Errorf("netlink bind: %w", err)
	}
	timeoutMs := time.Until(deadline).Milliseconds()
	if timeoutMs <= 0 {
		timeoutMs = 100
	}
	tv := unix.Timeval{Sec: timeoutMs / 1000, Usec: (timeoutMs % 1000) * 1000}
	// Propager les erreurs setsockopt : sans timeout, Read peut bloquer et
	// dépasser NFR17 < 5s.
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv); err != nil {
		return fmt.Errorf("netlink SO_RCVTIMEO: %w", err)
	}
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_SNDTIMEO, &tv); err != nil {
		return fmt.Errorf("netlink SO_SNDTIMEO: %w", err)
	}

	// Construit le message : nlmsghdr + ifinfomsg + IFLA_IFNAME attr.
	nameBytes := append([]byte(name), 0) // null-terminated
	attrLen := unix.SizeofRtAttr + len(nameBytes)
	// padding à 4 octets pour l'attribut
	padAttr := (4 - (attrLen % 4)) % 4
	msgLen := unix.SizeofNlMsghdr + unix.SizeofIfInfomsg + attrLen + padAttr

	buf := make([]byte, msgLen)

	// nlmsghdr
	binary.LittleEndian.PutUint32(buf[0:4], uint32(msgLen))
	binary.LittleEndian.PutUint16(buf[4:6], unix.RTM_DELLINK)
	binary.LittleEndian.PutUint16(buf[6:8], unix.NLM_F_REQUEST|unix.NLM_F_ACK)
	binary.LittleEndian.PutUint32(buf[8:12], 1) // seq
	binary.LittleEndian.PutUint32(buf[12:16], 0)

	// ifinfomsg — famille AF_UNSPEC, type/index/flags/change à 0 (select by
	// IFLA_IFNAME attribute)
	// Offset 16..16+SizeofIfInfomsg : laisser à zéro.

	// IFLA_IFNAME attribute
	off := unix.SizeofNlMsghdr + unix.SizeofIfInfomsg
	binary.LittleEndian.PutUint16(buf[off:off+2], uint16(attrLen))
	binary.LittleEndian.PutUint16(buf[off+2:off+4], unix.IFLA_IFNAME)
	copy(buf[off+4:], nameBytes)

	if _, err := unix.Write(fd, buf); err != nil {
		return fmt.Errorf("netlink write: %w", err)
	}

	// Lire la réponse ACK (ou NLMSG_ERROR).
	resp := make([]byte, 4096)
	n, err := unix.Read(fd, resp)
	if err != nil {
		return fmt.Errorf("netlink read: %w", err)
	}
	if n < unix.SizeofNlMsghdr {
		return fmt.Errorf("netlink short response: %d octets", n)
	}
	msgType := binary.LittleEndian.Uint16(resp[4:6])
	if msgType == unix.NLMSG_ERROR {
		// NLMSG_ERROR payload : errno int32 (négatif) puis echo du header.
		if n < unix.SizeofNlMsghdr+4 {
			return errors.New("netlink error message tronqué")
		}
		errno := int32(binary.LittleEndian.Uint32(resp[unix.SizeofNlMsghdr : unix.SizeofNlMsghdr+4]))
		if errno == 0 {
			return nil // ACK succès
		}
		if errno == -int32(unix.ENODEV) {
			return nil // déjà absente : idempotent
		}
		return fmt.Errorf("netlink errno %d", errno)
	}
	return nil
}
