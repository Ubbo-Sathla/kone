//
//   date  : 2016-05-13
//   author: xjdrew
//

package k1

import (
	"github.com/Ubbo-Sathla/kone/tcpip"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"

	"fmt"
	"io"
	"net"
)

type halfCloseConn interface {
	net.Conn
	CloseRead() error
	CloseWrite() error
}

type TCPRelay struct {
	one       *One
	nat       *Nat
	relayIP   net.IP
	relayPort uint16
}

func copy(src net.Conn, dst net.Conn, ch chan<- int64) {
	written, _ := io.Copy(dst, src)
	ch <- written
}

func copyAndClose(src halfCloseConn, dst halfCloseConn, ch chan<- int64) {
	written, _ := io.Copy(dst, src)

	dst.CloseWrite()
	src.CloseRead()
	ch <- written
}

func (r *TCPRelay) realRemoteHost(conn net.Conn, connData *ConnData) (addr string, proxy string) {
	remoteAddr := conn.RemoteAddr().(*net.TCPAddr)
	remotePort := uint16(remoteAddr.Port)

	session := r.nat.getSession(remotePort)
	if session == nil {
		logger.Errorf("[tcp] %s > %s no session", conn.LocalAddr(), remoteAddr)
		return
	}

	one := r.one

	var host string
	if record := one.dnsTable.GetByIP(session.dstIP); record != nil {
		host = record.Hostname
		proxy = record.Proxy
	} else if one.dnsTable.Contains(session.dstIP) {
		logger.Debugf("[tcp] %s:%d > %s:%d dns expired", session.srcIP, session.srcPort, session.dstIP, session.dstPort)
		return
	} else {
		host = session.dstIP.String()
	}

	connData.Src = session.srcIP.String()
	connData.Dst = host
	connData.Proxy = proxy

	addr = fmt.Sprintf("%s:%d", host, session.dstPort)
	logger.Debugf("[tcp] %s:%d > %s proxy %q", session.srcIP, session.srcPort, addr, proxy)
	return
}

func (r *TCPRelay) handleConn(conn net.Conn) {
	var connData ConnData
	remoteAddr, proxy := r.realRemoteHost(conn, &connData)
	if remoteAddr == "" {
		conn.Close()
		return
	}

	proxies := r.one.proxies
	tunnel, err := proxies.Dial(proxy, remoteAddr)
	if err != nil {
		conn.Close()
		logger.Errorf("[tcp] dial %s by proxy %q failed: %s", remoteAddr, proxy, err)
		return
	}

	uploadChan := make(chan int64)
	downloadChan := make(chan int64)

	connHCC, connOK := conn.(halfCloseConn)
	tunnelHCC, tunnelOK := tunnel.(halfCloseConn)
	if connOK && tunnelOK {
		go copyAndClose(connHCC, tunnelHCC, uploadChan)
		go copyAndClose(tunnelHCC, connHCC, downloadChan)
	} else {
		go copy(conn, tunnel, uploadChan)
		go copy(tunnel, conn, downloadChan)
		defer conn.Close()
		defer tunnel.Close()
	}
	connData.Upload = <-uploadChan
	connData.Download = <-downloadChan

	if r.one.manager != nil {
		r.one.manager.dataCh <- connData
	}
}

func (r *TCPRelay) Serve() error {
	addr := &net.TCPAddr{IP: r.relayIP, Port: int(r.relayPort)}
	ln, err := net.ListenTCP("tcp4", addr)
	if err != nil {
		return err
	}

	logger.Infof("[tcp] listen on %v", addr)

	for {
		conn, err := ln.AcceptTCP()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Temporary() {
				logger.Errorf("acceept failed temporary: %s", netErr.Error())
				continue
			} else {
				return err
			}
		}
		go r.handleConn(conn)
	}
}

