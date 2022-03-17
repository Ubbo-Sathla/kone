//
//   date  : 2016-05-13
//   author: xjdrew
//

package k1

import (
	"net"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/songgao/water"
)

var MTU = 1500

type TunDriver struct {
	ifce    *water.Interface
	filters map[layers.IPProtocol]PacketFilter
}

func (tun *TunDriver) Serve() error {
	ifce := tun.ifce
	filters := tun.filters

	buffer := make([]byte, MTU)
	for {
		n, err := ifce.Read(buffer)
		if err != nil {
			return err
		}

		packet := buffer[:n]

		p := gopacket.NewPacket(packet, layers.LayerTypeIPv4, gopacket.DecodeOptions{Lazy: true, NoCopy: true})
		if network := p.NetworkLayer(); network == nil {
			logger.Debug("No net layer found")
		} else if ip, ok := network.(*layers.IPv4); !ok {
			logger.Debug("Net layer is not IP layer")
		} else {

			filter := filters[ip.Protocol]
			if filter == nil {
				logger.Noticef("%v > %v protocol %d unsupport", ip.SrcIP, ip.DstIP, ip.Protocol)
				continue
			}

			filter.Filter(ifce, packet)
		}

	}
}

func (tun *TunDriver) AddRoutes(vals []string) {
	name := tun.ifce.Name()
	for _, val := range vals {
		_, subnet, _ := net.ParseCIDR(val)
		if subnet != nil {
			addRoute(name, subnet)
			logger.Infof("add route %s by %s", val, name)
		}
	}
}

func NewTunDriver(ip net.IP, subnet *net.IPNet, filters map[layers.IPProtocol]PacketFilter) (*TunDriver, error) {
	ifce, err := createTun(ip, subnet.Mask)
	if err != nil {
		return nil, err
	}
	return &TunDriver{ifce: ifce, filters: filters}, nil
}
