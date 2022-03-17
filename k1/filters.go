//
//   date  : 2016-05-13
//   author: xjdrew
//

package k1

import (
	"io"

	"github.com/Ubbo-Sathla/kone/tcpip"
	"github.com/google/gopacket/layers"
)

type PacketFilter interface {
	Filter(wr io.Writer, p tcpip.IPv4Packet)
}

type PacketFilterFunc func(wr io.Writer, p tcpip.IPv4Packet)

func (f PacketFilterFunc) Filter(wr io.Writer, p tcpip.IPv4Packet) {
	f(wr, p)
}

func icmpFilterFunc(wr io.Writer, ipPacket tcpip.IPv4Packet) {
	icmpPacket := tcpip.ICMPPacket(ipPacket.Payload())
	if icmpPacket.Type() == layers.ICMPv4TypeEchoRequest && icmpPacket.Code() == 0 {
		logger.Debugf("icmp echo request: %s -> %s", ipPacket.SourceIP(), ipPacket.DestinationIP())
		// forge a reply
		icmpPacket.SetType(layers.ICMPv4TypeEchoReply)
		srcIP := ipPacket.SourceIP()
		dstIP := ipPacket.DestinationIP()
		ipPacket.SetSourceIP(dstIP)
		ipPacket.SetDestinationIP(srcIP)

		icmpPacket.ResetChecksum()
		ipPacket.ResetChecksum()
		wr.Write(ipPacket)
	} else {
		logger.Debugf("icmp: %s -> %s", ipPacket.SourceIP(), ipPacket.DestinationIP())
	}
}