// redirect tcp packet to relay
func (r *TCPRelay) Filter(wr io.Writer, ipPacket tcpip.IPv4Packet) {
	p := gopacket.NewPacket(ipPacket, layers.LayerTypeIPv4, gopacket.DecodeOptions{Lazy: true, NoCopy: true})

	tcpPacket := tcpip.TCPPacket(ipPacket.Payload())

	srcIP := p.NetworkLayer().(*layers.IPv4).SrcIP
	dstIP := p.NetworkLayer().(*layers.IPv4).DstIP
	//srcPort := p.TransportLayer().(*layers.TCP).SrcPort
	//dstPort := p.TransportLayer().(*layers.TCP).DstPort

	//srcIP := ipPacket.SourceIP()
	//dstIP := ipPacket.DestinationIP()
	srcPort := tcpPacket.SourcePort()
	dstPort := tcpPacket.DestinationPort()

	var netLayer *layers.IPv4
	var transportLayer *layers.TCP
	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}
	slayers := []gopacket.SerializableLayer{}

	if r.relayIP.Equal(srcIP) && uint16(srcPort) == r.relayPort {
		// from relay
		session := r.nat.getSession(uint16(dstPort))
		if session == nil {
			logger.Debugf("[tcp] %s:%d > %s:%d: no session", srcIP, srcPort, dstIP, dstPort)
			return
		}

		for _, l := range p.Layers() {
			switch l.LayerType() {
			case layers.LayerTypeIPv4:
				netLayer = l.(*layers.IPv4)
				netLayer.SrcIP = session.dstIP
				netLayer.DstIP = session.srcIP
				slayers = append(slayers, l.(gopacket.SerializableLayer))
			case layers.LayerTypeTCP:
				transportLayer = l.(*layers.TCP)
				transportLayer.SrcPort = layers.TCPPort(session.dstPort)
				transportLayer.DstPort = layers.TCPPort(session.srcPort)
				transportLayer.SetNetworkLayerForChecksum(netLayer)
				slayers = append(slayers, l.(gopacket.SerializableLayer))

			case gopacket.LayerTypePayload:
				slayers = append(slayers, l.(gopacket.SerializableLayer))
			default:
				logger.Errorf("unknown network layer: %s", l.LayerType())
			}
		}

	} else {
		// redirect to relay
		isNew, port := r.nat.allocSession(srcIP, dstIP, uint16(srcPort), uint16(dstPort))

		for _, l := range p.Layers() {
			switch l.LayerType() {
			case layers.LayerTypeIPv4:
				netLayer = l.(*layers.IPv4)
				netLayer.SrcIP = dstIP
				netLayer.DstIP = r.relayIP
				slayers = append(slayers, l.(gopacket.SerializableLayer))
			case layers.LayerTypeTCP:
				transportLayer = l.(*layers.TCP)
				transportLayer.SrcPort = layers.TCPPort(port)
				transportLayer.DstPort = layers.TCPPort(r.relayPort)
				transportLayer.SetNetworkLayerForChecksum(netLayer)
				slayers = append(slayers, l.(gopacket.SerializableLayer))
			case gopacket.LayerTypePayload:
				slayers = append(slayers, l.(gopacket.SerializableLayer))
			default:
				logger.Errorf("unknown network layer: %s", l.LayerType())
			}
		}

		if isNew {
			logger.Debugf("[tcp] %s:%d > %s:%d: shape to %s:%d > %s:%d",
				srcIP, srcPort, dstIP, dstPort, dstIP, port, r.relayIP, r.relayPort)
		}

	}

	gopacket.SerializeLayers(buf, opts, slayers...)
	//logger.Debugf("buf: %x", buf.Bytes())

	// write back packet
	wr.Write(buf.Bytes())
}

func NewTCPRelay(one *One, cfg NatConfig) *TCPRelay {
	relay := new(TCPRelay)
	relay.one = one
	relay.nat = NewNat(cfg.NatPortStart, cfg.NatPortEnd)
	relay.relayIP = one.ip
	relay.relayPort = cfg.ListenPort
	return relay
}
